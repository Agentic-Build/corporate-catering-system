use corporate_catering_system::audit::{AuditRetentionPolicy, ImmutableAuditTrail};
use corporate_catering_system::identity::{
    ActorId, AuthenticatedActorContext, AuthenticationSource, PlantId, PlantScope, Role,
};
use corporate_catering_system::persistence::VendorComplianceSqlRepository;
use corporate_catering_system::vendor_compliance::{
    ComplianceDate, ComplianceDocumentTemplate, DocumentTemplateId, HistoryRetentionPolicy,
    VendorCategory, VendorComplianceStatus, VendorDocumentSubmission, VendorId,
    VendorReviewDecision,
};
use sqlx::postgres::PgPoolOptions;
use testcontainers_modules::postgres::Postgres;
use testcontainers_modules::testcontainers::runners::AsyncRunner;
use testcontainers_modules::testcontainers::ImageExt;

fn actor_id(value: &str) -> ActorId {
    ActorId::parse(value).expect("actor id should be valid")
}

fn plant_id(value: &str) -> PlantId {
    PlantId::parse(value).expect("plant id should be valid")
}

fn committee_admin() -> AuthenticatedActorContext {
    AuthenticatedActorContext::new(
        actor_id("committee-sqlx-001"),
        Role::CommitteeAdmin,
        PlantScope::all(),
        AuthenticationSource::CorporateSso,
    )
    .expect("committee admin context should be valid")
}

fn vendor_operator() -> AuthenticatedActorContext {
    AuthenticatedActorContext::new(
        actor_id("vendor-sqlx-operator-001"),
        Role::VendorOperator,
        PlantScope::restricted(vec![plant_id("fab-a")]).expect("restricted scope should be valid"),
        AuthenticationSource::VendorAccountMfa,
    )
    .expect("vendor operator context should be valid")
}

#[tokio::test]
async fn vendor_compliance_domain_flow_persists_on_real_postgres_with_transactions() {
    std::env::set_var("OTEL_EXPORTER_OTLP_ENDPOINT", "http://127.0.0.1:4317");
    let postgres = Postgres::default()
        .with_tag("16-alpine")
        .start()
        .await
        .expect("testcontainers postgres should start");
    let database_url = format!(
        "postgres://postgres:postgres@{}:{}/postgres",
        postgres
            .get_host()
            .await
            .expect("postgres host should resolve"),
        postgres
            .get_host_port_ipv4(5432)
            .await
            .expect("postgres mapped port should resolve"),
    );

    let pool = PgPoolOptions::new()
        .max_connections(5)
        .connect(database_url.as_str())
        .await
        .expect("postgres pool should connect");
    sqlx::migrate!("./migrations")
        .run(&pool)
        .await
        .expect("migrations should apply");

    let repository = VendorComplianceSqlRepository::new(pool);
    let retention_policy = HistoryRetentionPolicy::default();
    let audit_trail = ImmutableAuditTrail::new(AuditRetentionPolicy::default());
    let committee = committee_admin();
    let vendor_actor = vendor_operator();

    let mut lifecycle =
        corporate_catering_system::vendor_compliance::VendorComplianceLifecycle::with_audit_trail(
            retention_policy.clone(),
            audit_trail.clone(),
        );
    let category = VendorCategory::parse("RESTAURANT").expect("category should be valid");
    let template_id =
        DocumentTemplateId::parse("tmpl-sql-license").expect("template id should be valid");
    lifecycle
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

    let vendor_id = VendorId::parse("ven-sqlx-integration-01").expect("vendor id should be valid");
    lifecycle
        .register_vendor_application(
            &vendor_actor,
            vendor_id.clone(),
            "SQLX Integration Vendor",
            category,
            ComplianceDate::from_epoch_day(0),
        )
        .expect("vendor registration should succeed");
    lifecycle
        .submit_document(
            &vendor_actor,
            &vendor_id,
            &template_id,
            VendorDocumentSubmission::new(
                "s3://evidence/docs/sqlx-integration-license.pdf",
                ComplianceDate::from_epoch_day(0),
                ComplianceDate::from_epoch_day(20),
            )
            .expect("document submission should be valid"),
        )
        .expect("document submission should succeed");
    lifecycle
        .review_application(
            &committee,
            &vendor_id,
            VendorReviewDecision::Approved,
            "Compliance package is complete and valid.",
            ComplianceDate::from_epoch_day(1),
        )
        .expect("approval should succeed");

    repository
        .save_lifecycle(&lifecycle)
        .await
        .expect("compliance lifecycle should persist");

    let loaded = repository
        .load_lifecycle(retention_policy.clone(), audit_trail.clone())
        .await
        .expect("load should succeed")
        .expect("persisted lifecycle should exist");
    assert_eq!(
        loaded
            .vendor(&vendor_id)
            .expect("vendor should load")
            .status(),
        VendorComplianceStatus::Active
    );

    let (_updated, run_result) = repository
        .mutate_lifecycle(retention_policy, audit_trail, |state| {
            state.run_lifecycle(&committee, ComplianceDate::from_epoch_day(21))
        })
        .await
        .expect("transactional lifecycle mutation should succeed");
    assert_eq!(run_result.suspensions.len(), 1);

    let reloaded = repository
        .load_lifecycle(
            HistoryRetentionPolicy::default(),
            ImmutableAuditTrail::new(AuditRetentionPolicy::default()),
        )
        .await
        .expect("reload should succeed")
        .expect("lifecycle should still exist after mutation");
    assert_eq!(
        reloaded
            .vendor(&vendor_id)
            .expect("vendor should still exist")
            .status(),
        VendorComplianceStatus::Suspended
    );
}
