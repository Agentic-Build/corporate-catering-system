use corporate_catering_system::identity::{
    ActorId, AuthenticatedActorContext, AuthenticationSource, PlantId, PlantScope, Role,
};
use corporate_catering_system::vendor_compliance::{
    ComplianceDate, ComplianceDocumentTemplate, ComplianceHistoryKind, DocumentTemplateId,
    HistoryRetentionPolicy, VendorCategory, VendorComplianceLifecycle, VendorComplianceStatus,
    VendorDocumentSubmission, VendorId, VendorReviewDecision,
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
        actor_id("committee-compliance-001"),
        Role::CommitteeAdmin,
        PlantScope::all(),
        AuthenticationSource::CorporateSso,
    )
    .expect("committee admin context should be valid")
}

fn vendor_operator() -> AuthenticatedActorContext {
    AuthenticatedActorContext::new(
        actor_id("vendor-compliance-operator-001"),
        Role::VendorOperator,
        restricted_scope(&["fab-a"]),
        AuthenticationSource::VendorAccountMfa,
    )
    .expect("vendor operator context should be valid")
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

fn required_template_for(category: &VendorCategory) -> ComplianceDocumentTemplate {
    ComplianceDocumentTemplate::new(
        template_id("tmpl-business-license"),
        category.clone(),
        "Business License",
        true,
        365,
        vec![30, 10, 7],
        0,
    )
    .expect("template should be valid")
}

fn ensure_test_otel_endpoint() {
    std::env::set_var("OTEL_EXPORTER_OTLP_ENDPOINT", "http://127.0.0.1:4317");
}

fn submit_required_document(
    lifecycle: &mut VendorComplianceLifecycle,
    vendor_actor: &AuthenticatedActorContext,
    vendor_id: &VendorId,
    submitted_on: i32,
    expires_on: i32,
) {
    lifecycle
        .submit_document(
            vendor_actor,
            vendor_id,
            &template_id("tmpl-business-license"),
            VendorDocumentSubmission::new(
                "s3://evidence/docs/business-license.pdf",
                ComplianceDate::from_epoch_day(submitted_on),
                ComplianceDate::from_epoch_day(expires_on),
            )
            .expect("document submission should be valid"),
        )
        .expect("document submission should succeed");
}

#[test]
fn committee_admin_can_review_vendor_applications_with_full_history() {
    ensure_test_otel_endpoint();
    let committee = committee_admin();
    let vendor_actor = vendor_operator();
    let category = vendor_category("RESTAURANT");
    let mut lifecycle = VendorComplianceLifecycle::new(HistoryRetentionPolicy::default());

    lifecycle
        .upsert_document_template(&committee, required_template_for(&category))
        .expect("template upsert should succeed");

    let vendor_alpha = vendor_id("ven-alpha001");
    lifecycle
        .register_vendor_application(
            &vendor_actor,
            vendor_alpha.clone(),
            "Vendor Alpha",
            category.clone(),
            ComplianceDate::from_epoch_day(0),
        )
        .expect("vendor alpha application should be registered");
    submit_required_document(&mut lifecycle, &vendor_actor, &vendor_alpha, 0, 120);
    lifecycle
        .review_application(
            &committee,
            &vendor_alpha,
            VendorReviewDecision::RequestFix,
            "Upload a newer business license scan.",
            ComplianceDate::from_epoch_day(1),
        )
        .expect("request-fix decision should succeed");
    submit_required_document(&mut lifecycle, &vendor_actor, &vendor_alpha, 2, 220);
    lifecycle
        .review_application(
            &committee,
            &vendor_alpha,
            VendorReviewDecision::Approved,
            "All mandatory documents are valid and complete.",
            ComplianceDate::from_epoch_day(3),
        )
        .expect("approval should succeed");

    let vendor_beta = vendor_id("ven-beta0001");
    lifecycle
        .register_vendor_application(
            &vendor_actor,
            vendor_beta.clone(),
            "Vendor Beta",
            category,
            ComplianceDate::from_epoch_day(4),
        )
        .expect("vendor beta application should be registered");
    submit_required_document(&mut lifecycle, &vendor_actor, &vendor_beta, 4, 180);
    lifecycle
        .review_application(
            &committee,
            &vendor_beta,
            VendorReviewDecision::Rejected,
            "Food safety registration details are inconsistent.",
            ComplianceDate::from_epoch_day(5),
        )
        .expect("rejection should succeed");

    let alpha_history = lifecycle
        .vendor(&vendor_alpha)
        .expect("vendor alpha should exist")
        .history();
    assert!(alpha_history.iter().any(|entry| matches!(
        entry.kind(),
        ComplianceHistoryKind::ReviewDecision {
            decision: VendorReviewDecision::RequestFix,
            ..
        }
    )));
    assert!(alpha_history.iter().any(|entry| matches!(
        entry.kind(),
        ComplianceHistoryKind::ReviewDecision {
            decision: VendorReviewDecision::Approved,
            ..
        }
    )));
    assert_eq!(
        lifecycle
            .vendor(&vendor_alpha)
            .expect("vendor alpha should exist")
            .status(),
        VendorComplianceStatus::Active
    );

    let beta_history = lifecycle
        .vendor(&vendor_beta)
        .expect("vendor beta should exist")
        .history();
    assert!(beta_history.iter().any(|entry| matches!(
        entry.kind(),
        ComplianceHistoryKind::ReviewDecision {
            decision: VendorReviewDecision::Rejected,
            ..
        }
    )));
    assert_eq!(
        lifecycle
            .vendor(&vendor_beta)
            .expect("vendor beta should exist")
            .status(),
        VendorComplianceStatus::Rejected
    );
}

#[test]
fn lifecycle_automation_emits_reminders_and_suspends_then_reinstates_vendors() {
    ensure_test_otel_endpoint();
    let committee = committee_admin();
    let vendor_actor = vendor_operator();
    let category = vendor_category("RESTAURANT");
    let mut lifecycle = VendorComplianceLifecycle::new(HistoryRetentionPolicy::default());

    lifecycle
        .upsert_document_template(&committee, required_template_for(&category))
        .expect("template upsert should succeed");

    let vendor = vendor_id("ven-lifecycle01");
    lifecycle
        .register_vendor_application(
            &vendor_actor,
            vendor.clone(),
            "Lifecycle Vendor",
            category,
            ComplianceDate::from_epoch_day(0),
        )
        .expect("vendor application should be registered");
    submit_required_document(&mut lifecycle, &vendor_actor, &vendor, 0, 130);
    lifecycle
        .review_application(
            &committee,
            &vendor,
            VendorReviewDecision::Approved,
            "Application approved with valid compliance package.",
            ComplianceDate::from_epoch_day(1),
        )
        .expect("approval should succeed");

    let reminder_30 = lifecycle
        .run_lifecycle(&committee, ComplianceDate::from_epoch_day(100))
        .expect("lifecycle run should succeed");
    assert_eq!(reminder_30.reminders.len(), 1);
    assert_eq!(reminder_30.suspensions.len(), 0);

    let reminder_30_duplicate = lifecycle
        .run_lifecycle(&committee, ComplianceDate::from_epoch_day(100))
        .expect("lifecycle run should succeed");
    assert_eq!(reminder_30_duplicate.reminders.len(), 0);

    let reminder_7 = lifecycle
        .run_lifecycle(&committee, ComplianceDate::from_epoch_day(123))
        .expect("lifecycle run should succeed");
    assert_eq!(reminder_7.reminders.len(), 1);

    let suspended = lifecycle
        .run_lifecycle(&committee, ComplianceDate::from_epoch_day(131))
        .expect("lifecycle run should succeed");
    assert_eq!(suspended.suspensions.len(), 1);
    assert_eq!(
        lifecycle
            .vendor(&vendor)
            .expect("vendor should exist")
            .status(),
        VendorComplianceStatus::Suspended
    );
    assert!(lifecycle.visible_vendor_ids_for_ordering().is_empty());

    submit_required_document(&mut lifecycle, &vendor_actor, &vendor, 132, 300);
    let reinstated = lifecycle
        .run_lifecycle(&committee, ComplianceDate::from_epoch_day(132))
        .expect("lifecycle run should succeed");
    assert_eq!(reinstated.reinstatements.len(), 1);
    assert_eq!(
        lifecycle
            .vendor(&vendor)
            .expect("vendor should exist")
            .status(),
        VendorComplianceStatus::Active
    );
    assert_eq!(lifecycle.visible_vendor_ids_for_ordering(), vec![&vendor]);
}

#[test]
fn retention_policy_prunes_history_and_deletes_rejected_vendor_records() {
    ensure_test_otel_endpoint();
    let committee = committee_admin();
    let vendor_actor = vendor_operator();
    let category = vendor_category("RESTAURANT");
    let retention_policy = HistoryRetentionPolicy::new(5, 3, 10).expect("policy should be valid");
    let mut lifecycle = VendorComplianceLifecycle::new(retention_policy);

    lifecycle
        .upsert_document_template(&committee, required_template_for(&category))
        .expect("template upsert should succeed");

    let rejected_vendor = vendor_id("ven-rejected01");
    lifecycle
        .register_vendor_application(
            &vendor_actor,
            rejected_vendor.clone(),
            "Rejected Vendor",
            category.clone(),
            ComplianceDate::from_epoch_day(0),
        )
        .expect("rejected vendor application should be registered");
    submit_required_document(&mut lifecycle, &vendor_actor, &rejected_vendor, 0, 120);
    lifecycle
        .review_application(
            &committee,
            &rejected_vendor,
            VendorReviewDecision::Rejected,
            "The submitted license does not match legal registration.",
            ComplianceDate::from_epoch_day(1),
        )
        .expect("rejection should succeed");

    let active_vendor = vendor_id("ven-active0001");
    lifecycle
        .register_vendor_application(
            &vendor_actor,
            active_vendor.clone(),
            "Active Vendor",
            category,
            ComplianceDate::from_epoch_day(0),
        )
        .expect("active vendor application should be registered");
    submit_required_document(&mut lifecycle, &vendor_actor, &active_vendor, 0, 20);
    lifecycle
        .review_application(
            &committee,
            &active_vendor,
            VendorReviewDecision::Approved,
            "Vendor meets onboarding compliance requirements.",
            ComplianceDate::from_epoch_day(1),
        )
        .expect("approval should succeed");
    lifecycle
        .run_lifecycle(&committee, ComplianceDate::from_epoch_day(10))
        .expect("lifecycle run should succeed");

    let first_prune = lifecycle
        .prune_history(&committee, ComplianceDate::from_epoch_day(12))
        .expect("history prune should succeed");
    assert!(first_prune.pruned_history_entries >= 3);
    assert_eq!(first_prune.deleted_vendor_records, 1);
    assert!(lifecycle.vendor(&rejected_vendor).is_none());
    assert_eq!(
        lifecycle
            .vendor(&active_vendor)
            .expect("active vendor should remain")
            .history()
            .len(),
        1
    );
    assert!(matches!(
        lifecycle
            .vendor(&active_vendor)
            .expect("active vendor should remain")
            .history()[0]
            .kind(),
        ComplianceHistoryKind::ExpiryReminderIssued { .. }
    ));

    let second_prune = lifecycle
        .prune_history(&committee, ComplianceDate::from_epoch_day(15))
        .expect("history prune should succeed");
    assert_eq!(second_prune.deleted_vendor_records, 0);
    assert_eq!(
        lifecycle
            .vendor(&active_vendor)
            .expect("active vendor should remain")
            .history()
            .len(),
        0
    );
}
