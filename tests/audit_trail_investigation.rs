use std::collections::BTreeSet;

use corporate_catering_system::audit::{
    AuditAction, AuditCorrelationId, AuditEntityRef, AuditEntityType, AuditEvidenceWrite,
    AuditIdentityLink, AuditInvestigationFilter, AuditRetentionPolicy, AuditTimestamp,
    AuditTrailError, ImmutableAuditTrail,
};
use corporate_catering_system::identity::{
    ActorId, AuthenticatedActorContext, AuthenticationSource, PlantId, PlantScope, Role,
};
use corporate_catering_system::menu_supply_window::{
    MenuHealthTag, MenuItemId, MenuSupplyPolicy, Money, OrderId, OrderLineItemRequest,
    OrderMutation, OrderingGovernancePolicy, VendorMenuItem, VendorMenuItemDraft,
};
use corporate_catering_system::transport::http::HttpAuditInvestigationExecutionGateway;
use corporate_catering_system::vendor_compliance::{
    ComplianceDate, ComplianceDocumentTemplate, DocumentTemplateId, HistoryRetentionPolicy,
    VendorCategory, VendorComplianceLifecycle, VendorDocumentSubmission, VendorId,
    VendorReviewDecision,
};
use corporate_catering_system::vendor_delivery_mapping::TaipeiBusinessMoment;

fn actor_id(value: &str) -> ActorId {
    ActorId::parse(value).expect("actor id should be valid")
}

fn plant_id(value: &str) -> PlantId {
    PlantId::parse(value).expect("plant id should be valid")
}

fn restricted_scope(plants: &[&str]) -> PlantScope {
    PlantScope::restricted(plants.iter().map(|value| plant_id(value)).collect())
        .expect("restricted plant scope should be valid")
}

fn committee_admin() -> AuthenticatedActorContext {
    AuthenticatedActorContext::new(
        actor_id("committee-audit-001"),
        Role::CommitteeAdmin,
        PlantScope::all(),
        AuthenticationSource::CorporateSso,
    )
    .expect("committee admin should be valid")
}

fn vendor_operator() -> AuthenticatedActorContext {
    AuthenticatedActorContext::new(
        actor_id("vendor-audit-001"),
        Role::VendorOperator,
        restricted_scope(&["fab-a"]),
        AuthenticationSource::VendorAccountMfa,
    )
    .expect("vendor operator should be valid")
}

fn employee_actor() -> AuthenticatedActorContext {
    AuthenticatedActorContext::new(
        actor_id("employee-audit-001"),
        Role::Employee,
        restricted_scope(&["fab-a"]),
        AuthenticationSource::CorporateSso,
    )
    .expect("employee actor should be valid")
}

fn payroll_operator() -> AuthenticatedActorContext {
    AuthenticatedActorContext::new(
        actor_id("payroll-audit-001"),
        Role::PayrollOperator,
        PlantScope::all(),
        AuthenticationSource::CorporateSso,
    )
    .expect("payroll operator should be valid")
}

fn vendor_id(value: &str) -> VendorId {
    VendorId::parse(value).expect("vendor id should be valid")
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

fn append_manual_evidence(
    audit_trail: &ImmutableAuditTrail,
    actor: &AuthenticatedActorContext,
    action: AuditAction,
    entity_type: AuditEntityType,
    entity_id: &str,
    epoch_day: i32,
) {
    audit_trail
        .append(AuditEvidenceWrite::new(
            AuditTimestamp::from_epoch_day(epoch_day),
            AuditIdentityLink::from_actor(actor, action.as_str()),
            action,
            AuditEntityRef::new(entity_type, entity_id).expect("entity id should be valid"),
            AuditCorrelationId::parse("case:test").expect("correlation should be valid"),
        ))
        .expect("manual audit append should succeed");
}

fn build_menu_item(vendor_id: &VendorId, delivery_epoch_day: i32) -> VendorMenuItem {
    VendorMenuItem::new(
        menu_item_id("menu-audit-001"),
        vendor_id.clone(),
        VendorMenuItemDraft::new(
            "Audit Bento",
            "Menu item used for audit investigation tests.",
            "BENTO",
            vec![MenuHealthTag::HighProtein],
            None,
            Money::new("TWD", 12000).expect("money should be valid"),
            30,
            delivery_epoch_day,
        )
        .expect("menu draft should be valid"),
    )
}

#[test]
fn investigation_query_filters_by_actor_action_entity_and_time() {
    ensure_test_otel_endpoint();
    let audit_trail =
        ImmutableAuditTrail::new(AuditRetentionPolicy::new(365).expect("policy should be valid"));
    let committee = committee_admin();
    let employee = employee_actor();
    let payroll = payroll_operator();

    append_manual_evidence(
        &audit_trail,
        &employee,
        AuditAction::CreateEmployeeOrder,
        AuditEntityType::Order,
        "ord-a",
        10,
    );
    append_manual_evidence(
        &audit_trail,
        &employee,
        AuditAction::UpdateEmployeeOrder,
        AuditEntityType::Order,
        "ord-a",
        11,
    );
    append_manual_evidence(
        &audit_trail,
        &payroll,
        AuditAction::MarkOrderRefunded,
        AuditEntityType::Settlement,
        "settle-a",
        12,
    );

    let gateway = HttpAuditInvestigationExecutionGateway::new(audit_trail.clone());
    let by_actor = gateway
        .execute_investigation_query(
            &committee,
            &AuditInvestigationFilter::default().with_actor_id(employee.actor_id().clone()),
        )
        .expect("actor filter query should succeed");
    assert_eq!(by_actor.len(), 2);

    let by_action = gateway
        .execute_investigation_query(
            &committee,
            &AuditInvestigationFilter::default().with_action(AuditAction::MarkOrderRefunded),
        )
        .expect("action filter query should succeed");
    assert_eq!(by_action.len(), 1);
    assert_eq!(by_action[0].audit_identity().actor_id(), payroll.actor_id());

    let by_entity = gateway
        .execute_investigation_query(
            &committee,
            &AuditInvestigationFilter::default()
                .with_entity(AuditEntityType::Order, "ord-a")
                .expect("entity filter should be valid"),
        )
        .expect("entity filter query should succeed");
    assert_eq!(by_entity.len(), 2);

    let by_time = gateway
        .execute_investigation_query(
            &committee,
            &AuditInvestigationFilter::default().with_time_range(
                Some(AuditTimestamp::from_epoch_day(11)),
                Some(AuditTimestamp::from_epoch_day(12)),
            ),
        )
        .expect("time filter query should succeed");
    assert_eq!(by_time.len(), 2);
}

#[test]
fn shared_vendor_correlation_links_order_verification_review_and_settlement_events() {
    ensure_test_otel_endpoint();
    let audit_trail =
        ImmutableAuditTrail::new(AuditRetentionPolicy::new(365).expect("policy should be valid"));
    let committee = committee_admin();
    let vendor_actor = vendor_operator();
    let employee = employee_actor();
    let payroll = payroll_operator();
    let vendor = vendor_id("ven-audit-correlation");

    let mut compliance = VendorComplianceLifecycle::with_audit_trail(
        HistoryRetentionPolicy::default(),
        audit_trail.clone(),
    );
    let category = VendorCategory::parse("RESTAURANT").expect("category should be valid");
    let template_id =
        DocumentTemplateId::parse("tmpl-audit-license").expect("template id should be valid");

    compliance
        .upsert_document_template(
            &committee,
            ComplianceDocumentTemplate::new(
                template_id.clone(),
                category.clone(),
                "Business License",
                true,
                365,
                vec![30, 7],
                0,
            )
            .expect("template should be valid"),
        )
        .expect("template upsert should succeed");
    compliance
        .register_vendor_application(
            &vendor_actor,
            vendor.clone(),
            "Vendor Correlation",
            category,
            ComplianceDate::from_epoch_day(10),
        )
        .expect("vendor registration should succeed");
    compliance
        .submit_document(
            &vendor_actor,
            &vendor,
            &template_id,
            VendorDocumentSubmission::new(
                "s3://evidence/docs/vendor-audit-license.pdf",
                ComplianceDate::from_epoch_day(10),
                ComplianceDate::from_epoch_day(300),
            )
            .expect("submission should be valid"),
        )
        .expect("document submit should succeed");
    compliance
        .review_application(
            &committee,
            &vendor,
            VendorReviewDecision::Approved,
            "Vendor compliance package approved.",
            ComplianceDate::from_epoch_day(11),
        )
        .expect("review should succeed");

    let menu_policy = MenuSupplyPolicy::with_audit_trail(
        OrderingGovernancePolicy::default(),
        audit_trail.clone(),
    );
    menu_policy
        .upsert_menu_item(&vendor_actor, build_menu_item(&vendor, 15))
        .expect("menu item upsert should succeed");

    let order = order_id("ord-audit-correlation-001");
    menu_policy
        .create_order(
            &employee,
            order.clone(),
            &vendor,
            &plant_id("fab-a"),
            15,
            vec![
                OrderLineItemRequest::new(menu_item_id("menu-audit-001"), 1, vec![])
                    .expect("line item should be valid"),
            ],
            taipei_moment(14, 600),
        )
        .expect("order create should succeed");
    menu_policy
        .update_order(
            &employee,
            &order,
            OrderMutation::MarkFulfilled,
            taipei_moment(15, 700),
        )
        .expect("pickup verification state transition should succeed");
    menu_policy
        .update_order(
            &payroll,
            &order,
            OrderMutation::MarkRefundPending,
            taipei_moment(15, 710),
        )
        .expect("refund pending transition should succeed");
    menu_policy
        .update_order(
            &payroll,
            &order,
            OrderMutation::MarkRefunded,
            taipei_moment(15, 720),
        )
        .expect("refund completion transition should succeed");

    let correlation_filter = AuditInvestigationFilter::default().with_correlation_id(
        AuditCorrelationId::for_vendor(vendor.as_str()).expect("vendor correlation should parse"),
    );
    let gateway = HttpAuditInvestigationExecutionGateway::new(audit_trail);
    let events = gateway
        .execute_investigation_query(&committee, &correlation_filter)
        .expect("correlation query should succeed");
    let actions = events
        .iter()
        .map(|event| event.action())
        .collect::<BTreeSet<_>>();
    assert!(actions.contains(&AuditAction::CreateEmployeeOrder));
    assert!(actions.contains(&AuditAction::VerifyPickupOrder));
    assert!(actions.contains(&AuditAction::ReviewVendorApplication));
    assert!(actions.contains(&AuditAction::MarkOrderRefunded));
}

#[test]
fn retention_purge_removes_expired_evidence_and_requires_committee_role() {
    ensure_test_otel_endpoint();
    let audit_trail =
        ImmutableAuditTrail::new(AuditRetentionPolicy::new(10).expect("policy should be valid"));
    let committee = committee_admin();
    let vendor_actor = vendor_operator();

    append_manual_evidence(
        &audit_trail,
        &vendor_actor,
        AuditAction::UpsertVendorMenuItem,
        AuditEntityType::MenuItem,
        "menu-old-a",
        1,
    );
    append_manual_evidence(
        &audit_trail,
        &vendor_actor,
        AuditAction::UpsertVendorMenuItem,
        AuditEntityType::MenuItem,
        "menu-old-b",
        9,
    );
    append_manual_evidence(
        &audit_trail,
        &vendor_actor,
        AuditAction::UpsertVendorMenuItem,
        AuditEntityType::MenuItem,
        "menu-keep-a",
        12,
    );

    let gateway = HttpAuditInvestigationExecutionGateway::new(audit_trail.clone());
    let report = gateway
        .execute_retention_purge(&committee, AuditTimestamp::from_epoch_day(20))
        .expect("retention purge should succeed");
    assert_eq!(report.purged_events, 2);

    let remaining = gateway
        .execute_investigation_query(&committee, &AuditInvestigationFilter::default())
        .expect("query should succeed");
    assert_eq!(remaining.len(), 1);
    assert_eq!(remaining[0].entity().entity_id(), "menu-keep-a");

    let employee = employee_actor();
    let unauthorized =
        gateway.execute_retention_purge(&employee, AuditTimestamp::from_epoch_day(20));
    assert!(matches!(
        unauthorized,
        Err(AuditTrailError::UnauthorizedInvestigatorRole {
            actual: Role::Employee
        })
    ));
}
