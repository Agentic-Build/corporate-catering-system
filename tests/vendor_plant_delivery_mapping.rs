use std::fs;
use std::time::{SystemTime, UNIX_EPOCH};

use corporate_catering_system::identity::{
    ActorId, AuthenticatedActorContext, AuthenticationSource, PlantId, PlantScope, Role,
};
use corporate_catering_system::vendor_compliance::{
    ComplianceDate, ComplianceDocumentTemplate, DocumentTemplateId, HistoryRetentionPolicy,
    VendorCategory, VendorComplianceLifecycle, VendorDocumentSubmission, VendorId,
    VendorReviewDecision,
};
use corporate_catering_system::vendor_delivery_mapping::{
    DeliverabilityApi, DeliveryMappingAuditKind, DeliveryMappingId, DeliveryRuleEffect,
    ServiceWindow, TaipeiBusinessMoment, VendorPlantDeliveryError, VendorPlantDeliveryMapping,
    VendorPlantDeliveryPolicy,
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
        actor_id("committee-delivery-001"),
        Role::CommitteeAdmin,
        PlantScope::all(),
        AuthenticationSource::CorporateSso,
    )
    .expect("committee admin should be valid")
}

fn vendor_operator() -> AuthenticatedActorContext {
    AuthenticatedActorContext::new(
        actor_id("vendor-delivery-operator-001"),
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

fn required_template_for(category: &VendorCategory) -> ComplianceDocumentTemplate {
    ComplianceDocumentTemplate::new(
        template_id("tmpl-vendor-delivery-license"),
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
            &template_id("tmpl-vendor-delivery-license"),
            VendorDocumentSubmission::new(
                format!(
                    "s3://compliance-evidence/compliance-documents/{}/docs/524288-deadbeef-vendor-license.pdf",
                    vendor_id.as_str()
                ),
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

fn unique_temp_file_path(prefix: &str) -> std::path::PathBuf {
    let unique_suffix = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .expect("system clock must be after unix epoch")
        .as_nanos();
    std::env::temp_dir().join(format!("{prefix}-{unique_suffix}.json"))
}

#[test]
fn employee_visible_vendor_list_is_filtered_by_plant_and_active_service_window() {
    let committee = committee_admin();
    let vendor_actor = vendor_operator();
    let category = vendor_category("RESTAURANT");
    let mut lifecycle = VendorComplianceLifecycle::new(HistoryRetentionPolicy::default());
    lifecycle
        .upsert_document_template(&committee, required_template_for(&category))
        .expect("template upsert should succeed");

    let vendor_alpha = vendor_id("ven-deliverya1");
    let vendor_bravo = vendor_id("ven-deliveryb1");
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
            taipei_moment(99, 900),
            mapping(
                "map-alpha-fab-a",
                &vendor_alpha,
                "fab-a",
                taipei_moment(100, 600),
                taipei_moment(100, 900),
                DeliveryRuleEffect::Allow,
                100,
            ),
        )
        .expect("alpha mapping should be upserted");
    policy
        .upsert_mapping(
            &committee,
            taipei_moment(99, 901),
            mapping(
                "map-bravo-fab-b",
                &vendor_bravo,
                "fab-b",
                taipei_moment(100, 600),
                taipei_moment(100, 900),
                DeliveryRuleEffect::Allow,
                100,
            ),
        )
        .expect("bravo mapping should be upserted");

    let browse = policy.employee_visible_vendor_ids_for_browse(
        &lifecycle,
        &plant_id("fab-a"),
        taipei_moment(100, 720),
    );
    assert_eq!(browse, vec![vendor_alpha.clone()]);

    let search = policy.employee_visible_vendor_ids_for_search(
        &lifecycle,
        &plant_id("fab-a"),
        taipei_moment(100, 720),
    );
    assert_eq!(search, vec![vendor_alpha.clone()]);

    assert!(policy
        .employee_visible_vendor_ids_for_browse(
            &lifecycle,
            &plant_id("fab-a"),
            taipei_moment(100, 900),
        )
        .is_empty());
}

#[test]
fn admin_mapping_changes_take_effect_immediately_and_are_auditable() {
    let committee = committee_admin();
    let vendor_actor = vendor_operator();
    let category = vendor_category("RESTAURANT");
    let mut lifecycle = VendorComplianceLifecycle::new(HistoryRetentionPolicy::default());
    lifecycle
        .upsert_document_template(&committee, required_template_for(&category))
        .expect("template upsert should succeed");

    let vendor = vendor_id("ven-deliveryc1");
    activate_vendor(
        &mut lifecycle,
        &committee,
        &vendor_actor,
        &vendor,
        &category,
        "Vendor Charlie",
    );

    let mut policy = VendorPlantDeliveryPolicy::new();
    let active_start = taipei_moment(120, 540);
    let active_end = taipei_moment(120, 900);
    let effective_at = taipei_moment(120, 700);
    policy
        .upsert_mapping(
            &committee,
            taipei_moment(120, 500),
            mapping(
                "map-charlie-primary",
                &vendor,
                "fab-a",
                active_start,
                active_end,
                DeliveryRuleEffect::Allow,
                10,
            ),
        )
        .expect("allow mapping should be upserted");
    policy
        .ensure_vendor_deliverable_for_order(&lifecycle, &vendor, &plant_id("fab-a"), effective_at)
        .expect("order deliverability should be allowed before rule change");

    policy
        .upsert_mapping(
            &committee,
            taipei_moment(120, 510),
            mapping(
                "map-charlie-primary",
                &vendor,
                "fab-a",
                active_start,
                active_end,
                DeliveryRuleEffect::Deny,
                10,
            ),
        )
        .expect("deny mapping should overwrite immediately");

    let error = policy
        .ensure_vendor_deliverable_for_order(&lifecycle, &vendor, &plant_id("fab-a"), effective_at)
        .expect_err("order should be blocked immediately after deny upsert");
    assert!(matches!(
        error,
        VendorPlantDeliveryError::DeliverabilityDenied {
            ref mapping_id,
            api: DeliverabilityApi::Order,
            ..
        } if mapping_id.as_str() == "map-charlie-primary"
    ));

    let audit_log = policy.audit_log();
    assert_eq!(audit_log.len(), 2);
    assert_eq!(audit_log[0].kind(), DeliveryMappingAuditKind::Upserted);
    assert_eq!(audit_log[1].kind(), DeliveryMappingAuditKind::Upserted);
    assert_eq!(
        audit_log[1].audit_identity().actor_id(),
        committee.actor_id()
    );
    assert_eq!(
        audit_log[1].audit_identity().operation_id(),
        "upsertVendorPlantDeliveryMapping"
    );
}

#[test]
fn deliverability_checks_are_enforced_for_browse_search_and_order_apis() {
    let committee = committee_admin();
    let vendor_actor = vendor_operator();
    let category = vendor_category("RESTAURANT");
    let mut lifecycle = VendorComplianceLifecycle::new(HistoryRetentionPolicy::default());
    lifecycle
        .upsert_document_template(&committee, required_template_for(&category))
        .expect("template upsert should succeed");

    let vendor = vendor_id("ven-deliveryd1");
    activate_vendor(
        &mut lifecycle,
        &committee,
        &vendor_actor,
        &vendor,
        &category,
        "Vendor Delta",
    );

    let mut policy = VendorPlantDeliveryPolicy::new();
    let at = taipei_moment(140, 700);
    policy
        .upsert_mapping(
            &committee,
            taipei_moment(140, 600),
            mapping(
                "map-delta-deny",
                &vendor,
                "fab-a",
                taipei_moment(140, 660),
                taipei_moment(140, 780),
                DeliveryRuleEffect::Deny,
                30,
            ),
        )
        .expect("deny mapping should be upserted");

    let browse_error = policy
        .ensure_vendor_deliverable_for_browse(&lifecycle, &vendor, &plant_id("fab-a"), at)
        .expect_err("browse API must enforce deny rule");
    assert!(matches!(
        browse_error,
        VendorPlantDeliveryError::DeliverabilityDenied {
            api: DeliverabilityApi::Browse,
            ..
        }
    ));

    let search_error = policy
        .ensure_vendor_deliverable_for_search(&lifecycle, &vendor, &plant_id("fab-a"), at)
        .expect_err("search API must enforce deny rule");
    assert!(matches!(
        search_error,
        VendorPlantDeliveryError::DeliverabilityDenied {
            api: DeliverabilityApi::Search,
            ..
        }
    ));

    let order_error = policy
        .ensure_vendor_deliverable_for_order(&lifecycle, &vendor, &plant_id("fab-a"), at)
        .expect_err("order API must enforce deny rule");
    assert!(matches!(
        order_error,
        VendorPlantDeliveryError::DeliverabilityDenied {
            api: DeliverabilityApi::Order,
            ..
        }
    ));

    assert!(policy
        .employee_visible_vendor_ids_for_browse(&lifecycle, &plant_id("fab-a"), at)
        .is_empty());
    assert!(policy
        .employee_visible_vendor_ids_for_search(&lifecycle, &plant_id("fab-a"), at)
        .is_empty());
}

#[test]
fn overlapping_rules_follow_precedence_then_latest_revision_deterministically() {
    let committee = committee_admin();
    let vendor_actor = vendor_operator();
    let category = vendor_category("RESTAURANT");
    let mut lifecycle = VendorComplianceLifecycle::new(HistoryRetentionPolicy::default());
    lifecycle
        .upsert_document_template(&committee, required_template_for(&category))
        .expect("template upsert should succeed");

    let vendor = vendor_id("ven-deliverye1");
    activate_vendor(
        &mut lifecycle,
        &committee,
        &vendor_actor,
        &vendor,
        &category,
        "Vendor Echo",
    );

    let mut policy = VendorPlantDeliveryPolicy::new();
    let starts_at = taipei_moment(160, 600);
    let ends_at = taipei_moment(160, 900);
    let evaluated_at = taipei_moment(160, 700);

    policy
        .upsert_mapping(
            &committee,
            taipei_moment(160, 500),
            mapping(
                "map-echo-low-allow",
                &vendor,
                "fab-a",
                starts_at,
                ends_at,
                DeliveryRuleEffect::Allow,
                10,
            ),
        )
        .expect("low precedence allow mapping should be upserted");
    policy
        .upsert_mapping(
            &committee,
            taipei_moment(160, 501),
            mapping(
                "map-echo-high-deny",
                &vendor,
                "fab-a",
                starts_at,
                ends_at,
                DeliveryRuleEffect::Deny,
                20,
            ),
        )
        .expect("high precedence deny mapping should be upserted");

    assert!(matches!(
        policy.ensure_vendor_deliverable_for_order(
            &lifecycle,
            &vendor,
            &plant_id("fab-a"),
            evaluated_at
        ),
        Err(VendorPlantDeliveryError::DeliverabilityDenied { .. })
    ));

    policy
        .upsert_mapping(
            &committee,
            taipei_moment(160, 502),
            mapping(
                "map-echo-high-allow",
                &vendor,
                "fab-a",
                starts_at,
                ends_at,
                DeliveryRuleEffect::Allow,
                20,
            ),
        )
        .expect("later high precedence allow mapping should be upserted");

    policy
        .ensure_vendor_deliverable_for_order(&lifecycle, &vendor, &plant_id("fab-a"), evaluated_at)
        .expect("latest mapping with same precedence should win deterministically");
}

#[test]
fn taipei_business_time_is_evaluated_with_fixed_plus_8_offset() {
    let at_unix_epoch = TaipeiBusinessMoment::from_utc_unix_seconds(0)
        .expect("unix epoch should convert to a Taipei business moment");
    assert_eq!(at_unix_epoch.epoch_day(), 0);
    assert_eq!(at_unix_epoch.minute_of_day(), 8 * 60);

    let one_second_before_epoch = TaipeiBusinessMoment::from_utc_unix_seconds(-1)
        .expect("one second before epoch should still be representable");
    assert_eq!(one_second_before_epoch.epoch_day(), 0);
    assert_eq!(one_second_before_epoch.minute_of_day(), (8 * 60) - 1);

    let service_window = ServiceWindow::new(taipei_moment(0, 8 * 60), taipei_moment(0, 9 * 60))
        .expect("service window should be valid");
    assert!(service_window.is_active_at(at_unix_epoch));
    assert!(!service_window.is_active_at(one_second_before_epoch));
    assert!(!service_window.is_active_at(
        TaipeiBusinessMoment::from_utc_unix_seconds(3600).expect("timestamp should convert")
    ));
}

#[test]
fn json_storage_persists_mappings_and_audit_history_across_reloads() {
    let committee = committee_admin();
    let vendor = vendor_id("ven-deliveryg1");
    let storage_path = unique_temp_file_path("vendor-delivery-policy");

    let mut policy = VendorPlantDeliveryPolicy::with_json_storage(&storage_path)
        .expect("policy should initialize with json storage");
    policy
        .upsert_mapping(
            &committee,
            taipei_moment(200, 600),
            mapping(
                "map-persist-primary",
                &vendor,
                "fab-a",
                taipei_moment(200, 660),
                taipei_moment(200, 780),
                DeliveryRuleEffect::Allow,
                5,
            ),
        )
        .expect("mapping should be persisted");
    policy
        .remove_mapping(
            &committee,
            taipei_moment(200, 601),
            &vendor,
            &mapping_id("map-persist-primary"),
        )
        .expect("mapping removal should be persisted");

    let reloaded_policy = VendorPlantDeliveryPolicy::with_json_storage(&storage_path)
        .expect("policy should reload from json storage");
    assert!(reloaded_policy.mappings_for_vendor(&vendor).is_empty());
    assert_eq!(reloaded_policy.audit_log().len(), 2);
    assert_eq!(
        reloaded_policy.audit_log()[1]
            .audit_identity()
            .operation_id(),
        "deleteVendorPlantDeliveryMapping"
    );

    fs::remove_file(&storage_path).expect("temporary persisted policy file should be removable");
}

#[test]
fn only_committee_admin_can_mutate_vendor_delivery_mappings() {
    let vendor_actor = vendor_operator();
    let mut policy = VendorPlantDeliveryPolicy::new();
    let result = policy.upsert_mapping(
        &vendor_actor,
        taipei_moment(1, 1),
        mapping(
            "map-role-denied",
            &vendor_id("ven-deliveryf1"),
            "fab-a",
            taipei_moment(1, 10),
            taipei_moment(1, 20),
            DeliveryRuleEffect::Allow,
            1,
        ),
    );

    assert!(matches!(
        result,
        Err(VendorPlantDeliveryError::UnauthorizedRole {
            expected: Role::CommitteeAdmin,
            actual: Role::VendorOperator,
        })
    ));
}
