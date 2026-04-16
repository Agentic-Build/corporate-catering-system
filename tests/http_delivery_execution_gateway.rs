use corporate_catering_system::identity::{
    ActorId, AuthenticatedActorContext, AuthenticationSource, PlantId, PlantScope, Role,
};
use corporate_catering_system::transport::http::HttpDeliveryExecutionGateway;
use corporate_catering_system::vendor_compliance::{
    ComplianceDate, ComplianceDocumentTemplate, DocumentTemplateId, HistoryRetentionPolicy,
    VendorCategory, VendorComplianceLifecycle, VendorDocumentSubmission, VendorId,
    VendorReviewDecision,
};
use corporate_catering_system::vendor_delivery_mapping::{
    DeliverabilityApi, DeliveryMappingId, DeliveryRuleEffect, ServiceWindow, TaipeiBusinessMoment,
    VendorPlantDeliveryError, VendorPlantDeliveryMapping, VendorPlantDeliveryPolicy,
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
        actor_id("committee-http-delivery-001"),
        Role::CommitteeAdmin,
        PlantScope::all(),
        AuthenticationSource::CorporateSso,
    )
    .expect("committee admin should be valid")
}

fn vendor_operator() -> AuthenticatedActorContext {
    AuthenticatedActorContext::new(
        actor_id("vendor-http-delivery-operator-001"),
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

fn taipei_moment(epoch_day: i32, minute_of_day: u16) -> TaipeiBusinessMoment {
    TaipeiBusinessMoment::new(epoch_day, minute_of_day).expect("Taipei business moment is valid")
}

fn ensure_test_otel_endpoint() {
    std::env::set_var("OTEL_EXPORTER_OTLP_ENDPOINT", "http://127.0.0.1:4317");
}

fn required_template_for(category: &VendorCategory) -> ComplianceDocumentTemplate {
    ComplianceDocumentTemplate::new(
        template_id("tmpl-http-delivery-license"),
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
            &template_id("tmpl-http-delivery-license"),
            VendorDocumentSubmission::new(
                "s3://evidence/docs/http-delivery-license.pdf",
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

#[test]
fn http_gateway_filters_browse_and_search_vendor_visibility() {
    ensure_test_otel_endpoint();
    let committee = committee_admin();
    let vendor_actor = vendor_operator();
    let category = vendor_category("RESTAURANT");
    let mut lifecycle = VendorComplianceLifecycle::new(HistoryRetentionPolicy::default());
    lifecycle
        .upsert_document_template(&committee, required_template_for(&category))
        .expect("template upsert should succeed");

    let vendor_alpha = vendor_id("ven-http-deliv-a1");
    let vendor_bravo = vendor_id("ven-http-deliv-b1");
    activate_vendor(
        &mut lifecycle,
        &committee,
        &vendor_actor,
        &vendor_alpha,
        &category,
        "Vendor Alpha",
    );
    activate_vendor(
        &mut lifecycle,
        &committee,
        &vendor_actor,
        &vendor_bravo,
        &category,
        "Vendor Bravo",
    );

    let mut policy = VendorPlantDeliveryPolicy::new();
    policy
        .upsert_mapping(
            &committee,
            taipei_moment(10, 500),
            mapping(
                "map-http-alpha-fab-a",
                &vendor_alpha,
                "fab-a",
                taipei_moment(10, 600),
                taipei_moment(10, 800),
                DeliveryRuleEffect::Allow,
                10,
            ),
        )
        .expect("alpha mapping should be upserted");
    policy
        .upsert_mapping(
            &committee,
            taipei_moment(10, 501),
            mapping(
                "map-http-bravo-fab-b",
                &vendor_bravo,
                "fab-b",
                taipei_moment(10, 600),
                taipei_moment(10, 800),
                DeliveryRuleEffect::Allow,
                10,
            ),
        )
        .expect("bravo mapping should be upserted");

    let gateway = HttpDeliveryExecutionGateway::new(&lifecycle, &policy);
    let browse_visible =
        gateway.execute_list_employee_menus_for_browse(&plant_id("fab-a"), taipei_moment(10, 700));
    let search_visible =
        gateway.execute_list_employee_menus_for_search(&plant_id("fab-a"), taipei_moment(10, 700));

    assert_eq!(browse_visible, vec![vendor_alpha.clone()]);
    assert_eq!(search_visible, vec![vendor_alpha]);
}

#[test]
fn http_gateway_enforces_deliverability_for_create_and_update_order_paths() {
    ensure_test_otel_endpoint();
    let committee = committee_admin();
    let vendor_actor = vendor_operator();
    let category = vendor_category("RESTAURANT");
    let mut lifecycle = VendorComplianceLifecycle::new(HistoryRetentionPolicy::default());
    lifecycle
        .upsert_document_template(&committee, required_template_for(&category))
        .expect("template upsert should succeed");

    let vendor = vendor_id("ven-http-deliv-c1");
    activate_vendor(
        &mut lifecycle,
        &committee,
        &vendor_actor,
        &vendor,
        &category,
        "Vendor Charlie",
    );

    let mut policy = VendorPlantDeliveryPolicy::new();
    policy
        .upsert_mapping(
            &committee,
            taipei_moment(30, 500),
            mapping(
                "map-http-deny-order",
                &vendor,
                "fab-a",
                taipei_moment(30, 600),
                taipei_moment(30, 800),
                DeliveryRuleEffect::Deny,
                50,
            ),
        )
        .expect("deny mapping should be upserted");

    let gateway = HttpDeliveryExecutionGateway::new(&lifecycle, &policy);
    let create_error = gateway
        .execute_create_employee_order_deliverability_check(
            &vendor,
            &plant_id("fab-a"),
            taipei_moment(30, 700),
        )
        .expect_err("create order path should enforce deliverability");
    assert!(matches!(
        create_error,
        VendorPlantDeliveryError::DeliverabilityDenied {
            api: DeliverabilityApi::Order,
            ..
        }
    ));

    let update_error = gateway
        .execute_update_employee_order_deliverability_check(
            &vendor,
            &plant_id("fab-a"),
            taipei_moment(30, 700),
        )
        .expect_err("update order path should enforce deliverability");
    assert!(matches!(
        update_error,
        VendorPlantDeliveryError::DeliverabilityDenied {
            api: DeliverabilityApi::Order,
            ..
        }
    ));
}
