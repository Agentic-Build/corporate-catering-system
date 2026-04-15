use std::sync::atomic::{AtomicUsize, Ordering};
use std::sync::Arc;

use corporate_catering_system::access::{
    AccessController, Action, AuthorizationError, AuthorizationPolicyEngine,
    CentralAuthorizationPolicyEngine, TransportLayer,
};
use corporate_catering_system::identity::{
    ActorId, AuthenticatedActorContext, AuthenticationSource, IdentityContextError, PlantId,
    PlantScope, Role,
};
use corporate_catering_system::transport::http::HttpAuthorizationGateway;
use corporate_catering_system::transport::mcp::McpAuthorizationGateway;

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
        actor_id("emp-001"),
        Role::Employee,
        restricted_scope(&["fab-a"]),
        AuthenticationSource::CorporateSso,
    )
    .expect("employee actor should be valid")
}

fn vendor_actor() -> AuthenticatedActorContext {
    AuthenticatedActorContext::new(
        actor_id("vendor-op-001"),
        Role::VendorOperator,
        restricted_scope(&["fab-a"]),
        AuthenticationSource::VendorAccountMfa,
    )
    .expect("vendor actor should be valid")
}

fn committee_actor() -> AuthenticatedActorContext {
    AuthenticatedActorContext::new(
        actor_id("committee-001"),
        Role::CommitteeAdmin,
        PlantScope::all(),
        AuthenticationSource::CorporateSso,
    )
    .expect("committee actor should be valid")
}

fn payroll_actor() -> AuthenticatedActorContext {
    AuthenticatedActorContext::new(
        actor_id("payroll-001"),
        Role::PayrollOperator,
        PlantScope::all(),
        AuthenticationSource::CorporateSso,
    )
    .expect("payroll actor should be valid")
}

#[test]
fn identity_model_enforces_only_supported_authentication_sources() {
    assert!(AuthenticatedActorContext::new(
        actor_id("employee-ok"),
        Role::Employee,
        restricted_scope(&["fab-a"]),
        AuthenticationSource::CorporateSso,
    )
    .is_ok());
    assert!(AuthenticatedActorContext::new(
        actor_id("committee-ok"),
        Role::CommitteeAdmin,
        PlantScope::all(),
        AuthenticationSource::CorporateSso,
    )
    .is_ok());
    assert!(AuthenticatedActorContext::new(
        actor_id("vendor-ok"),
        Role::VendorOperator,
        restricted_scope(&["fab-a"]),
        AuthenticationSource::VendorAccountMfa,
    )
    .is_ok());

    assert!(matches!(
        AuthenticatedActorContext::new(
            actor_id("employee-invalid"),
            Role::Employee,
            restricted_scope(&["fab-a"]),
            AuthenticationSource::VendorAccountMfa,
        ),
        Err(IdentityContextError::UnsupportedIdentitySource {
            role: Role::Employee,
            source: AuthenticationSource::VendorAccountMfa,
        })
    ));

    assert!(matches!(
        AuthenticatedActorContext::new(
            actor_id("vendor-invalid"),
            Role::VendorOperator,
            restricted_scope(&["fab-a"]),
            AuthenticationSource::CorporateSso,
        ),
        Err(IdentityContextError::UnsupportedIdentitySource {
            role: Role::VendorOperator,
            source: AuthenticationSource::CorporateSso,
        })
    ));
}

#[test]
fn authenticated_context_carries_actor_role_and_plant_scope() {
    let actor = employee_actor();

    assert_eq!(actor.actor_id().as_str(), "emp-001");
    assert_eq!(actor.role(), Role::Employee);
    assert_eq!(
        actor.authentication_source(),
        AuthenticationSource::CorporateSso
    );
    assert!(actor.plant_scope().contains(&plant_id("fab-a")));
    assert!(!actor.plant_scope().contains(&plant_id("fab-b")));
}

#[test]
fn rbac_allows_and_denies_expected_actions_for_all_roles() {
    let policy = CentralAuthorizationPolicyEngine;
    let target_plant = plant_id("fab-a");

    assert!(policy
        .authorize(
            &employee_actor(),
            Action::PlaceEmployeeOrder,
            Some(&target_plant),
        )
        .is_ok());
    assert!(policy
        .authorize(
            &vendor_actor(),
            Action::ManageVendorMenu,
            Some(&target_plant),
        )
        .is_ok());
    assert!(policy
        .authorize(
            &committee_actor(),
            Action::ApproveVendorEnrollment,
            Some(&target_plant),
        )
        .is_ok());
    assert!(policy
        .authorize(
            &payroll_actor(),
            Action::ExportPayrollDeductions,
            Some(&target_plant),
        )
        .is_ok());

    assert!(matches!(
        policy.authorize(
            &employee_actor(),
            Action::ManageVendorMenu,
            Some(&target_plant),
        ),
        Err(AuthorizationError::RoleNotPermitted {
            role: Role::Employee,
            action: Action::ManageVendorMenu,
        })
    ));
    assert!(matches!(
        policy.authorize(
            &vendor_actor(),
            Action::ApproveVendorEnrollment,
            Some(&target_plant),
        ),
        Err(AuthorizationError::RoleNotPermitted {
            role: Role::VendorOperator,
            action: Action::ApproveVendorEnrollment,
        })
    ));
    assert!(matches!(
        policy.authorize(
            &committee_actor(),
            Action::ExportPayrollDeductions,
            Some(&target_plant),
        ),
        Err(AuthorizationError::RoleNotPermitted {
            role: Role::CommitteeAdmin,
            action: Action::ExportPayrollDeductions,
        })
    ));
    assert!(matches!(
        policy.authorize(
            &payroll_actor(),
            Action::PlaceEmployeeOrder,
            Some(&target_plant),
        ),
        Err(AuthorizationError::RoleNotPermitted {
            role: Role::PayrollOperator,
            action: Action::PlaceEmployeeOrder,
        })
    ));
}

#[test]
fn policy_enforces_plant_scope_boundaries() {
    let policy = CentralAuthorizationPolicyEngine;
    let actor = employee_actor();

    assert!(policy
        .authorize(&actor, Action::PlaceEmployeeOrder, Some(&plant_id("fab-a")))
        .is_ok());
    assert!(matches!(
        policy.authorize(&actor, Action::PlaceEmployeeOrder, Some(&plant_id("fab-b"))),
        Err(AuthorizationError::TargetPlantOutOfScope { .. })
    ));
}

#[test]
fn write_operations_require_authenticated_actor_context_and_auditable_link() {
    let access_controller = AccessController::with_default_policy();
    let http_gateway = HttpAuthorizationGateway::new(access_controller.clone());
    let mcp_gateway = McpAuthorizationGateway::new(access_controller);
    let target_plant = plant_id("fab-a");

    assert!(matches!(
        http_gateway.authorize_write(
            None,
            Action::PlaceEmployeeOrder,
            Some(&target_plant),
            "http-write-1",
        ),
        Err(AuthorizationError::MissingAuthenticatedActorContext {
            action: Action::PlaceEmployeeOrder,
            transport: TransportLayer::Http,
        })
    ));

    let employee = employee_actor();
    let authorized = mcp_gateway
        .authorize_write(
            Some(&employee),
            Action::PlaceEmployeeOrder,
            Some(&target_plant),
            "mcp-write-1",
        )
        .expect("authorized write should succeed");

    assert_eq!(authorized.actor().actor_id(), employee.actor_id());
    assert_eq!(authorized.audit_identity().actor_id(), employee.actor_id());
    assert_eq!(authorized.audit_identity().role(), Role::Employee);
    assert_eq!(
        authorized.audit_identity().authentication_source(),
        AuthenticationSource::CorporateSso
    );
    assert_eq!(authorized.audit_identity().operation_id(), "mcp-write-1");
    assert_eq!(authorized.transport(), TransportLayer::Mcp);
}

#[test]
fn http_and_mcp_paths_share_the_same_policy_engine() {
    #[derive(Debug)]
    struct CountingPolicyEngine {
        inner: CentralAuthorizationPolicyEngine,
        calls: Arc<AtomicUsize>,
    }

    impl AuthorizationPolicyEngine for CountingPolicyEngine {
        fn authorize(
            &self,
            actor: &AuthenticatedActorContext,
            action: Action,
            target_plant: Option<&PlantId>,
        ) -> Result<(), AuthorizationError> {
            self.calls.fetch_add(1, Ordering::SeqCst);
            self.inner.authorize(actor, action, target_plant)
        }
    }

    let calls = Arc::new(AtomicUsize::new(0));
    let shared_engine = Arc::new(CountingPolicyEngine {
        inner: CentralAuthorizationPolicyEngine,
        calls: Arc::clone(&calls),
    });

    let access_controller = AccessController::new(shared_engine);
    let http_gateway = HttpAuthorizationGateway::new(access_controller.clone());
    let mcp_gateway = McpAuthorizationGateway::new(access_controller);
    let employee = employee_actor();
    let target_plant = plant_id("fab-a");

    http_gateway
        .authorize_write(
            Some(&employee),
            Action::PlaceEmployeeOrder,
            Some(&target_plant),
            "http-write-2",
        )
        .expect("http write should succeed");
    mcp_gateway
        .authorize_write(
            Some(&employee),
            Action::PlaceEmployeeOrder,
            Some(&target_plant),
            "mcp-write-2",
        )
        .expect("mcp write should succeed");

    assert_eq!(calls.load(Ordering::SeqCst), 2);
}
