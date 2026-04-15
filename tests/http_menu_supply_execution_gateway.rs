use corporate_catering_system::identity::{
    ActorId, AuthenticatedActorContext, AuthenticationSource, PlantId, PlantScope, Role,
};
use corporate_catering_system::menu_supply_window::{
    MenuHealthTag, MenuImageUrl, MenuItemId, MenuSupplyPolicy, Money, OrderId,
    OrderLineItemRequest, OrderMutation, SpecialRequest, VendorMenuItem, VendorMenuItemDraft,
    VendorOrderingPolicyOverride,
};
use corporate_catering_system::transport::http::{
    HttpOrderExecutionError, HttpOrderingExecutionGateway, HttpVendorMenuExecutionGateway,
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

fn committee_admin() -> AuthenticatedActorContext {
    AuthenticatedActorContext::new(
        actor_id("committee-http-menu-supply"),
        Role::CommitteeAdmin,
        PlantScope::all(),
        AuthenticationSource::CorporateSso,
    )
    .expect("committee admin should be valid")
}

fn vendor_operator() -> AuthenticatedActorContext {
    AuthenticatedActorContext::new(
        actor_id("vendor-http-menu-supply"),
        Role::VendorOperator,
        restricted_scope(&["fab-a"]),
        AuthenticationSource::VendorAccountMfa,
    )
    .expect("vendor operator should be valid")
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

fn required_template_for(category: &VendorCategory) -> ComplianceDocumentTemplate {
    ComplianceDocumentTemplate::new(
        template_id("tmpl-http-menu-supply-license"),
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
    vendor_actor: &AuthenticatedActorContext,
    vendor_id: &VendorId,
    category: &VendorCategory,
    display_name: &str,
) {
    lifecycle
        .register_vendor_application(
            vendor_actor,
            vendor_id.clone(),
            display_name,
            category.clone(),
            ComplianceDate::from_epoch_day(0),
        )
        .expect("vendor application should be registered");
    lifecycle
        .submit_document(
            vendor_actor,
            vendor_id,
            &template_id("tmpl-http-menu-supply-license"),
            VendorDocumentSubmission::new(
                "s3://evidence/docs/http-menu-supply-license.pdf",
                ComplianceDate::from_epoch_day(0),
                ComplianceDate::from_epoch_day(300),
            )
            .expect("document submission should be valid"),
        )
        .expect("document submission should succeed");
    lifecycle
        .review_application(
            committee,
            vendor_id,
            VendorReviewDecision::Approved,
            "Vendor compliance package is complete and valid.",
            ComplianceDate::from_epoch_day(1),
        )
        .expect("vendor approval should succeed");
}

fn mapping(
    mapping_id_value: &str,
    vendor_id: &VendorId,
    plant_id_value: &str,
    starts_at: TaipeiBusinessMoment,
    ends_at: TaipeiBusinessMoment,
    effect: DeliveryRuleEffect,
    precedence: u16,
) -> VendorPlantDeliveryMapping {
    VendorPlantDeliveryMapping::new(
        mapping_id(mapping_id_value),
        vendor_id.clone(),
        plant_id(plant_id_value),
        ServiceWindow::new(starts_at, ends_at).expect("service window should be valid"),
        effect,
        precedence,
    )
}

fn menu_item_with_overrides(
    menu_item_id_value: &str,
    vendor_id: &VendorId,
    max_daily_quantity: u16,
    delivery_epoch_day: i32,
    policy_override: VendorOrderingPolicyOverride,
) -> VendorMenuItem {
    VendorMenuItem::new(
        menu_item_id(menu_item_id_value),
        vendor_id.clone(),
        VendorMenuItemDraft::new(
            "Herb Chicken Bowl",
            "Herb chicken bowl with grilled vegetables.",
            "BOWL",
            vec![MenuHealthTag::HighProtein],
            Some(
                MenuImageUrl::parse("https://cdn.example.com/menu/herb-chicken-bowl.jpg")
                    .expect("menu image URL should be valid"),
            ),
            Money::new("TWD", 16000).expect("money should be valid"),
            max_daily_quantity,
            delivery_epoch_day,
        )
        .expect("menu draft should be valid")
        .with_ordering_policy_overrides(policy_override),
    )
}

#[test]
fn http_ordering_gateway_enforces_deliverability_and_menu_supply_rules() {
    let committee = committee_admin();
    let vendor_actor = vendor_operator();
    let category = vendor_category("RESTAURANT");

    let mut lifecycle = VendorComplianceLifecycle::new(HistoryRetentionPolicy::default());
    lifecycle
        .upsert_document_template(&committee, required_template_for(&category))
        .expect("template upsert should succeed");

    let vendor = vendor_id("ven-http-menu-sup-a1");
    activate_vendor(
        &mut lifecycle,
        &committee,
        &vendor_actor,
        &vendor,
        &category,
        "Vendor Menu Supply A",
    );

    let mut delivery_policy = VendorPlantDeliveryPolicy::new();
    delivery_policy
        .upsert_mapping(
            &committee,
            taipei_moment(9, 480),
            mapping(
                "map-http-menu-supply-allow",
                &vendor,
                "fab-a",
                taipei_moment(9, 540),
                taipei_moment(20, 1200),
                DeliveryRuleEffect::Allow,
                10,
            ),
        )
        .expect("allow mapping should be upserted");

    let menu_supply = MenuSupplyPolicy::default();
    let vendor_menu_gateway = HttpVendorMenuExecutionGateway::new(&menu_supply);
    vendor_menu_gateway
        .execute_upsert_vendor_menu_item(
            &vendor_actor,
            menu_item_with_overrides(
                "menu-http-supply-a1",
                &vendor,
                5,
                11,
                VendorOrderingPolicyOverride {
                    preorder_open_days_ahead: Some(3),
                    modify_cancel_cutoff_minute_of_day: Some(15 * 60),
                },
            ),
        )
        .expect("menu item upsert should succeed");

    let gateway_before_deny =
        HttpOrderingExecutionGateway::new(&lifecycle, &delivery_policy, &menu_supply);

    gateway_before_deny
        .execute_create_employee_order(
            order_id("ord-http-supply-001"),
            &vendor,
            &plant_id("fab-a"),
            11,
            vec![OrderLineItemRequest::new(
                menu_item_id("menu-http-supply-a1"),
                1,
                vec![SpecialRequest::NoUtensils],
            )
            .expect("line item should be valid")],
            taipei_moment(10, 850),
        )
        .expect("create order should pass deliverability and supply checks");

    let update_after_cutoff_error = gateway_before_deny
        .execute_update_employee_order(
            &order_id("ord-http-supply-001"),
            &vendor,
            &plant_id("fab-a"),
            OrderMutation::Cancel,
            taipei_moment(10, 900),
        )
        .expect_err("order update should fail after overridden previous-day cutoff");
    assert!(matches!(
        update_after_cutoff_error,
        HttpOrderExecutionError::MenuSupply(_)
    ));

    let unsupported_mutation_error = gateway_before_deny
        .execute_update_employee_order(
            &order_id("ord-http-supply-001"),
            &vendor,
            &plant_id("fab-a"),
            OrderMutation::MarkRefundPending,
            taipei_moment(10, 851),
        )
        .expect_err("employee gateway must reject non-employee lifecycle operations");
    assert!(matches!(
        unsupported_mutation_error,
        HttpOrderExecutionError::UnsupportedEmployeeMutation {
            operation: "MARK_REFUND_PENDING",
        }
    ));

    delivery_policy
        .upsert_mapping(
            &committee,
            taipei_moment(10, 950),
            mapping(
                "map-http-menu-supply-deny",
                &vendor,
                "fab-a",
                taipei_moment(9, 540),
                taipei_moment(20, 1200),
                DeliveryRuleEffect::Deny,
                20,
            ),
        )
        .expect("deny mapping should be upserted with higher precedence");

    let gateway_after_deny =
        HttpOrderingExecutionGateway::new(&lifecycle, &delivery_policy, &menu_supply);
    let create_blocked_by_deliverability = gateway_after_deny
        .execute_create_employee_order(
            order_id("ord-http-supply-002"),
            &vendor,
            &plant_id("fab-a"),
            11,
            vec![
                OrderLineItemRequest::new(menu_item_id("menu-http-supply-a1"), 1, vec![])
                    .expect("line item should be valid"),
            ],
            taipei_moment(10, 960),
        )
        .expect_err("deny deliverability mapping should block create order");
    assert!(matches!(
        create_blocked_by_deliverability,
        HttpOrderExecutionError::Deliverability(_)
    ));
}
