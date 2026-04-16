use std::collections::HashSet;

use corporate_catering_system::access::AccessController;
use corporate_catering_system::anomaly_alert::{
    AnomalyAlertSeverity, AnomalyAlertWorkflow, AnomalyRule, AnomalyRuleId, AnomalyRuleKind,
    AnomalyThresholdComparator,
};
use corporate_catering_system::audit::{
    AuditAction, AuditInvestigationFilter, AuditRetentionPolicy, AuditTimestamp,
    ImmutableAuditTrail,
};
use corporate_catering_system::identity::{
    ActorId, AuthenticatedActorContext, AuthenticationSource, PlantId, PlantScope, Role,
};
use corporate_catering_system::menu_supply_window::{
    MenuHealthTag, MenuItemId, MenuSupplyPolicy, Money, OrderId, OrderLineItemRequest,
    OrderMutation, VendorMenuItem, VendorMenuItemDraft,
};
use corporate_catering_system::transport::http::{
    HttpOrderExecutionError, HttpOrderingExecutionGateway,
};
use corporate_catering_system::transport::mcp::{
    runtime_mcp_resource_contract_issues, runtime_mcp_resources, runtime_mcp_tool_contract_issues,
    runtime_mcp_tools, runtime_mcp_write_tool_mapping_contract_issues, McpAnomalyExecutionGateway,
    McpAuthenticationModel, McpAuthorizationError, McpAuthorizationGateway, McpCapabilityDomain,
    McpOrderingExecutionGateway, McpServiceAccountGrant, McpShortLivedKeyBridge,
    McpToolExecutionError, MCP_TOOL_ANOMALY_UPSERT_RULE, MCP_TOOL_ORDERING_CREATE_EMPLOYEE_ORDER,
    MCP_TOOL_ORDERING_UPDATE_EMPLOYEE_ORDER,
};
use corporate_catering_system::vendor_compliance::{
    ComplianceDate, ComplianceDocumentTemplate, DocumentTemplateId, HistoryRetentionPolicy,
    VendorCategory, VendorComplianceLifecycle, VendorDocumentSubmission, VendorId,
    VendorReviewDecision,
};
use corporate_catering_system::vendor_delivery_mapping::{
    DeliveryMappingId, DeliveryRuleEffect, ServiceWindow, TaipeiBusinessMoment,
    VendorPlantDeliveryMapping, VendorPlantDeliveryPolicy,
};

fn actor_id(value: &str) -> ActorId {
    ActorId::parse(value).expect("actor id should be valid")
}

fn plant_id(value: &str) -> PlantId {
    PlantId::parse(value).expect("plant id should be valid")
}

fn restricted_scope(plants: &[&str]) -> PlantScope {
    PlantScope::restricted(plants.iter().map(|value| plant_id(value)).collect())
        .expect("restricted scope should be valid")
}

fn employee_actor(actor_id_value: &str) -> AuthenticatedActorContext {
    AuthenticatedActorContext::new(
        actor_id(actor_id_value),
        Role::Employee,
        restricted_scope(&["fab-a"]),
        AuthenticationSource::CorporateSso,
    )
    .expect("employee actor should be valid")
}

fn vendor_actor(actor_id_value: &str) -> AuthenticatedActorContext {
    AuthenticatedActorContext::new(
        actor_id(actor_id_value),
        Role::VendorOperator,
        restricted_scope(&["fab-a"]),
        AuthenticationSource::VendorAccountMfa,
    )
    .expect("vendor actor should be valid")
}

fn committee_actor(actor_id_value: &str) -> AuthenticatedActorContext {
    AuthenticatedActorContext::new(
        actor_id(actor_id_value),
        Role::CommitteeAdmin,
        PlantScope::all(),
        AuthenticationSource::CorporateSso,
    )
    .expect("committee actor should be valid")
}

fn oauth_service_account_actor(
    actor_id_value: &str,
    role: Role,
    plant_scope: PlantScope,
) -> AuthenticatedActorContext {
    AuthenticatedActorContext::new(
        actor_id(actor_id_value),
        role,
        plant_scope,
        AuthenticationSource::OAuthServiceAccount,
    )
    .expect("oauth service-account actor should be valid")
}

fn vendor_id(value: &str) -> VendorId {
    VendorId::parse(value).expect("vendor id should be valid")
}

fn vendor_category(value: &str) -> VendorCategory {
    VendorCategory::parse(value).expect("vendor category should be valid")
}

fn template_id(value: &str) -> DocumentTemplateId {
    DocumentTemplateId::parse(value).expect("template id should be valid")
}

fn mapping_id(value: &str) -> DeliveryMappingId {
    DeliveryMappingId::parse(value).expect("delivery mapping id should be valid")
}

fn menu_item_id(value: &str) -> MenuItemId {
    MenuItemId::parse(value).expect("menu item id should be valid")
}

fn order_id(value: &str) -> OrderId {
    OrderId::parse(value).expect("order id should be valid")
}

fn taipei_moment(epoch_day: i32, minute_of_day: u16) -> TaipeiBusinessMoment {
    TaipeiBusinessMoment::new(epoch_day, minute_of_day).expect("Taipei business moment is valid")
}

fn ensure_test_otel_endpoint() {
    std::env::set_var("OTEL_EXPORTER_OTLP_ENDPOINT", "http://127.0.0.1:4317");
}

fn required_template_for(category: &VendorCategory) -> ComplianceDocumentTemplate {
    ComplianceDocumentTemplate::new(
        template_id("tmpl-mcp-domain-parity-license"),
        category.clone(),
        "Business License",
        true,
        365,
        vec![30, 7],
        0,
    )
    .expect("template should be valid")
}

fn activate_vendor(
    lifecycle: &mut VendorComplianceLifecycle,
    committee: &AuthenticatedActorContext,
    vendor_operator: &AuthenticatedActorContext,
    vendor: &VendorId,
    category: &VendorCategory,
) {
    lifecycle
        .register_vendor_application(
            vendor_operator,
            vendor.clone(),
            "MCP Domain Parity Vendor",
            category.clone(),
            ComplianceDate::from_epoch_day(0),
        )
        .expect("vendor application should be registered");
    lifecycle
        .submit_document(
            vendor_operator,
            vendor,
            &template_id("tmpl-mcp-domain-parity-license"),
            VendorDocumentSubmission::new(
                "s3://evidence/docs/mcp-domain-parity-license.pdf",
                ComplianceDate::from_epoch_day(0),
                ComplianceDate::from_epoch_day(300),
            )
            .expect("vendor document submission should be valid"),
        )
        .expect("vendor document should be submitted");
    lifecycle
        .review_application(
            committee,
            vendor,
            VendorReviewDecision::Approved,
            "Vendor is approved for MCP parity tests.",
            ComplianceDate::from_epoch_day(1),
        )
        .expect("vendor review should succeed");
}

fn delivery_mapping(
    mapping_id_value: &str,
    vendor_id: &VendorId,
    effect: DeliveryRuleEffect,
    precedence: u16,
) -> VendorPlantDeliveryMapping {
    VendorPlantDeliveryMapping::new(
        mapping_id(mapping_id_value),
        vendor_id.clone(),
        plant_id("fab-a"),
        ServiceWindow::new(taipei_moment(10, 540), taipei_moment(40, 1200))
            .expect("service window should be valid"),
        effect,
        precedence,
    )
}

fn menu_item(vendor_id: &VendorId, delivery_epoch_day: i32) -> VendorMenuItem {
    VendorMenuItem::new(
        menu_item_id("menu-mcp-domain-parity"),
        vendor_id.clone(),
        VendorMenuItemDraft::new(
            "MCP Domain Parity Bento",
            "Used for MCP/HTTP ordering parity tests.",
            "BENTO",
            vec![MenuHealthTag::HighProtein],
            None,
            Money::new("TWD", 12000).expect("money should be valid"),
            20,
            delivery_epoch_day,
        )
        .expect("menu draft should be valid"),
    )
}

#[test]
fn mcp_catalog_covers_required_domains_for_tools_and_resources() {
    let tool_issues = runtime_mcp_tool_contract_issues();
    assert!(
        tool_issues.is_empty(),
        "runtime MCP tool catalog has issues:\n{}",
        tool_issues.join("\n")
    );

    let resource_issues = runtime_mcp_resource_contract_issues();
    assert!(
        resource_issues.is_empty(),
        "runtime MCP resource catalog has issues:\n{}",
        resource_issues.join("\n")
    );

    let tool_domains = runtime_mcp_tools()
        .iter()
        .map(|tool| tool.capability_domain())
        .collect::<HashSet<_>>();
    let resource_domains = runtime_mcp_resources()
        .iter()
        .map(|resource| resource.capability_domain())
        .collect::<HashSet<_>>();
    for domain in McpCapabilityDomain::ALL {
        assert!(
            tool_domains.contains(&domain),
            "tool catalog missing domain {}",
            domain.as_str()
        );
        assert!(
            resource_domains.contains(&domain),
            "resource catalog missing domain {}",
            domain.as_str()
        );
    }
}

#[test]
fn mcp_write_tools_have_shared_service_mapping_evidence() {
    let issues = runtime_mcp_write_tool_mapping_contract_issues();
    assert!(
        issues.is_empty(),
        "runtime MCP write-tool mappings have issues:\n{}",
        issues.join("\n")
    );
}

#[test]
fn mcp_oauth_service_account_and_bridge_controls_enforce_tool_level_rbac() {
    ensure_test_otel_endpoint();
    let service_account_actor = oauth_service_account_actor(
        "svc-mcp-domain-authz",
        Role::CommitteeAdmin,
        PlantScope::all(),
    );
    let service_account_grant = McpServiceAccountGrant::new(
        service_account_actor.actor_id().clone(),
        service_account_actor,
        [MCP_TOOL_ANOMALY_UPSERT_RULE],
    )
    .expect("service account grant should be valid");
    let valid_bridge = McpShortLivedKeyBridge::new(
        "bridge-a1",
        1_000,
        1_600,
        1_550,
        "rotated before invocation",
    )
    .expect("bridge key should be valid");

    let gateway = McpAuthorizationGateway::new(AccessController::with_default_policy());
    let authorized = gateway
        .authorize_tool_write(
            &service_account_grant,
            MCP_TOOL_ANOMALY_UPSERT_RULE,
            None,
            1_575,
            Some(&valid_bridge),
        )
        .expect("authorized MCP write should succeed");
    assert_eq!(
        authorized.authentication_model(),
        McpAuthenticationModel::OAuthServiceAccountWithBridgeKey
    );
    assert_eq!(authorized.bridge_key_id(), Some("bridge-a1"));
    assert!(authorized.risk().is_high_risk_write());

    let disallowed = gateway
        .authorize_tool_write(
            &service_account_grant,
            "settlement.lock_cycle",
            None,
            1_575,
            None,
        )
        .expect_err("tool-level RBAC should block ungranted tools");
    assert!(matches!(
        disallowed,
        McpAuthorizationError::ToolNotGrantedForServiceAccount { .. }
    ));

    let expired = gateway
        .authorize_tool_write(
            &service_account_grant,
            MCP_TOOL_ANOMALY_UPSERT_RULE,
            None,
            1_700,
            Some(&valid_bridge),
        )
        .expect_err("expired bridge key should be rejected");
    assert!(matches!(
        expired,
        McpAuthorizationError::BridgeKeyExpired { .. }
    ));

    let long_ttl_bridge =
        McpShortLivedKeyBridge::new("bridge-a2", 1_000, 3_000, 1_550, "invalid ttl bridge")
            .expect("bridge structure should be valid before ttl check");
    let ttl_error = gateway
        .authorize_tool_write(
            &service_account_grant,
            MCP_TOOL_ANOMALY_UPSERT_RULE,
            None,
            1_575,
            Some(&long_ttl_bridge),
        )
        .expect_err("overlong bridge TTL should be rejected");
    assert!(matches!(
        ttl_error,
        McpAuthorizationError::BridgeKeyTtlTooLong { .. }
    ));

    let stale_rotation_bridge =
        McpShortLivedKeyBridge::new("bridge-a3", 1_000, 1_600, 1_200, "rotation staleness guard")
            .expect("bridge structure should be valid before staleness check");
    let stale_error = gateway
        .authorize_tool_write(
            &service_account_grant,
            MCP_TOOL_ANOMALY_UPSERT_RULE,
            None,
            1_575,
            Some(&stale_rotation_bridge),
        )
        .expect_err("stale bridge rotation should be rejected");
    assert!(matches!(
        stale_error,
        McpAuthorizationError::BridgeKeyRotationStale { .. }
    ));

    let non_oauth_actor = committee_actor("svc-mcp-domain-authz-legacy-source");
    let non_oauth_error = McpServiceAccountGrant::new(
        non_oauth_actor.actor_id().clone(),
        non_oauth_actor,
        [MCP_TOOL_ANOMALY_UPSERT_RULE],
    )
    .expect_err("service-account grant must require OAuth service-account source");
    assert!(matches!(
        non_oauth_error,
        McpAuthorizationError::UnsupportedServiceAccountAuthenticationSource { .. }
    ));
}

#[test]
fn mcp_and_http_ordering_gateways_share_validation_and_error_behavior() {
    ensure_test_otel_endpoint();
    let committee = committee_actor("committee-mcp-http-parity");
    let vendor_operator = vendor_actor("vendor-mcp-http-parity");
    let employee = employee_actor("employee-mcp-http-parity");
    let category = vendor_category("RESTAURANT");
    let vendor = vendor_id("ven-mcp-http-parity-a1");

    let mut lifecycle = VendorComplianceLifecycle::new(HistoryRetentionPolicy::default());
    lifecycle
        .upsert_document_template(&committee, required_template_for(&category))
        .expect("template upsert should succeed");
    activate_vendor(
        &mut lifecycle,
        &committee,
        &vendor_operator,
        &vendor,
        &category,
    );

    let mut delivery_policy = VendorPlantDeliveryPolicy::new();
    delivery_policy
        .upsert_mapping(
            &committee,
            taipei_moment(10, 550),
            delivery_mapping(
                "map-mcp-http-parity-allow",
                &vendor,
                DeliveryRuleEffect::Allow,
                10,
            ),
        )
        .expect("allow mapping should be accepted");

    let menu_supply = MenuSupplyPolicy::default();
    menu_supply
        .upsert_menu_item(&vendor_operator, menu_item(&vendor, 20))
        .expect("menu item upsert should succeed");

    let http_gateway =
        HttpOrderingExecutionGateway::new(&lifecycle, &delivery_policy, &menu_supply);
    let mcp_gateway = McpOrderingExecutionGateway::new(&lifecycle, &delivery_policy, &menu_supply);
    let mcp_service_account = oauth_service_account_actor(
        "svc-mcp-ordering-parity",
        Role::Employee,
        restricted_scope(&["fab-a"]),
    );
    let mcp_grant = McpServiceAccountGrant::new(
        mcp_service_account.actor_id().clone(),
        mcp_service_account,
        [
            MCP_TOOL_ORDERING_CREATE_EMPLOYEE_ORDER,
            MCP_TOOL_ORDERING_UPDATE_EMPLOYEE_ORDER,
        ],
    )
    .expect("MCP ordering grant should be valid");
    let auth_gateway = McpAuthorizationGateway::new(AccessController::with_default_policy());
    let create_authorized = auth_gateway
        .authorize_tool_write(
            &mcp_grant,
            MCP_TOOL_ORDERING_CREATE_EMPLOYEE_ORDER,
            Some(&plant_id("fab-a")),
            2_000,
            None,
        )
        .expect("mcp create write should be authorized");
    let update_authorized = auth_gateway
        .authorize_tool_write(
            &mcp_grant,
            MCP_TOOL_ORDERING_UPDATE_EMPLOYEE_ORDER,
            Some(&plant_id("fab-a")),
            2_001,
            None,
        )
        .expect("mcp update write should be authorized");

    http_gateway
        .execute_create_employee_order(
            &employee,
            order_id("ord-mcp-http-parity-001"),
            &vendor,
            &plant_id("fab-a"),
            20,
            vec![
                OrderLineItemRequest::new(menu_item_id("menu-mcp-domain-parity"), 1, vec![])
                    .expect("line item should be valid"),
            ],
            taipei_moment(19, 700),
        )
        .expect("http create should succeed");
    mcp_gateway
        .execute_create_employee_order(
            &create_authorized,
            order_id("ord-mcp-http-parity-002"),
            &vendor,
            &plant_id("fab-a"),
            20,
            vec![
                OrderLineItemRequest::new(menu_item_id("menu-mcp-domain-parity"), 1, vec![])
                    .expect("line item should be valid"),
            ],
            taipei_moment(19, 701),
        )
        .expect("mcp create should succeed");

    let http_unsupported = http_gateway
        .execute_update_employee_order(
            &employee,
            &order_id("ord-mcp-http-parity-001"),
            &vendor,
            &plant_id("fab-a"),
            OrderMutation::MarkRefundPending,
            taipei_moment(19, 702),
        )
        .expect_err("http should reject unsupported employee mutation");
    let mcp_unsupported = mcp_gateway
        .execute_update_employee_order(
            &update_authorized,
            &order_id("ord-mcp-http-parity-002"),
            &vendor,
            &plant_id("fab-a"),
            OrderMutation::MarkRefundPending,
            taipei_moment(19, 703),
        )
        .expect_err("mcp should reject unsupported employee mutation");
    assert!(matches!(
        http_unsupported,
        HttpOrderExecutionError::UnsupportedEmployeeMutation {
            operation: "MARK_REFUND_PENDING"
        }
    ));
    assert!(matches!(
        mcp_unsupported,
        McpToolExecutionError::Domain(HttpOrderExecutionError::UnsupportedEmployeeMutation {
            operation: "MARK_REFUND_PENDING"
        })
    ));

    drop(http_gateway);
    drop(mcp_gateway);
    delivery_policy
        .upsert_mapping(
            &committee,
            taipei_moment(19, 704),
            delivery_mapping(
                "map-mcp-http-parity-deny",
                &vendor,
                DeliveryRuleEffect::Deny,
                20,
            ),
        )
        .expect("deny mapping should be accepted");

    let http_gateway =
        HttpOrderingExecutionGateway::new(&lifecycle, &delivery_policy, &menu_supply);
    let mcp_gateway = McpOrderingExecutionGateway::new(&lifecycle, &delivery_policy, &menu_supply);
    let create_authorized = auth_gateway
        .authorize_tool_write(
            &mcp_grant,
            MCP_TOOL_ORDERING_CREATE_EMPLOYEE_ORDER,
            Some(&plant_id("fab-a")),
            2_002,
            None,
        )
        .expect("mcp create write should remain authorized");

    let http_deliverability = http_gateway
        .execute_create_employee_order(
            &employee,
            order_id("ord-mcp-http-parity-003"),
            &vendor,
            &plant_id("fab-a"),
            20,
            vec![
                OrderLineItemRequest::new(menu_item_id("menu-mcp-domain-parity"), 1, vec![])
                    .expect("line item should be valid"),
            ],
            taipei_moment(19, 705),
        )
        .expect_err("http create should fail due to deny mapping");
    let mcp_deliverability = mcp_gateway
        .execute_create_employee_order(
            &create_authorized,
            order_id("ord-mcp-http-parity-004"),
            &vendor,
            &plant_id("fab-a"),
            20,
            vec![
                OrderLineItemRequest::new(menu_item_id("menu-mcp-domain-parity"), 1, vec![])
                    .expect("line item should be valid"),
            ],
            taipei_moment(19, 706),
        )
        .expect_err("mcp create should fail due to deny mapping");
    assert!(matches!(
        http_deliverability,
        HttpOrderExecutionError::Deliverability(_)
    ));
    assert!(matches!(
        mcp_deliverability,
        McpToolExecutionError::Domain(HttpOrderExecutionError::Deliverability(_))
    ));
}

#[test]
fn high_risk_mcp_writes_are_authorized_and_audited() {
    ensure_test_otel_endpoint();
    let audit_trail =
        ImmutableAuditTrail::new(AuditRetentionPolicy::new(365).expect("policy should be valid"));
    let anomaly_workflow = AnomalyAlertWorkflow::with_default_rules(audit_trail.clone());

    let service_account_actor = oauth_service_account_actor(
        "svc-mcp-audit-highrisk",
        Role::CommitteeAdmin,
        PlantScope::all(),
    );
    let grant = McpServiceAccountGrant::new(
        service_account_actor.actor_id().clone(),
        service_account_actor.clone(),
        [MCP_TOOL_ANOMALY_UPSERT_RULE],
    )
    .expect("service account grant should be valid");

    let auth_gateway = McpAuthorizationGateway::new(AccessController::with_default_policy());
    let authorized = auth_gateway
        .authorize_tool_write(&grant, MCP_TOOL_ANOMALY_UPSERT_RULE, None, 2_000, None)
        .expect("high-risk MCP write should be authorized");
    assert!(authorized.risk().is_high_risk_write());
    assert_eq!(
        authorized.authentication_model(),
        McpAuthenticationModel::OAuthServiceAccount
    );

    let mcp_anomaly_gateway = McpAnomalyExecutionGateway::new(&anomaly_workflow);
    let rule = AnomalyRule::new(
        AnomalyRuleId::parse("rule-mcp-domain-parity-alert").expect("rule id should parse"),
        AnomalyRuleKind::ComplaintSpike,
        "Complaint Spike High-Risk Rule",
        "Trigger when complaint volume exceeds managed threshold.",
        "issue-mcp-authz-model",
        true,
        3.0,
        AnomalyThresholdComparator::GreaterThanOrEqual,
        7,
        60,
        AnomalyAlertSeverity::Critical,
    )
    .expect("anomaly rule should be valid");
    mcp_anomaly_gateway
        .execute_upsert_rule(&authorized, rule, AuditTimestamp::from_epoch_day(200))
        .expect("anomaly rule upsert should succeed");

    let events = audit_trail
        .investigation_query(
            &service_account_actor,
            &AuditInvestigationFilter::default()
                .with_actor_id(service_account_actor.actor_id().clone())
                .with_action(AuditAction::UpsertAnomalyDetectionRule),
        )
        .expect("audit investigation query should succeed");
    assert!(
        events
            .iter()
            .any(|event| event.audit_identity().operation_id() == "upsertAnomalyRule"),
        "high-risk MCP write should emit an auditable event with the operation id"
    );
}
