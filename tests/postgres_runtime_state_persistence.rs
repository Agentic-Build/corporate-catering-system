use std::collections::HashSet;

use corporate_catering_system::anomaly_alert::{
    AnomalyAlertQuery, AnomalyAlertWorkflow, AnomalyAlertWorkflowSnapshot, AnomalySignalSnapshot,
};
use corporate_catering_system::audit::{AuditRetentionPolicy, AuditTimestamp, ImmutableAuditTrail};
use corporate_catering_system::identity::{
    ActorId, AuthenticatedActorContext, AuthenticationSource, EmploymentStatus, PlantId,
    PlantScope, Role,
};
use corporate_catering_system::menu_supply_window::{
    MenuHealthTag, MenuImageUrl, MenuItemId, MenuSupplyPolicy, MenuSupplyPolicySnapshot, Money,
    OrderId, OrderLineItemRequest, OrderRetentionPolicy, VendorMenuItem, VendorMenuItemDraft,
};
use corporate_catering_system::payroll::{
    PayrollLedgerService, PayrollLedgerServiceSnapshot, PayrollLedgerSourceKind,
    PayrollLedgerSourceRef, PayrollRetentionPolicy,
};
use corporate_catering_system::persistence::{
    allocate_order_id_hex_from_postgres, JsonStatePersistenceError, SqlJsonStateRepository,
};
use corporate_catering_system::transport::http::HttpOrderingExecutionGateway;
use corporate_catering_system::vendor_compliance::{
    ComplianceDate, ComplianceDocumentTemplate, DocumentTemplateId, HistoryRetentionPolicy,
    VendorCategory, VendorComplianceLifecycle, VendorDocumentSubmission, VendorId,
    VendorReviewDecision,
};
use corporate_catering_system::vendor_delivery_mapping::{
    DeliveryMappingId, DeliveryRuleEffect, ServiceWindow, TaipeiBusinessMoment,
    VendorPlantDeliveryMapping, VendorPlantDeliveryPolicy,
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
        actor_id("committee-runtime-sqlx-001"),
        Role::CommitteeAdmin,
        PlantScope::all(),
        AuthenticationSource::CorporateSso,
    )
    .expect("committee admin context should be valid")
}

fn vendor_operator(plant: &PlantId) -> AuthenticatedActorContext {
    AuthenticatedActorContext::new(
        actor_id("vendor-runtime-sqlx-001"),
        Role::VendorOperator,
        PlantScope::restricted(vec![plant.clone()]).expect("restricted scope should be valid"),
        AuthenticationSource::VendorAccountMfa,
    )
    .expect("vendor operator context should be valid")
}

fn employee_actor(plant: &PlantId) -> AuthenticatedActorContext {
    AuthenticatedActorContext::new(
        actor_id("emp-runtime-sqlx-001"),
        Role::Employee,
        PlantScope::restricted(vec![plant.clone()]).expect("restricted scope should be valid"),
        AuthenticationSource::CorporateSso,
    )
    .expect("employee actor context should be valid")
}

fn build_approved_compliance_lifecycle(
    audit_trail: ImmutableAuditTrail,
    committee: &AuthenticatedActorContext,
    vendor_actor: &AuthenticatedActorContext,
    vendor_id: &VendorId,
    delivery_epoch_day: i32,
) -> VendorComplianceLifecycle {
    let mut lifecycle =
        VendorComplianceLifecycle::with_audit_trail(HistoryRetentionPolicy::default(), audit_trail);
    let category = VendorCategory::parse("RESTAURANT").expect("category should be valid");
    let template_id =
        DocumentTemplateId::parse("tmpl-runtime-sql-license").expect("template id should be valid");
    lifecycle
        .upsert_document_template(
            committee,
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
    lifecycle
        .register_vendor_application(
            vendor_actor,
            vendor_id.clone(),
            "Runtime SQLX Vendor",
            category,
            ComplianceDate::from_epoch_day(delivery_epoch_day.saturating_sub(5)),
        )
        .expect("vendor registration should succeed");
    lifecycle
        .submit_document(
            vendor_actor,
            vendor_id,
            &template_id,
            VendorDocumentSubmission::new(
                "s3://evidence/docs/runtime-sql-license.pdf",
                ComplianceDate::from_epoch_day(delivery_epoch_day.saturating_sub(5)),
                ComplianceDate::from_epoch_day(delivery_epoch_day.saturating_add(300)),
            )
            .expect("document submission should be valid"),
        )
        .expect("document submission should succeed");
    lifecycle
        .review_application(
            committee,
            vendor_id,
            VendorReviewDecision::Approved,
            "approved",
            ComplianceDate::from_epoch_day(delivery_epoch_day.saturating_sub(4)),
        )
        .expect("approval should succeed");
    lifecycle
}

#[tokio::test]
async fn order_id_allocator_is_unique_across_pool_restarts_on_real_postgres() {
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

    let mut generated = HashSet::new();
    for _ in 0..64 {
        let suffix = allocate_order_id_hex_from_postgres(&pool)
            .await
            .expect("order id suffix should allocate");
        assert_eq!(suffix.len(), 32, "order id suffix should be 32 hex chars");
        assert!(
            suffix
                .chars()
                .all(|character| matches!(character, '0'..='9' | 'a'..='f')),
            "order id suffix should be lowercase hex"
        );
        assert!(
            generated.insert(suffix),
            "order id suffixes should be unique for one runtime instance"
        );
    }
    drop(pool);

    let restarted_pool = PgPoolOptions::new()
        .max_connections(5)
        .connect(database_url.as_str())
        .await
        .expect("postgres pool should reconnect after restart");
    for _ in 0..64 {
        let suffix = allocate_order_id_hex_from_postgres(&restarted_pool)
            .await
            .expect("order id suffix should allocate after restart");
        assert!(
            generated.insert(suffix),
            "order id suffixes should remain unique across runtime restarts"
        );
    }
}

#[tokio::test]
async fn runtime_order_payroll_anomaly_flows_persist_on_real_postgres_with_transactions() {
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

    let menu_repo = SqlJsonStateRepository::for_menu_supply(pool.clone());
    let payroll_repo = SqlJsonStateRepository::for_payroll_ledger(pool.clone());
    let anomaly_repo = SqlJsonStateRepository::for_anomaly_alert(pool.clone());
    let delivery_repo = SqlJsonStateRepository::for_delivery_policy(pool);

    let audit_trail = ImmutableAuditTrail::new(AuditRetentionPolicy::default());
    let committee = committee_admin();
    let plant = plant_id("fab-a");
    let vendor_actor = vendor_operator(&plant);
    let employee = employee_actor(&plant);
    let vendor_id = VendorId::parse("ven-runtime-sqlx-001").expect("vendor id should be valid");
    let order_id = OrderId::parse("ord-runtime-sqlx-001").expect("order id should be valid");
    let delivery_epoch_day = 20_500;

    let compliance_lifecycle = build_approved_compliance_lifecycle(
        audit_trail.clone(),
        &committee,
        &vendor_actor,
        &vendor_id,
        delivery_epoch_day,
    );

    let mut delivery_policy = VendorPlantDeliveryPolicy::with_audit_trail(audit_trail.clone());
    delivery_policy
        .upsert_mapping(
            &committee,
            TaipeiBusinessMoment::new(delivery_epoch_day.saturating_sub(1), 1)
                .expect("moment should be valid"),
            VendorPlantDeliveryMapping::new(
                DeliveryMappingId::parse("map-runtime-sqlx-allow")
                    .expect("mapping id should be valid"),
                vendor_id.clone(),
                plant.clone(),
                ServiceWindow::new(
                    TaipeiBusinessMoment::new(delivery_epoch_day.saturating_sub(1), 0)
                        .expect("moment should be valid"),
                    TaipeiBusinessMoment::new(delivery_epoch_day.saturating_add(3), 23 * 60 + 59)
                        .expect("moment should be valid"),
                )
                .expect("service window should be valid"),
                DeliveryRuleEffect::Allow,
                100,
            ),
        )
        .expect("delivery mapping should upsert");
    delivery_repo
        .save_snapshot(&delivery_policy.snapshot())
        .await
        .expect("delivery policy snapshot should persist");

    let menu_supply_policy = MenuSupplyPolicy::with_audit_trail_and_retention(
        Default::default(),
        audit_trail.clone(),
        OrderRetentionPolicy::default(),
    );
    menu_supply_policy
        .upsert_menu_item(
            &vendor_actor,
            VendorMenuItem::new(
                MenuItemId::parse("menu-runtime-sqlx-001").expect("menu id should be valid"),
                vendor_id.clone(),
                VendorMenuItemDraft::new(
                    "Runtime SQLX Bento",
                    "seeded for transactional persistence test",
                    "BENTO",
                    vec![MenuHealthTag::HighProtein],
                    Some(
                        MenuImageUrl::parse("s3://menu-assets/menu/runtime-sqlx.jpg")
                            .expect("image url should parse"),
                    ),
                    Money::new("TWD", 12_000).expect("money should be valid"),
                    20,
                    delivery_epoch_day,
                )
                .expect("menu draft should be valid"),
            ),
        )
        .expect("menu item should upsert");
    menu_repo
        .save_snapshot(
            &menu_supply_policy
                .snapshot()
                .expect("menu supply snapshot should build"),
        )
        .await
        .expect("menu supply snapshot should persist");

    payroll_repo
        .save_snapshot(
            &PayrollLedgerService::new(PayrollRetentionPolicy::default(), audit_trail.clone())
                .snapshot()
                .expect("payroll snapshot should build"),
        )
        .await
        .expect("payroll snapshot should persist");

    anomaly_repo
        .save_snapshot(
            &AnomalyAlertWorkflow::with_default_rules(audit_trail.clone())
                .snapshot()
                .expect("anomaly snapshot should build"),
        )
        .await
        .expect("anomaly snapshot should persist");

    let forced_order_rollback = menu_repo
        .mutate_snapshot::<MenuSupplyPolicySnapshot, (), String, _>(|snapshot| {
            let snapshot = snapshot.ok_or("missing menu supply snapshot".to_owned())?;
            let policy = MenuSupplyPolicy::from_snapshot(snapshot, audit_trail.clone());
            let gateway =
                HttpOrderingExecutionGateway::new(&compliance_lifecycle, &delivery_policy, &policy);
            gateway
                .execute_create_employee_order(
                    &employee,
                    order_id.clone(),
                    &vendor_id,
                    &plant,
                    delivery_epoch_day,
                    vec![OrderLineItemRequest::new(
                        MenuItemId::parse("menu-runtime-sqlx-001").expect("menu id should parse"),
                        2,
                        vec![],
                    )
                    .expect("line item should be valid")],
                    TaipeiBusinessMoment::new(delivery_epoch_day.saturating_sub(1), 600)
                        .expect("moment should be valid"),
                )
                .map_err(|error| error.to_string())?;
            Err("forced-order-rollback".to_owned())
        })
        .await;
    assert!(
        matches!(
            forced_order_rollback,
            Err(JsonStatePersistenceError::Domain(message)) if message == "forced-order-rollback"
        ),
        "forced rollback should abort order transaction"
    );
    let menu_after_rollback = MenuSupplyPolicy::from_snapshot(
        menu_repo
            .load_snapshot::<MenuSupplyPolicySnapshot>()
            .await
            .expect("menu snapshot load should succeed")
            .expect("menu snapshot should exist"),
        audit_trail.clone(),
    );
    assert!(
        menu_after_rollback
            .order_snapshot(&order_id)
            .expect("order snapshot lookup should succeed")
            .is_none(),
        "order mutation must rollback on domain error"
    );

    let (_menu_snapshot, created_order) = menu_repo
        .mutate_snapshot::<MenuSupplyPolicySnapshot, _, String, _>(|snapshot| {
            let snapshot = snapshot.ok_or("missing menu supply snapshot".to_owned())?;
            let policy = MenuSupplyPolicy::from_snapshot(snapshot, audit_trail.clone());
            let gateway =
                HttpOrderingExecutionGateway::new(&compliance_lifecycle, &delivery_policy, &policy);
            gateway
                .execute_create_employee_order(
                    &employee,
                    order_id.clone(),
                    &vendor_id,
                    &plant,
                    delivery_epoch_day,
                    vec![OrderLineItemRequest::new(
                        MenuItemId::parse("menu-runtime-sqlx-001").expect("menu id should parse"),
                        2,
                        vec![],
                    )
                    .expect("line item should be valid")],
                    TaipeiBusinessMoment::new(delivery_epoch_day.saturating_sub(1), 610)
                        .expect("moment should be valid"),
                )
                .map_err(|error| error.to_string())?;
            let snapshot = policy
                .order_snapshot(&order_id)
                .map_err(|error| error.to_string())?
                .ok_or("created order snapshot missing".to_owned())?;
            let persisted = policy.snapshot().map_err(|error| error.to_string())?;
            Ok((persisted, snapshot))
        })
        .await
        .expect("order mutation should persist");
    assert_eq!(created_order.order_id(), &order_id);

    let forced_payroll_rollback = payroll_repo
        .mutate_snapshot::<PayrollLedgerServiceSnapshot, (), String, _>(|snapshot| {
            let snapshot = snapshot.ok_or("missing payroll snapshot".to_owned())?;
            let service = PayrollLedgerService::from_snapshot(snapshot, audit_trail.clone());
            service
                .reconcile_order_charge(
                    &employee,
                    "createEmployeeOrder",
                    &order_id,
                    employee.actor_id(),
                    EmploymentStatus::Active,
                    delivery_epoch_day,
                    "TWD",
                    24_000,
                    AuditTimestamp::new(delivery_epoch_day, 620)
                        .expect("audit timestamp should be valid"),
                    PayrollLedgerSourceRef::new(
                        PayrollLedgerSourceKind::OrderMutation,
                        "order:ord-runtime-sqlx-001:state:PENDING",
                    )
                    .expect("payroll source ref should be valid"),
                )
                .map_err(|error| error.to_string())?;
            Err("forced-payroll-rollback".to_owned())
        })
        .await;
    assert!(
        matches!(
            forced_payroll_rollback,
            Err(JsonStatePersistenceError::Domain(message)) if message == "forced-payroll-rollback"
        ),
        "forced rollback should abort payroll transaction"
    );
    let payroll_after_rollback = PayrollLedgerService::from_snapshot(
        payroll_repo
            .load_snapshot::<PayrollLedgerServiceSnapshot>()
            .await
            .expect("payroll snapshot load should succeed")
            .expect("payroll snapshot should exist"),
        audit_trail.clone(),
    );
    assert!(
        payroll_after_rollback
            .employee_order_view(&employee, &order_id)
            .is_err(),
        "payroll rollback must keep order ledger absent"
    );

    let (_payroll_snapshot, order_view) = payroll_repo
        .mutate_snapshot::<PayrollLedgerServiceSnapshot, _, String, _>(|snapshot| {
            let snapshot = snapshot.ok_or("missing payroll snapshot".to_owned())?;
            let service = PayrollLedgerService::from_snapshot(snapshot, audit_trail.clone());
            service
                .reconcile_order_charge(
                    &employee,
                    "createEmployeeOrder",
                    &order_id,
                    employee.actor_id(),
                    EmploymentStatus::Active,
                    delivery_epoch_day,
                    "TWD",
                    24_000,
                    AuditTimestamp::new(delivery_epoch_day, 625)
                        .expect("audit timestamp should be valid"),
                    PayrollLedgerSourceRef::new(
                        PayrollLedgerSourceKind::OrderMutation,
                        "order:ord-runtime-sqlx-001:state:PENDING",
                    )
                    .expect("payroll source ref should be valid"),
                )
                .map_err(|error| error.to_string())?;
            let view = service
                .employee_order_view(&employee, &order_id)
                .map_err(|error| error.to_string())?;
            let persisted = service.snapshot().map_err(|error| error.to_string())?;
            Ok((persisted, view))
        })
        .await
        .expect("payroll mutation should persist");
    assert_eq!(order_view.net_amount_minor(), 24_000);

    let anomaly_owner = ActorId::parse("anomaly-owner-runtime-sqlx")
        .expect("anomaly owner actor id should be valid");
    let (_anomaly_snapshot, triggered_count) = anomaly_repo
        .mutate_snapshot::<AnomalyAlertWorkflowSnapshot, _, String, _>(|snapshot| {
            let snapshot = snapshot.ok_or("missing anomaly snapshot".to_owned())?;
            let workflow = AnomalyAlertWorkflow::from_snapshot(snapshot, audit_trail.clone());
            let result = workflow
                .evaluate_rules(
                    &committee,
                    AnomalySignalSnapshot::new(
                        vendor_id.clone(),
                        AuditTimestamp::new(delivery_epoch_day, 630)
                            .expect("audit timestamp should be valid"),
                    )
                    .with_on_time_rate(Some(0.60)),
                    &anomaly_owner,
                )
                .map_err(|error| error.to_string())?;
            let persisted = workflow.snapshot().map_err(|error| error.to_string())?;
            Ok((persisted, result.triggered_alerts().len()))
        })
        .await
        .expect("anomaly evaluation should persist");
    assert!(
        triggered_count > 0,
        "anomaly evaluation should trigger alerts"
    );

    let anomaly_reloaded = AnomalyAlertWorkflow::from_snapshot(
        anomaly_repo
            .load_snapshot::<AnomalyAlertWorkflowSnapshot>()
            .await
            .expect("anomaly snapshot load should succeed")
            .expect("anomaly snapshot should exist"),
        audit_trail,
    );
    let alerts = anomaly_reloaded
        .query_alerts(
            &AnomalyAlertQuery {
                vendor_id: Some(vendor_id),
                ..AnomalyAlertQuery::default()
            },
            AuditTimestamp::new(delivery_epoch_day, 640).expect("audit timestamp should be valid"),
        )
        .expect("alert query should succeed");
    assert!(
        !alerts.is_empty(),
        "anomaly alerts should remain after SQL persistence reload"
    );
}
