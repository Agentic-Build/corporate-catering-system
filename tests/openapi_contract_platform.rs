use std::collections::BTreeSet;
use std::time::{SystemTime, UNIX_EPOCH};

use corporate_catering_system::access::{AccessController, Action, AuthorizationError};
use corporate_catering_system::contract::{
    canonical_openapi_spec, write_openapi_artifacts, HttpAudience, HttpOperation,
};
use corporate_catering_system::identity::{
    ActorId, AuthenticatedActorContext, AuthenticationSource, PlantId, PlantScope, Role,
};
use corporate_catering_system::transport::http::HttpAuthorizationGateway;
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
        BTreeSet::from(["CORPORATE_SSO".to_owned(), "VENDOR_ACCOUNT_MFA".to_owned()])
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
fn http_gateway_enforces_contract_operation_id_and_action_mapping() {
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
