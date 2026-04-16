use std::collections::BTreeSet;
use std::time::{SystemTime, UNIX_EPOCH};

use corporate_catering_system::access::{AccessController, Action, AuthorizationError};
use corporate_catering_system::contract::{
    canonical_openapi_spec, write_openapi_artifacts, HttpAudience, HttpOperation,
};
use corporate_catering_system::identity::{
    ActorId, AuthenticatedActorContext, AuthenticationSource, PlantId, PlantScope, Role,
};
use corporate_catering_system::transport::http::{runtime_http_routes, HttpAuthorizationGateway};
use corporate_catering_system::transport::mcp::{
    runtime_mcp_tool_contract_issues, runtime_mcp_tools,
    runtime_mcp_write_tool_mapping_contract_issues, McpAuthorizationGateway, McpOperation,
};
use serde_json::Value;

fn actor_id(value: &str) -> ActorId {
    ActorId::parse(value).expect("actor id should be valid")
}

fn plant_id(value: &str) -> PlantId {
    PlantId::parse(value).expect("plant id should be valid")
}

fn restricted_scope(plants: &[&str]) -> PlantScope {
    let plant_ids = plants.iter().map(|plant| plant_id(plant)).collect();
    PlantScope::restricted(plant_ids).expect("restricted scope should be valid")
}

fn employee_actor() -> AuthenticatedActorContext {
    AuthenticatedActorContext::new(
        actor_id("emp-contract-001"),
        Role::Employee,
        restricted_scope(&["fab-a"]),
        AuthenticationSource::CorporateSso,
    )
    .expect("employee actor should be valid")
}

fn collect_operation_ids(spec: &Value) -> BTreeSet<String> {
    let mut operation_ids = BTreeSet::new();
    let paths = spec["paths"].as_object().expect("paths must be an object");
    for path_item in paths.values() {
        let methods = path_item.as_object().expect("path item must be an object");
        for operation in methods.values() {
            if let Some(operation_id) = operation["operationId"].as_str() {
                operation_ids.insert(operation_id.to_owned());
            }
        }
    }

    operation_ids
}

fn collect_openapi_routes(spec: &Value) -> BTreeSet<(String, String, String)> {
    let mut routes = BTreeSet::new();
    let paths = spec["paths"].as_object().expect("paths must be an object");
    for (path, path_item) in paths {
        let methods = path_item.as_object().expect("path item must be an object");
        for (method, operation) in methods {
            if !matches!(
                method.as_str(),
                "get" | "post" | "put" | "patch" | "delete" | "options" | "head" | "trace"
            ) {
                continue;
            }

            let operation_id = operation["operationId"]
                .as_str()
                .expect("operation id must be string");
            routes.insert((method.to_owned(), path.to_owned(), operation_id.to_owned()));
        }
    }

    routes
}

fn operation_by_path_and_method<'a>(spec: &'a Value, path: &str, method: &str) -> &'a Value {
    let operation = &spec["paths"][path][method];
    assert!(
        operation.is_object(),
        "operation {method} {path} must exist"
    );
    operation
}

fn assert_error_response_ref(operation: &Value, status_code: &str, expected_ref: &str) {
    let actual_ref = operation["responses"][status_code]["$ref"]
        .as_str()
        .unwrap_or_else(|| panic!("response {status_code} should be a $ref"));
    assert_eq!(actual_ref, expected_ref);
}

fn different_action(action: Action) -> Action {
    match action {
        Action::PlaceEmployeeOrder => Action::ManageVendorMenu,
        Action::ManageVendorMenu => Action::ManageVendorComplianceLifecycle,
        Action::ManageVendorComplianceLifecycle => Action::ExportPayrollDeductions,
        Action::ExportPayrollDeductions => Action::PlaceEmployeeOrder,
    }
}

fn ensure_test_otel_endpoint() {
    std::env::set_var("OTEL_EXPORTER_OTLP_ENDPOINT", "http://127.0.0.1:4317");
}

#[test]
fn contract_is_openapi_31_and_uses_only_locked_auth_model() {
    let spec = canonical_openapi_spec();

    assert_eq!(spec["openapi"], "3.1.0");
    let security_schemes = spec["components"]["securitySchemes"]
        .as_object()
        .expect("security schemes must be object");

    let keys: BTreeSet<String> = security_schemes.keys().cloned().collect();
    let expected = BTreeSet::from([
        "corporateSsoBearer".to_owned(),
        "vendorMfaBearer".to_owned(),
    ]);
    assert_eq!(keys, expected);

    let auth_sources = spec["components"]["schemas"]["AuthenticationSource"]["enum"]
        .as_array()
        .expect("authentication source enum must be array")
        .iter()
        .map(|value| {
            value
                .as_str()
                .expect("authentication source enum entries must be strings")
                .to_owned()
        })
        .collect::<BTreeSet<_>>();
    assert_eq!(
        auth_sources,
        BTreeSet::from([
            "CORPORATE_SSO".to_owned(),
            "VENDOR_ACCOUNT_MFA".to_owned(),
            "OAUTH_SERVICE_ACCOUNT".to_owned(),
        ])
    );
}

#[test]
fn openapi_spec_covers_all_official_http_operations() {
    let spec = canonical_openapi_spec();
    let paths = spec["paths"].as_object().expect("paths must be an object");

    for operation in HttpOperation::ALL {
        let path_item = paths
            .get(operation.path())
            .expect("official operation path must exist in OpenAPI");
        let method_item = path_item
            .get(operation.method().as_openapi_verb())
            .expect("official operation method must exist in OpenAPI");
        let operation_id = method_item["operationId"]
            .as_str()
            .expect("operation id must be string");
        assert_eq!(operation_id, operation.operation_id());

        let security = method_item["security"]
            .as_array()
            .expect("security must be an array");
        assert_eq!(security.len(), 1);
        let scheme = security[0]
            .as_object()
            .expect("security item must be object")
            .keys()
            .next()
            .expect("security scheme key must exist");
        match operation.audience() {
            HttpAudience::Vendor => assert_eq!(scheme, "vendorMfaBearer"),
            HttpAudience::Employee | HttpAudience::Admin | HttpAudience::Integration => {
                assert_eq!(scheme, "corporateSsoBearer")
            }
        }
    }

    let openapi_operation_ids = collect_operation_ids(&spec);
    let expected_operation_ids = HttpOperation::ALL
        .iter()
        .map(|operation| operation.operation_id().to_owned())
        .collect::<BTreeSet<_>>();
    assert_eq!(openapi_operation_ids, expected_operation_ids);
}

#[test]
fn admin_contract_exposes_vendor_compliance_and_delivery_mapping_capabilities() {
    let spec = canonical_openapi_spec();
    let decision_enum = spec["components"]["schemas"]["VendorReviewDecision"]["enum"]
        .as_array()
        .expect("vendor review decision enum must be array")
        .iter()
        .map(|value| {
            value
                .as_str()
                .expect("enum values must be strings")
                .to_owned()
        })
        .collect::<BTreeSet<_>>();
    assert_eq!(
        decision_enum,
        BTreeSet::from([
            "APPROVED".to_owned(),
            "REJECTED".to_owned(),
            "REQUEST_FIX".to_owned(),
        ])
    );

    let paths = spec["paths"].as_object().expect("paths must be object");
    assert!(paths.contains_key("/api/v1/admin/compliance/document-templates"));
    assert!(paths
        .contains_key("/api/v1/admin/compliance/document-templates/{vendorCategory}/{templateId}"));
    assert!(paths.contains_key("/api/v1/admin/compliance/lifecycle/executions"));
    assert!(paths.contains_key("/api/v1/admin/audit/investigations"));
    assert!(paths.contains_key("/api/v1/admin/audit/responsibilities"));
    assert!(paths.contains_key("/api/v1/admin/audit/retention-purge"));
    assert!(paths.contains_key("/api/v1/admin/orders/retention-purge"));
    assert!(paths.contains_key("/api/v1/admin/vendor-plant-delivery-mappings"));
    assert!(
        paths.contains_key("/api/v1/admin/vendors/{vendorId}/plant-delivery-mappings/{mappingId}")
    );

    let investigations_operation =
        operation_by_path_and_method(&spec, "/api/v1/admin/audit/investigations", "get");
    let investigation_parameter_refs = investigations_operation["parameters"]
        .as_array()
        .expect("audit investigation parameters should be array")
        .iter()
        .map(|parameter| {
            parameter["$ref"]
                .as_str()
                .expect("audit investigation parameter should be $ref")
                .to_owned()
        })
        .collect::<BTreeSet<_>>();
    assert_eq!(
        investigation_parameter_refs,
        BTreeSet::from([
            "#/components/parameters/AuditActionFilterQuery".to_owned(),
            "#/components/parameters/AuditActorIdFilterQuery".to_owned(),
            "#/components/parameters/AuditCorrelationIdFilterQuery".to_owned(),
            "#/components/parameters/AuditEntityIdFilterQuery".to_owned(),
            "#/components/parameters/AuditEntityTypeFilterQuery".to_owned(),
            "#/components/parameters/AuditOccurredFromEpochDayQuery".to_owned(),
            "#/components/parameters/AuditOccurredToEpochDayQuery".to_owned(),
        ])
    );
    assert_eq!(
        investigations_operation["responses"]["200"]["content"]["application/json"]["schema"]
            ["$ref"],
        "#/components/schemas/AuditInvestigationResponse"
    );

    let responsibilities_operation =
        operation_by_path_and_method(&spec, "/api/v1/admin/audit/responsibilities", "get");
    assert_eq!(
        responsibilities_operation["responses"]["200"]["content"]["application/json"]["schema"]
            ["$ref"],
        "#/components/schemas/AuditResponsibilityResponse"
    );

    let purge_operation =
        operation_by_path_and_method(&spec, "/api/v1/admin/audit/retention-purge", "post");
    assert_eq!(
        purge_operation["requestBody"]["content"]["application/json"]["schema"]["$ref"],
        "#/components/schemas/AuditRetentionPurgeRequest"
    );
    assert_eq!(
        purge_operation["responses"]["200"]["content"]["application/json"]["schema"]["$ref"],
        "#/components/schemas/AuditRetentionPurgeResponse"
    );
}

#[test]
fn audit_endpoints_have_tested_error_code_to_schema_refs() {
    let spec = canonical_openapi_spec();
    for (path, method) in [
        ("/api/v1/admin/audit/investigations", "get"),
        ("/api/v1/admin/audit/responsibilities", "get"),
        ("/api/v1/admin/audit/retention-purge", "post"),
    ] {
        let operation = operation_by_path_and_method(&spec, path, method);
        assert_error_response_ref(operation, "400", "#/components/responses/BadRequest");
        assert_error_response_ref(operation, "401", "#/components/responses/Unauthorized");
        assert_error_response_ref(operation, "403", "#/components/responses/Forbidden");
        assert_error_response_ref(
            operation,
            "500",
            "#/components/responses/InternalServerError",
        );
    }

    for response_name in [
        "BadRequest",
        "Unauthorized",
        "Forbidden",
        "InternalServerError",
    ] {
        let response_schema_ref = spec["components"]["responses"][response_name]["content"]
            ["application/json"]["schema"]["$ref"]
            .as_str()
            .unwrap_or_else(|| panic!("{response_name} response should reference a schema"));
        assert_eq!(response_schema_ref, "#/components/schemas/ErrorResponse");
    }

    let error_codes = spec["components"]["schemas"]["ErrorCode"]["enum"]
        .as_array()
        .expect("error code enum should exist")
        .iter()
        .map(|value| {
            value
                .as_str()
                .expect("error code enum value should be string")
                .to_owned()
        })
        .collect::<BTreeSet<_>>();
    assert!(error_codes.contains("INVALID_AUDIT_INVESTIGATION_QUERY"));
    assert!(error_codes.contains("AUDIT_INVESTIGATION_INTERNAL_ERROR"));
    assert!(error_codes.contains("AUDIT_RETENTION_PURGE_INTERNAL_ERROR"));
    assert!(error_codes.contains("ORDER_RETENTION_PURGE_INTERNAL_ERROR"));
}

#[test]
fn vendor_fulfillment_board_and_export_batch_contracts_are_declared_with_controlled_special_requests(
) {
    let spec = canonical_openapi_spec();
    let paths = spec["paths"].as_object().expect("paths must be object");
    assert!(paths.contains_key("/api/v1/vendor/fulfillment-board"));
    assert!(paths.contains_key("/api/v1/vendor/orders/{orderId}/delivery-status"));
    assert!(paths.contains_key("/api/v1/vendor/fulfillment-batches"));
    assert!(paths.contains_key("/api/v1/vendor/fulfillment-batches/{batchId}"));

    let board_operation =
        operation_by_path_and_method(&spec, "/api/v1/vendor/fulfillment-board", "get");
    let board_parameter_refs = board_operation["parameters"]
        .as_array()
        .expect("board parameters should be array")
        .iter()
        .map(|parameter| {
            parameter["$ref"]
                .as_str()
                .expect("board parameter should be $ref")
                .to_owned()
        })
        .collect::<BTreeSet<_>>();
    assert_eq!(
        board_parameter_refs,
        BTreeSet::from([
            "#/components/parameters/DeliveryDateQuery".to_owned(),
            "#/components/parameters/PlantIdFilterQuery".to_owned(),
            "#/components/parameters/IncludeAuditTransitionsQuery".to_owned(),
        ])
    );

    let transition_operation = operation_by_path_and_method(
        &spec,
        "/api/v1/vendor/orders/{orderId}/delivery-status",
        "post",
    );
    assert_error_response_ref(
        transition_operation,
        "409",
        "#/components/responses/Conflict",
    );
    assert_eq!(
        transition_operation["requestBody"]["content"]["application/json"]["schema"]["$ref"],
        "#/components/schemas/VendorFulfillmentDeliveryStatusTransitionRequest"
    );

    let batch_create_operation =
        operation_by_path_and_method(&spec, "/api/v1/vendor/fulfillment-batches", "post");
    assert_eq!(
        batch_create_operation["responses"]["201"]["content"]["application/json"]["schema"]["$ref"],
        "#/components/schemas/VendorFulfillmentExportBatch"
    );

    let label_schema = &spec["components"]["schemas"]["VendorFulfillmentLabelEntry"];
    let label_special_request_ref = label_schema["properties"]["specialRequests"]["items"]["$ref"]
        .as_str()
        .expect("label special requests should reference controlled enum");
    assert_eq!(
        label_special_request_ref,
        "#/components/schemas/SpecialRequestOption"
    );
}

#[test]
fn delivery_mapping_endpoints_have_tested_error_code_to_schema_refs() {
    let spec = canonical_openapi_spec();

    let list_mapping_operation =
        operation_by_path_and_method(&spec, "/api/v1/admin/vendor-plant-delivery-mappings", "get");
    assert_error_response_ref(
        list_mapping_operation,
        "400",
        "#/components/responses/BadRequest",
    );
    assert_error_response_ref(
        list_mapping_operation,
        "401",
        "#/components/responses/Unauthorized",
    );
    assert_error_response_ref(
        list_mapping_operation,
        "403",
        "#/components/responses/Forbidden",
    );

    let upsert_mapping_operation = operation_by_path_and_method(
        &spec,
        "/api/v1/admin/vendors/{vendorId}/plant-delivery-mappings/{mappingId}",
        "put",
    );
    assert_error_response_ref(
        upsert_mapping_operation,
        "400",
        "#/components/responses/BadRequest",
    );
    assert_error_response_ref(
        upsert_mapping_operation,
        "401",
        "#/components/responses/Unauthorized",
    );
    assert_error_response_ref(
        upsert_mapping_operation,
        "403",
        "#/components/responses/Forbidden",
    );
    assert_error_response_ref(
        upsert_mapping_operation,
        "404",
        "#/components/responses/NotFound",
    );
    assert_error_response_ref(
        upsert_mapping_operation,
        "422",
        "#/components/responses/ValidationFailed",
    );

    let delete_mapping_operation = operation_by_path_and_method(
        &spec,
        "/api/v1/admin/vendors/{vendorId}/plant-delivery-mappings/{mappingId}",
        "delete",
    );
    assert_error_response_ref(
        delete_mapping_operation,
        "400",
        "#/components/responses/BadRequest",
    );
    assert_error_response_ref(
        delete_mapping_operation,
        "401",
        "#/components/responses/Unauthorized",
    );
    assert_error_response_ref(
        delete_mapping_operation,
        "403",
        "#/components/responses/Forbidden",
    );
    assert_error_response_ref(
        delete_mapping_operation,
        "404",
        "#/components/responses/NotFound",
    );

    for response_name in [
        "BadRequest",
        "Unauthorized",
        "Forbidden",
        "NotFound",
        "ValidationFailed",
    ] {
        let response_schema_ref = spec["components"]["responses"][response_name]["content"]
            ["application/json"]["schema"]["$ref"]
            .as_str()
            .unwrap_or_else(|| panic!("{response_name} response should reference a schema"));
        assert_eq!(response_schema_ref, "#/components/schemas/ErrorResponse");
    }

    let error_codes = spec["components"]["schemas"]["ErrorCode"]["enum"]
        .as_array()
        .expect("error code enum must be present");
    let as_set = error_codes
        .iter()
        .map(|value| value.as_str().expect("error code values must be strings"))
        .collect::<BTreeSet<_>>();
    assert!(as_set.contains("BAD_REQUEST"));
    assert!(as_set.contains("UNAUTHORIZED"));
    assert!(as_set.contains("FORBIDDEN"));
    assert!(as_set.contains("NOT_FOUND"));
    assert!(as_set.contains("VALIDATION_FAILED"));
}

#[test]
fn ordering_and_menu_endpoints_have_tested_error_code_to_schema_refs() {
    let spec = canonical_openapi_spec();

    let create_order_operation =
        operation_by_path_and_method(&spec, "/api/v1/employee/orders", "post");
    assert_error_response_ref(
        create_order_operation,
        "400",
        "#/components/responses/BadRequest",
    );
    assert_error_response_ref(
        create_order_operation,
        "401",
        "#/components/responses/Unauthorized",
    );
    assert_error_response_ref(
        create_order_operation,
        "403",
        "#/components/responses/Forbidden",
    );
    assert_error_response_ref(
        create_order_operation,
        "409",
        "#/components/responses/Conflict",
    );
    assert_error_response_ref(
        create_order_operation,
        "422",
        "#/components/responses/ValidationFailed",
    );

    let update_order_operation =
        operation_by_path_and_method(&spec, "/api/v1/employee/orders/{orderId}", "patch");
    assert_error_response_ref(
        update_order_operation,
        "400",
        "#/components/responses/BadRequest",
    );
    assert_error_response_ref(
        update_order_operation,
        "401",
        "#/components/responses/Unauthorized",
    );
    assert_error_response_ref(
        update_order_operation,
        "403",
        "#/components/responses/Forbidden",
    );
    assert_error_response_ref(
        update_order_operation,
        "404",
        "#/components/responses/NotFound",
    );
    assert_error_response_ref(
        update_order_operation,
        "409",
        "#/components/responses/Conflict",
    );
    assert_error_response_ref(
        update_order_operation,
        "422",
        "#/components/responses/ValidationFailed",
    );

    let pickup_verification_operation = operation_by_path_and_method(
        &spec,
        "/api/v1/employee/orders/{orderId}/pickup-verifications",
        "post",
    );
    assert_error_response_ref(
        pickup_verification_operation,
        "400",
        "#/components/responses/BadRequest",
    );
    assert_error_response_ref(
        pickup_verification_operation,
        "401",
        "#/components/responses/Unauthorized",
    );
    assert_error_response_ref(
        pickup_verification_operation,
        "403",
        "#/components/responses/Forbidden",
    );
    assert_error_response_ref(
        pickup_verification_operation,
        "404",
        "#/components/responses/NotFound",
    );
    assert_error_response_ref(
        pickup_verification_operation,
        "500",
        "#/components/responses/InternalServerError",
    );

    let upsert_menu_operation =
        operation_by_path_and_method(&spec, "/api/v1/vendor/menu-items/{menuItemId}", "put");
    assert_error_response_ref(
        upsert_menu_operation,
        "400",
        "#/components/responses/BadRequest",
    );
    assert_error_response_ref(
        upsert_menu_operation,
        "401",
        "#/components/responses/Unauthorized",
    );
    assert_error_response_ref(
        upsert_menu_operation,
        "403",
        "#/components/responses/Forbidden",
    );
    assert_error_response_ref(
        upsert_menu_operation,
        "422",
        "#/components/responses/ValidationFailed",
    );

    for response_name in [
        "BadRequest",
        "Unauthorized",
        "Forbidden",
        "NotFound",
        "Conflict",
        "ValidationFailed",
        "InternalServerError",
    ] {
        let response_schema_ref = spec["components"]["responses"][response_name]["content"]
            ["application/json"]["schema"]["$ref"]
            .as_str()
            .unwrap_or_else(|| panic!("{response_name} response should reference a schema"));
        assert_eq!(response_schema_ref, "#/components/schemas/ErrorResponse");
    }

    let error_codes = spec["components"]["schemas"]["ErrorCode"]["enum"]
        .as_array()
        .expect("error code enum must be present");
    let as_set = error_codes
        .iter()
        .map(|value| value.as_str().expect("error code values must be strings"))
        .collect::<BTreeSet<_>>();
    assert!(as_set.contains("BAD_REQUEST"));
    assert!(as_set.contains("UNAUTHORIZED"));
    assert!(as_set.contains("FORBIDDEN"));
    assert!(as_set.contains("NOT_FOUND"));
    assert!(as_set.contains("CONFLICT"));
    assert!(as_set.contains("VALIDATION_FAILED"));
    assert!(as_set.contains("INVALID_PICKUP_VERIFICATION_REQUEST"));
    assert!(as_set.contains("PICKUP_VERIFICATION_REPLAYED"));
    assert!(as_set.contains("PICKUP_VERIFICATION_STATE_CONFLICT"));
    assert!(as_set.contains("PICKUP_VERIFICATION_EXPIRED"));
    assert!(as_set.contains("PICKUP_VERIFICATION_INVALID_WINDOW"));
    assert!(as_set.contains("PICKUP_VERIFICATION_INVALID_CODE"));
    assert!(as_set.contains("PICKUP_VERIFICATION_INTERNAL_ERROR"));
    assert!(as_set.contains("ORDER_NOT_FOUND"));
}

#[test]
fn payroll_endpoints_have_tested_error_code_to_schema_refs() {
    let spec = canonical_openapi_spec();

    let employee_ledger_operation = operation_by_path_and_method(
        &spec,
        "/api/v1/employee/orders/{orderId}/payroll-ledger",
        "get",
    );
    assert_error_response_ref(
        employee_ledger_operation,
        "400",
        "#/components/responses/BadRequest",
    );
    assert_error_response_ref(
        employee_ledger_operation,
        "401",
        "#/components/responses/Unauthorized",
    );
    assert_error_response_ref(
        employee_ledger_operation,
        "403",
        "#/components/responses/Forbidden",
    );
    assert_error_response_ref(
        employee_ledger_operation,
        "404",
        "#/components/responses/NotFound",
    );
    assert_error_response_ref(
        employee_ledger_operation,
        "500",
        "#/components/responses/InternalServerError",
    );

    let employee_dispute_operation =
        operation_by_path_and_method(&spec, "/api/v1/employee/orders/{orderId}/disputes", "post");
    assert_error_response_ref(
        employee_dispute_operation,
        "400",
        "#/components/responses/BadRequest",
    );
    assert_error_response_ref(
        employee_dispute_operation,
        "401",
        "#/components/responses/Unauthorized",
    );
    assert_error_response_ref(
        employee_dispute_operation,
        "403",
        "#/components/responses/Forbidden",
    );
    assert_error_response_ref(
        employee_dispute_operation,
        "404",
        "#/components/responses/NotFound",
    );
    assert_error_response_ref(
        employee_dispute_operation,
        "409",
        "#/components/responses/Conflict",
    );
    assert_error_response_ref(
        employee_dispute_operation,
        "500",
        "#/components/responses/InternalServerError",
    );

    let admin_dispute_operation =
        operation_by_path_and_method(&spec, "/api/v1/admin/payroll/disputes/{disputeId}", "patch");
    assert_error_response_ref(
        admin_dispute_operation,
        "400",
        "#/components/responses/BadRequest",
    );
    assert_error_response_ref(
        admin_dispute_operation,
        "401",
        "#/components/responses/Unauthorized",
    );
    assert_error_response_ref(
        admin_dispute_operation,
        "403",
        "#/components/responses/Forbidden",
    );
    assert_error_response_ref(
        admin_dispute_operation,
        "404",
        "#/components/responses/NotFound",
    );
    assert_error_response_ref(
        admin_dispute_operation,
        "409",
        "#/components/responses/Conflict",
    );
    assert_error_response_ref(
        admin_dispute_operation,
        "500",
        "#/components/responses/InternalServerError",
    );

    let payroll_retention_purge_operation =
        operation_by_path_and_method(&spec, "/api/v1/admin/payroll/retention-purge", "post");
    assert_error_response_ref(
        payroll_retention_purge_operation,
        "400",
        "#/components/responses/BadRequest",
    );
    assert_error_response_ref(
        payroll_retention_purge_operation,
        "401",
        "#/components/responses/Unauthorized",
    );
    assert_error_response_ref(
        payroll_retention_purge_operation,
        "403",
        "#/components/responses/Forbidden",
    );
    assert_error_response_ref(
        payroll_retention_purge_operation,
        "500",
        "#/components/responses/InternalServerError",
    );
    assert_eq!(
        payroll_retention_purge_operation["requestBody"]["content"]["application/json"]["schema"]
            ["$ref"],
        "#/components/schemas/PayrollRetentionPurgeRequest"
    );

    let order_retention_purge_operation =
        operation_by_path_and_method(&spec, "/api/v1/admin/orders/retention-purge", "post");
    assert_error_response_ref(
        order_retention_purge_operation,
        "400",
        "#/components/responses/BadRequest",
    );
    assert_error_response_ref(
        order_retention_purge_operation,
        "401",
        "#/components/responses/Unauthorized",
    );
    assert_error_response_ref(
        order_retention_purge_operation,
        "403",
        "#/components/responses/Forbidden",
    );
    assert_error_response_ref(
        order_retention_purge_operation,
        "500",
        "#/components/responses/InternalServerError",
    );
    assert_eq!(
        order_retention_purge_operation["requestBody"]["content"]["application/json"]["schema"]
            ["$ref"],
        "#/components/schemas/OrderRetentionPurgeRequest"
    );
    assert_eq!(
        order_retention_purge_operation["responses"]["200"]["content"]["application/json"]
            ["schema"]["$ref"],
        "#/components/schemas/OrderRetentionPurgeResponse"
    );

    let monthly_close_operation = operation_by_path_and_method(
        &spec,
        "/api/v1/admin/payroll/monthly-settlements/close",
        "post",
    );
    assert_error_response_ref(
        monthly_close_operation,
        "400",
        "#/components/responses/BadRequest",
    );
    assert_error_response_ref(
        monthly_close_operation,
        "401",
        "#/components/responses/Unauthorized",
    );
    assert_error_response_ref(
        monthly_close_operation,
        "403",
        "#/components/responses/Forbidden",
    );
    assert_error_response_ref(
        monthly_close_operation,
        "409",
        "#/components/responses/Conflict",
    );
    assert_error_response_ref(
        monthly_close_operation,
        "500",
        "#/components/responses/InternalServerError",
    );
    assert_eq!(
        monthly_close_operation["requestBody"]["content"]["application/json"]["schema"]["$ref"],
        "#/components/schemas/PayrollMonthlySettlementCloseRequest"
    );

    let lock_cycle_operation = operation_by_path_and_method(
        &spec,
        "/api/v1/admin/payroll/monthly-settlements/{cycleKey}/lock",
        "post",
    );
    assert_error_response_ref(
        lock_cycle_operation,
        "400",
        "#/components/responses/BadRequest",
    );
    assert_error_response_ref(
        lock_cycle_operation,
        "401",
        "#/components/responses/Unauthorized",
    );
    assert_error_response_ref(
        lock_cycle_operation,
        "403",
        "#/components/responses/Forbidden",
    );
    assert_error_response_ref(
        lock_cycle_operation,
        "404",
        "#/components/responses/NotFound",
    );
    assert_error_response_ref(
        lock_cycle_operation,
        "409",
        "#/components/responses/Conflict",
    );
    assert_error_response_ref(
        lock_cycle_operation,
        "500",
        "#/components/responses/InternalServerError",
    );
    assert_eq!(
        lock_cycle_operation["requestBody"]["content"]["application/json"]["schema"]["$ref"],
        "#/components/schemas/PayrollSettlementCycleLockRequest"
    );
    let lock_cycle_parameter_refs = lock_cycle_operation["parameters"]
        .as_array()
        .expect("lock cycle parameters should be array")
        .iter()
        .map(|parameter| {
            parameter["$ref"]
                .as_str()
                .expect("lock cycle parameter should be $ref")
                .to_owned()
        })
        .collect::<BTreeSet<_>>();
    assert_eq!(
        lock_cycle_parameter_refs,
        BTreeSet::from(["#/components/parameters/PayrollSettlementCycleKeyPath".to_owned(),])
    );

    let unlock_cycle_operation = operation_by_path_and_method(
        &spec,
        "/api/v1/admin/payroll/monthly-settlements/{cycleKey}/unlock",
        "post",
    );
    assert_error_response_ref(
        unlock_cycle_operation,
        "400",
        "#/components/responses/BadRequest",
    );
    assert_error_response_ref(
        unlock_cycle_operation,
        "401",
        "#/components/responses/Unauthorized",
    );
    assert_error_response_ref(
        unlock_cycle_operation,
        "403",
        "#/components/responses/Forbidden",
    );
    assert_error_response_ref(
        unlock_cycle_operation,
        "404",
        "#/components/responses/NotFound",
    );
    assert_error_response_ref(
        unlock_cycle_operation,
        "409",
        "#/components/responses/Conflict",
    );
    assert_error_response_ref(
        unlock_cycle_operation,
        "500",
        "#/components/responses/InternalServerError",
    );
    assert_eq!(
        unlock_cycle_operation["requestBody"]["content"]["application/json"]["schema"]["$ref"],
        "#/components/schemas/PayrollSettlementCycleLockRequest"
    );
    let unlock_cycle_parameter_refs = unlock_cycle_operation["parameters"]
        .as_array()
        .expect("unlock cycle parameters should be array")
        .iter()
        .map(|parameter| {
            parameter["$ref"]
                .as_str()
                .expect("unlock cycle parameter should be $ref")
                .to_owned()
        })
        .collect::<BTreeSet<_>>();
    assert_eq!(
        unlock_cycle_parameter_refs,
        BTreeSet::from(["#/components/parameters/PayrollSettlementCycleKeyPath".to_owned(),])
    );

    let payroll_export_operation =
        operation_by_path_and_method(&spec, "/api/v1/integrations/payroll/deductions", "get");
    assert_error_response_ref(
        payroll_export_operation,
        "400",
        "#/components/responses/BadRequest",
    );
    assert_error_response_ref(
        payroll_export_operation,
        "401",
        "#/components/responses/Unauthorized",
    );
    assert_error_response_ref(
        payroll_export_operation,
        "403",
        "#/components/responses/Forbidden",
    );
    assert_error_response_ref(
        payroll_export_operation,
        "500",
        "#/components/responses/InternalServerError",
    );

    let payroll_export_parameter_refs = payroll_export_operation["parameters"]
        .as_array()
        .expect("payroll export parameters should be array")
        .iter()
        .map(|parameter| {
            parameter["$ref"]
                .as_str()
                .expect("payroll export parameter should be $ref")
                .to_owned()
        })
        .collect::<BTreeSet<_>>();
    assert_eq!(
        payroll_export_parameter_refs,
        BTreeSet::from([
            "#/components/parameters/PayPeriodQuery".to_owned(),
            "#/components/parameters/PayrollCycleKeyQuery".to_owned(),
            "#/components/parameters/PageQuery".to_owned(),
            "#/components/parameters/PageSizeQuery".to_owned(),
            "#/components/parameters/PayrollSortByQuery".to_owned(),
            "#/components/parameters/SortOrderQuery".to_owned(),
        ])
    );

    let payroll_sync_operation = operation_by_path_and_method(
        &spec,
        "/api/v1/integrations/payroll/sftp-batches/{batchId}/hr-api-sync",
        "post",
    );
    assert_error_response_ref(
        payroll_sync_operation,
        "400",
        "#/components/responses/BadRequest",
    );
    assert_error_response_ref(
        payroll_sync_operation,
        "401",
        "#/components/responses/Unauthorized",
    );
    assert_error_response_ref(
        payroll_sync_operation,
        "403",
        "#/components/responses/Forbidden",
    );
    assert_error_response_ref(
        payroll_sync_operation,
        "404",
        "#/components/responses/NotFound",
    );
    assert_error_response_ref(
        payroll_sync_operation,
        "500",
        "#/components/responses/InternalServerError",
    );
    assert_eq!(
        payroll_sync_operation["requestBody"]["content"]["application/json"]["schema"]["$ref"],
        "#/components/schemas/PayrollHrApiSyncRequest"
    );
    assert_eq!(payroll_sync_operation["requestBody"]["required"], true);
    let payroll_sync_required_fields = spec["components"]["schemas"]["PayrollHrApiSyncRequest"]
        ["required"]
        .as_array()
        .expect("PayrollHrApiSyncRequest.required should be array")
        .iter()
        .map(|value| {
            value
                .as_str()
                .expect("required field should be string")
                .to_owned()
        })
        .collect::<BTreeSet<_>>();
    assert_eq!(
        payroll_sync_required_fields,
        BTreeSet::from(["outcome".to_owned()])
    );

    let error_codes = spec["components"]["schemas"]["ErrorCode"]["enum"]
        .as_array()
        .expect("error code enum should exist")
        .iter()
        .map(|value| {
            value
                .as_str()
                .expect("error code enum value should be string")
                .to_owned()
        })
        .collect::<BTreeSet<_>>();
    assert!(error_codes.contains("BAD_REQUEST"));
    assert!(error_codes.contains("NOT_FOUND"));
    assert!(error_codes.contains("CONFLICT"));
    assert!(error_codes.contains("PAYROLL_LEDGER_INTERNAL_ERROR"));
}

#[test]
fn anomaly_alert_workflow_contract_exposes_governance_endpoints_and_schemas() {
    let spec = canonical_openapi_spec();

    let list_rules_operation =
        operation_by_path_and_method(&spec, "/api/v1/admin/anomaly/rules", "get");
    assert_eq!(
        list_rules_operation["operationId"],
        HttpOperation::ListAnomalyRules.operation_id()
    );
    assert_error_response_ref(
        list_rules_operation,
        "401",
        "#/components/responses/Unauthorized",
    );
    assert_error_response_ref(
        list_rules_operation,
        "403",
        "#/components/responses/Forbidden",
    );

    let upsert_rule_operation =
        operation_by_path_and_method(&spec, "/api/v1/admin/anomaly/rules/{ruleId}", "put");
    assert_eq!(
        upsert_rule_operation["operationId"],
        HttpOperation::UpsertAnomalyRule.operation_id()
    );
    assert_error_response_ref(
        upsert_rule_operation,
        "400",
        "#/components/responses/BadRequest",
    );
    assert_eq!(
        upsert_rule_operation["requestBody"]["content"]["application/json"]["schema"]["$ref"],
        "#/components/schemas/AnomalyRuleUpsertRequest"
    );
    assert_eq!(
        spec["components"]["parameters"]["AnomalyRuleIdPath"]["schema"]["pattern"].as_str(),
        Some("^rule-[a-z0-9-]{3,64}$")
    );

    let evaluate_operation =
        operation_by_path_and_method(&spec, "/api/v1/admin/anomaly/alerts/evaluations", "post");
    assert_eq!(
        evaluate_operation["operationId"],
        HttpOperation::EvaluateAnomalyAlerts.operation_id()
    );
    assert_error_response_ref(
        evaluate_operation,
        "400",
        "#/components/responses/BadRequest",
    );
    assert_eq!(
        evaluate_operation["requestBody"]["content"]["application/json"]["schema"]["$ref"],
        "#/components/schemas/AnomalyAlertEvaluationRequest"
    );
    let anomaly_evaluation_properties =
        &spec["components"]["schemas"]["AnomalyAlertEvaluationRequest"]["properties"];
    assert_eq!(
        anomaly_evaluation_properties["daysUntilExpiry"]["minimum"].as_f64(),
        Some(0.0)
    );
    assert_eq!(
        anomaly_evaluation_properties["onTimeRate"]["minimum"].as_f64(),
        Some(0.0)
    );
    assert_eq!(
        anomaly_evaluation_properties["onTimeRate"]["maximum"].as_f64(),
        Some(1.0)
    );
    assert_eq!(
        anomaly_evaluation_properties["satisfactionScore"]["minimum"].as_f64(),
        Some(0.0)
    );
    assert_eq!(
        anomaly_evaluation_properties["satisfactionScore"]["maximum"].as_f64(),
        Some(5.0)
    );
    assert_eq!(
        anomaly_evaluation_properties["complaintCount"]["minimum"].as_f64(),
        Some(0.0)
    );

    let list_alerts_operation =
        operation_by_path_and_method(&spec, "/api/v1/admin/anomaly/alerts", "get");
    assert_eq!(
        list_alerts_operation["operationId"],
        HttpOperation::ListAnomalyAlerts.operation_id()
    );
    assert_error_response_ref(
        list_alerts_operation,
        "400",
        "#/components/responses/BadRequest",
    );

    let update_alert_operation =
        operation_by_path_and_method(&spec, "/api/v1/admin/anomaly/alerts/{alertId}", "patch");
    assert_eq!(
        update_alert_operation["operationId"],
        HttpOperation::UpdateAdminAnomalyAlert.operation_id()
    );
    assert_error_response_ref(
        update_alert_operation,
        "404",
        "#/components/responses/NotFound",
    );
    assert_error_response_ref(
        update_alert_operation,
        "409",
        "#/components/responses/Conflict",
    );
    assert_eq!(
        update_alert_operation["requestBody"]["content"]["application/json"]["schema"]["$ref"],
        "#/components/schemas/AdminAnomalyAlertPatchRequest"
    );
    assert_eq!(
        spec["components"]["parameters"]["AnomalyAlertIdPath"]["schema"]["pattern"].as_str(),
        Some("^alt-[0-9a-f]{16}$")
    );
    for schema_name in [
        "AdminAnomalyAlertAssignOwnerPatchRequest",
        "AdminAnomalyAlertAcknowledgePatchRequest",
        "AdminAnomalyAlertStartRemediationPatchRequest",
        "AdminAnomalyAlertEscalatePatchRequest",
        "AdminAnomalyAlertClosePatchRequest",
    ] {
        let note_schema = &spec["components"]["schemas"][schema_name]["properties"]["note"];
        assert_eq!(note_schema["minLength"].as_u64(), Some(1));
        assert_eq!(note_schema["maxLength"].as_u64(), Some(280));
        assert_eq!(note_schema["pattern"].as_str(), Some(r".*\S.*"));
    }
    let close_schema = &spec["components"]["schemas"]["AdminAnomalyAlertClosePatchRequest"]
        ["properties"]["closureNote"];
    assert_eq!(close_schema["minLength"].as_u64(), Some(1));
    assert_eq!(close_schema["maxLength"].as_u64(), Some(280));
    assert_eq!(close_schema["pattern"].as_str(), Some(r".*\S.*"));
    let close_evidence_item_schema = &spec["components"]["schemas"]
        ["AdminAnomalyAlertClosePatchRequest"]["properties"]["closureEvidenceRefs"]["items"];
    assert_eq!(close_evidence_item_schema["minLength"].as_u64(), Some(1));
    assert_eq!(close_evidence_item_schema["maxLength"].as_u64(), Some(280));
    assert_eq!(
        close_evidence_item_schema["pattern"].as_str(),
        Some(r".*\S.*")
    );

    let anomaly_rule_kind_enum = spec["components"]["schemas"]["AnomalyRuleKind"]["enum"]
        .as_array()
        .expect("anomaly rule kind enum should be array")
        .iter()
        .map(|value| {
            value
                .as_str()
                .expect("anomaly rule kind enum value must be string")
                .to_owned()
        })
        .collect::<BTreeSet<_>>();
    assert_eq!(
        anomaly_rule_kind_enum,
        BTreeSet::from([
            "EXPIRY_RISK".to_owned(),
            "ON_TIME_DEGRADATION".to_owned(),
            "SATISFACTION_DROP".to_owned(),
            "COMPLAINT_SPIKE".to_owned(),
        ])
    );

    let audit_actions = spec["components"]["schemas"]["AuditAction"]["enum"]
        .as_array()
        .expect("audit action enum should be array")
        .iter()
        .map(|value| {
            value
                .as_str()
                .expect("audit action enum value must be string")
                .to_owned()
        })
        .collect::<BTreeSet<_>>();
    assert!(audit_actions.contains("UPSERT_ANOMALY_DETECTION_RULE"));
    assert!(audit_actions.contains("TRIGGER_ANOMALY_ALERT"));
    assert!(audit_actions.contains("ASSIGN_ANOMALY_ALERT_OWNER"));
    assert!(audit_actions.contains("ADVANCE_ANOMALY_ALERT_STATUS"));
    assert!(audit_actions.contains("CLOSE_ANOMALY_ALERT"));
}

#[test]
fn runtime_http_route_catalog_matches_openapi_contract() {
    let spec = canonical_openapi_spec();
    let openapi_routes = collect_openapi_routes(&spec);
    let runtime_routes = runtime_http_routes()
        .iter()
        .map(|route| {
            (
                route.method().as_openapi_verb().to_owned(),
                route.path().to_owned(),
                route.operation_id().to_owned(),
            )
        })
        .collect::<BTreeSet<_>>();

    assert_eq!(
        runtime_routes.len(),
        runtime_http_routes().len(),
        "runtime HTTP route catalog must not contain duplicate method/path/operation entries",
    );
    assert_eq!(runtime_routes, openapi_routes);
}

#[test]
fn http_gateway_enforces_contract_operation_id_and_action_mapping() {
    ensure_test_otel_endpoint();
    let access_controller = AccessController::with_default_policy();
    let gateway = HttpAuthorizationGateway::new(access_controller);
    let actor = employee_actor();
    let target_plant = plant_id("fab-a");

    assert!(matches!(
        gateway.authorize_write(
            Some(&actor),
            Action::PlaceEmployeeOrder,
            Some(&target_plant),
            "unknownOperationId"
        ),
        Err(AuthorizationError::UnknownHttpOperationId { .. })
    ));

    assert!(matches!(
        gateway.authorize_write(
            Some(&actor),
            Action::PlaceEmployeeOrder,
            Some(&target_plant),
            HttpOperation::ListEmployeeMenus.operation_id()
        ),
        Err(AuthorizationError::HttpOperationIsNotWriteOperation { .. })
    ));

    assert!(matches!(
        gateway.authorize_write(
            Some(&actor),
            Action::ManageVendorMenu,
            Some(&target_plant),
            HttpOperation::CreateEmployeeOrder.operation_id()
        ),
        Err(AuthorizationError::HttpOperationActionMismatch { .. })
    ));
}

#[test]
fn mcp_contract_checks_are_wired_for_future_runtime_tools() {
    ensure_test_otel_endpoint();
    let issues = runtime_mcp_tool_contract_issues();
    assert!(
        issues.is_empty(),
        "runtime MCP tool catalog has contract issues:\n{}",
        issues.join("\n")
    );
    let mapping_issues = runtime_mcp_write_tool_mapping_contract_issues();
    assert!(
        mapping_issues.is_empty(),
        "runtime MCP write-tool mappings have contract issues:\n{}",
        mapping_issues.join("\n")
    );

    let access_controller = AccessController::with_default_policy();
    let gateway = McpAuthorizationGateway::new(access_controller);
    let actor = employee_actor();
    let target_plant = plant_id("fab-a");
    let valid_operation = runtime_mcp_tools()
        .first()
        .map(|tool| tool.operation())
        .unwrap_or(McpOperation::PlaceEmployeeOrder);

    for tool in runtime_mcp_tools() {
        assert_eq!(
            McpOperation::from_operation_id(tool.operation_id()),
            Some(tool.operation())
        );
    }

    gateway
        .authorize_write(
            Some(&actor),
            valid_operation.action(),
            Some(&target_plant),
            valid_operation.operation_id(),
        )
        .expect("MCP writes must always use a known contract operation id");

    assert!(matches!(
        gateway.authorize_write(
            Some(&actor),
            valid_operation.action(),
            Some(&target_plant),
            "unknownMcpOperationId"
        ),
        Err(AuthorizationError::UnknownMcpOperationId { .. })
    ));

    assert!(matches!(
        gateway.authorize_write(
            Some(&actor),
            different_action(valid_operation.action()),
            Some(&target_plant),
            valid_operation.operation_id()
        ),
        Err(AuthorizationError::McpOperationActionMismatch { .. })
    ));
}

#[test]
fn employee_discovery_contract_supports_multi_day_preorder_and_deterministic_filters() {
    let spec = canonical_openapi_spec();
    let operation = operation_by_path_and_method(&spec, "/api/v1/employee/menus", "get");

    assert_eq!(
        operation["x-discovery-governance"]["timezone"],
        "Asia/Taipei"
    );
    assert_eq!(
        operation["x-discovery-governance"]["deterministicFiltering"],
        true
    );
    assert_eq!(
        operation["x-discovery-governance"]["recommendationAppliedInMvp"],
        false
    );
    assert_eq!(
        operation["x-discovery-governance"]["remainingQuantitySource"],
        "MENU_SUPPLY_POLICY_ALLOCATED_COUNTER"
    );
    assert_eq!(
        operation["responses"]["500"]["$ref"],
        "#/components/responses/InternalServerError"
    );

    let parameter_refs = operation["parameters"]
        .as_array()
        .expect("employee discovery parameters should be array")
        .iter()
        .map(|parameter| {
            parameter["$ref"]
                .as_str()
                .expect("employee discovery parameter must be a $ref")
                .to_owned()
        })
        .collect::<BTreeSet<_>>();
    assert_eq!(
        parameter_refs,
        BTreeSet::from([
            "#/components/parameters/PlantIdQuery".to_owned(),
            "#/components/parameters/DiscoveryViewQuery".to_owned(),
            "#/components/parameters/MenuDateQuery".to_owned(),
            "#/components/parameters/FromDateQuery".to_owned(),
            "#/components/parameters/ToDateQuery".to_owned(),
            "#/components/parameters/PageQuery".to_owned(),
            "#/components/parameters/PageSizeQuery".to_owned(),
            "#/components/parameters/MenuSortByQuery".to_owned(),
            "#/components/parameters/SortOrderQuery".to_owned(),
            "#/components/parameters/MenuSearchQuery".to_owned(),
            "#/components/parameters/MenuTypeFilterQuery".to_owned(),
            "#/components/parameters/HealthTagFilterQuery".to_owned(),
            "#/components/parameters/PriceMinMinorQuery".to_owned(),
            "#/components/parameters/PriceMaxMinorQuery".to_owned(),
            "#/components/parameters/RemainingQuantityFilterQuery".to_owned(),
            "#/components/parameters/RecommendationEnabledQuery".to_owned(),
        ])
    );

    let menu_page_required = spec["components"]["schemas"]["MenuPage"]["required"]
        .as_array()
        .expect("menu page required fields should be array")
        .iter()
        .map(|value| {
            value
                .as_str()
                .expect("required field should be string")
                .to_owned()
        })
        .collect::<BTreeSet<_>>();
    assert!(menu_page_required.contains("view"));
    assert!(menu_page_required.contains("days"));
    assert!(menu_page_required.contains("recommendationRequested"));
    assert!(menu_page_required.contains("recommendationApplied"));

    let menu_item_required = spec["components"]["schemas"]["MenuListItem"]["required"]
        .as_array()
        .expect("menu list item required fields should be array")
        .iter()
        .map(|value| {
            value
                .as_str()
                .expect("required field should be string")
                .to_owned()
        })
        .collect::<BTreeSet<_>>();
    assert!(menu_item_required.contains("menuType"));
    assert!(menu_item_required.contains("remainingQuantity"));
    assert!(menu_item_required.contains("preorderOpen"));
    assert!(menu_item_required.contains("preorderOpenDaysAhead"));
    assert!(menu_item_required.contains("modifyCancelCutoffMinuteOfDay"));

    let error_codes = spec["components"]["schemas"]["ErrorCode"]["enum"]
        .as_array()
        .expect("error code enum should exist")
        .iter()
        .map(|value| {
            value
                .as_str()
                .expect("error code enum value should be string")
                .to_owned()
        })
        .collect::<BTreeSet<_>>();
    assert!(error_codes.contains("UNSUPPORTED_PLANT_ID"));
    assert!(error_codes.contains("INVALID_MENU_DISCOVERY_QUERY"));
    assert!(error_codes.contains("TIME_RESOLUTION_FAILED"));
    assert!(error_codes.contains("MENU_DISCOVERY_INTERNAL_ERROR"));
}

#[test]
fn ordering_contract_enforces_taipei_window_governance_and_controlled_special_requests() {
    let spec = canonical_openapi_spec();

    let create_order_operation =
        operation_by_path_and_method(&spec, "/api/v1/employee/orders", "post");
    assert_eq!(
        create_order_operation["x-order-governance"]["timezone"],
        "Asia/Taipei"
    );
    assert_eq!(
        create_order_operation["x-order-governance"]["strictLifecycle"],
        true
    );
    assert_eq!(
        create_order_operation["x-order-governance"]["inventoryReservationMode"],
        "ATOMIC_IDEMPOTENT"
    );
    assert_eq!(
        create_order_operation["x-order-governance"]["preorderWindow"]["defaultOpenDaysAhead"],
        7
    );
    assert_eq!(
        create_order_operation["x-order-governance"]["modifyCancelCutoff"]["defaultRule"]
            ["minuteOfDay"],
        1020
    );
    assert_eq!(
        create_order_operation["x-order-governance"]["specialRequestPolicy"]["allowFreeText"],
        false
    );
    let timeline_includes = create_order_operation["x-order-governance"]["timeline"]["includes"]
        .as_array()
        .expect("timeline includes must be declared")
        .iter()
        .map(|value| {
            value
                .as_str()
                .expect("timeline include must be string")
                .to_owned()
        })
        .collect::<BTreeSet<_>>();
    assert_eq!(
        timeline_includes,
        BTreeSet::from([
            "CREATED".to_owned(),
            "MODIFIED".to_owned(),
            "CANCELLED".to_owned(),
            "SOLD_OUT".to_owned(),
            "REFUND_PENDING".to_owned(),
            "REFUNDED".to_owned(),
            "FULFILLED".to_owned(),
        ])
    );
    assert_eq!(
        create_order_operation["responses"]["500"]["$ref"],
        "#/components/responses/InternalServerError"
    );

    let patch_order_operation =
        operation_by_path_and_method(&spec, "/api/v1/employee/orders/{orderId}", "patch");
    assert_eq!(
        patch_order_operation["x-order-governance"]["timezone"],
        "Asia/Taipei"
    );
    let supported_operations = patch_order_operation["x-order-governance"]["supportedOperations"]
        .as_array()
        .expect("patch operation should advertise supported operations")
        .iter()
        .map(|value| {
            value
                .as_str()
                .expect("supported operation must be string")
                .to_owned()
        })
        .collect::<BTreeSet<_>>();
    assert_eq!(
        supported_operations,
        BTreeSet::from(["REPLACE_LINE_ITEMS".to_owned(), "CANCEL".to_owned()])
    );
    assert_eq!(
        patch_order_operation["responses"]["500"]["$ref"],
        "#/components/responses/InternalServerError"
    );

    let pickup_verify_operation = operation_by_path_and_method(
        &spec,
        "/api/v1/employee/orders/{orderId}/pickup-verifications",
        "post",
    );
    assert_eq!(
        pickup_verify_operation["x-order-governance"]["timezone"],
        "Asia/Taipei"
    );
    assert_eq!(
        pickup_verify_operation["x-order-governance"]["strictLifecycle"],
        true
    );
    assert_eq!(
        pickup_verify_operation["x-order-governance"]["pickupVerification"]["mechanism"],
        "TOTP_QR_SINGLE_USE"
    );
    assert_eq!(
        pickup_verify_operation["x-order-governance"]["pickupVerification"]["stepSeconds"],
        30
    );
    assert_eq!(
        pickup_verify_operation["x-order-governance"]["pickupVerification"]["maxClockSkewSteps"],
        1
    );
    assert_eq!(
        pickup_verify_operation["requestBody"]["content"]["application/json"]["schema"]["$ref"],
        "#/components/schemas/PickupVerificationRequest"
    );
    assert_eq!(
        pickup_verify_operation["responses"]["200"]["content"]["application/json"]["schema"]
            ["$ref"],
        "#/components/schemas/PickupVerificationResponse"
    );
    assert_eq!(
        pickup_verify_operation["responses"]["409"]["$ref"],
        "#/components/responses/Conflict"
    );

    let pickup_verification_code = &spec["components"]["schemas"]["PickupVerificationRequest"]
        ["properties"]["verificationCode"];
    assert_eq!(
        pickup_verification_code["pattern"],
        "^TOTP1:[0-9]{1,20}:[0-9]{6}$"
    );

    let patch_schema_variants = spec["components"]["schemas"]["EmployeeOrderPatchRequest"]["oneOf"]
        .as_array()
        .expect("patch request should be modeled as command union")
        .iter()
        .map(|variant| {
            variant["$ref"]
                .as_str()
                .expect("patch variant must be $ref")
                .to_owned()
        })
        .collect::<BTreeSet<_>>();
    assert_eq!(
        patch_schema_variants,
        BTreeSet::from([
            "#/components/schemas/EmployeeOrderReplaceLineItemsPatchRequest".to_owned(),
            "#/components/schemas/EmployeeOrderCancelPatchRequest".to_owned(),
        ])
    );

    let order_line_item_properties =
        &spec["components"]["schemas"]["OrderLineItemRequest"]["properties"];
    assert!(
        order_line_item_properties.get("note").is_none(),
        "free-text note must not exist in controlled special-request schema"
    );
    assert_eq!(order_line_item_properties["specialRequests"]["maxItems"], 3);
    assert_eq!(
        order_line_item_properties["specialRequests"]["items"]["$ref"],
        "#/components/schemas/SpecialRequestOption"
    );

    let special_request_options = spec["components"]["schemas"]["SpecialRequestOption"]["enum"]
        .as_array()
        .expect("special request enum must exist")
        .iter()
        .map(|value| {
            value
                .as_str()
                .expect("special request enum value must be string")
                .to_owned()
        })
        .collect::<BTreeSet<_>>();
    assert_eq!(
        special_request_options,
        BTreeSet::from([
            "LESS_RICE".to_owned(),
            "NO_GREEN_ONION".to_owned(),
            "SAUCE_ON_SIDE".to_owned(),
            "NO_UTENSILS".to_owned(),
            "EXTRA_SPICY".to_owned(),
        ])
    );

    let vendor_menu_schema = &spec["components"]["schemas"]["VendorMenuItem"];
    let vendor_required = vendor_menu_schema["required"]
        .as_array()
        .expect("vendor menu item required fields should be array")
        .iter()
        .map(|value| {
            value
                .as_str()
                .expect("required entry should be string")
                .to_owned()
        })
        .collect::<BTreeSet<_>>();
    assert!(vendor_required.contains("remainingQuantity"));
    assert!(vendor_required.contains("preorderOpenDaysAhead"));
    assert!(vendor_required.contains("modifyCancelCutoffMinuteOfDay"));
    assert_eq!(
        vendor_menu_schema["properties"]["imageUrl"]["format"],
        "uri"
    );

    let order_status_values = spec["components"]["schemas"]["EmployeeOrderStatus"]["enum"]
        .as_array()
        .expect("order status enum should exist")
        .iter()
        .map(|value| {
            value
                .as_str()
                .expect("status value must be string")
                .to_owned()
        })
        .collect::<BTreeSet<_>>();
    assert_eq!(
        order_status_values,
        BTreeSet::from([
            "PENDING".to_owned(),
            "MODIFIED".to_owned(),
            "CANCELLED".to_owned(),
            "SOLD_OUT".to_owned(),
            "REFUND_PENDING".to_owned(),
            "REFUNDED".to_owned(),
            "FULFILLED".to_owned(),
        ])
    );

    let employee_order_required = spec["components"]["schemas"]["EmployeeOrder"]["required"]
        .as_array()
        .expect("employee order required fields must be array")
        .iter()
        .map(|value| {
            value
                .as_str()
                .expect("required field must be string")
                .to_owned()
        })
        .collect::<BTreeSet<_>>();
    assert!(employee_order_required.contains("timeline"));
    assert_eq!(
        spec["components"]["schemas"]["EmployeeOrder"]["properties"]["timeline"]["items"]["$ref"],
        "#/components/schemas/OrderTimelineEvent"
    );
}

#[test]
fn openapi_export_produces_json_yaml_and_browsable_docs_artifacts() {
    let unique_suffix = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .expect("system clock must be after unix epoch")
        .as_nanos();
    let output_dir = std::env::temp_dir().join(format!("openapi-contract-{unique_suffix}"));

    let artifacts =
        write_openapi_artifacts(&output_dir).expect("contract artifact export should succeed");
    let openapi_json =
        std::fs::read_to_string(&artifacts.openapi_json).expect("openapi json should be written");
    let openapi_yaml =
        std::fs::read_to_string(&artifacts.openapi_yaml).expect("openapi yaml should be written");
    let docs_html =
        std::fs::read_to_string(&artifacts.docs_html).expect("docs html should be written");

    let parsed_json: Value = serde_json::from_str(&openapi_json).expect("json should parse");
    assert_eq!(parsed_json["openapi"], "3.1.0");
    assert!(openapi_yaml.contains("openapi: 3.1.0"));
    assert!(docs_html.contains("redoc"));
    assert!(docs_html.contains("openapi.json"));

    std::fs::remove_dir_all(&output_dir).expect("temporary output directory should be removable");
}
