use std::fmt;
use std::sync::Arc;

use crate::audit::AuditIdentityLink;
use crate::identity::{AuthenticatedActorContext, PlantId, Role};

#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum Action {
    PlaceEmployeeOrder,
    ManageVendorMenu,
    ApproveVendorEnrollment,
    ExportPayrollDeductions,
}

impl Action {
    pub const fn is_write(self) -> bool {
        true
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum TransportLayer {
    Http,
    Mcp,
}

pub trait AuthorizationPolicyEngine: Send + Sync {
    fn authorize(
        &self,
        actor: &AuthenticatedActorContext,
        action: Action,
        target_plant: Option<&PlantId>,
    ) -> Result<(), AuthorizationError>;
}

#[derive(Debug, Default)]
pub struct CentralAuthorizationPolicyEngine;

impl AuthorizationPolicyEngine for CentralAuthorizationPolicyEngine {
    fn authorize(
        &self,
        actor: &AuthenticatedActorContext,
        action: Action,
        target_plant: Option<&PlantId>,
    ) -> Result<(), AuthorizationError> {
        if !role_can_execute(actor.role(), action) {
            return Err(AuthorizationError::RoleNotPermitted {
                role: actor.role(),
                action,
            });
        }

        if let Some(plant) = target_plant {
            if !actor.plant_scope().contains(plant) {
                return Err(AuthorizationError::TargetPlantOutOfScope {
                    actor_id: actor.actor_id().clone(),
                    target_plant: plant.clone(),
                });
            }
        }

        Ok(())
    }
}

fn role_can_execute(role: Role, action: Action) -> bool {
    matches!(
        (role, action),
        (Role::Employee, Action::PlaceEmployeeOrder)
            | (Role::VendorOperator, Action::ManageVendorMenu)
            | (Role::CommitteeAdmin, Action::ApproveVendorEnrollment)
            | (Role::PayrollOperator, Action::ExportPayrollDeductions)
    )
}

#[derive(Clone)]
pub struct AccessController {
    policy_engine: Arc<dyn AuthorizationPolicyEngine>,
}

impl AccessController {
    pub fn new(policy_engine: Arc<dyn AuthorizationPolicyEngine>) -> Self {
        Self { policy_engine }
    }

    pub fn with_default_policy() -> Self {
        Self::new(Arc::new(CentralAuthorizationPolicyEngine))
    }

    pub fn authorize_write(
        &self,
        actor: Option<&AuthenticatedActorContext>,
        action: Action,
        target_plant: Option<&PlantId>,
        transport: TransportLayer,
        operation_id: impl Into<String>,
    ) -> Result<AuthorizedWriteOperation, AuthorizationError> {
        let operation_id = operation_id.into();
        if operation_id.trim().is_empty() {
            return Err(AuthorizationError::InvalidOperationId);
        }
        if !action.is_write() {
            return Err(AuthorizationError::ActionIsNotWriteOperation(action));
        }

        let actor = actor
            .ok_or(AuthorizationError::MissingAuthenticatedActorContext { action, transport })?;

        self.policy_engine.authorize(actor, action, target_plant)?;

        Ok(AuthorizedWriteOperation {
            action,
            transport,
            actor: actor.clone(),
            audit_identity: AuditIdentityLink::from_actor(actor, operation_id),
        })
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AuthorizedWriteOperation {
    action: Action,
    transport: TransportLayer,
    actor: AuthenticatedActorContext,
    audit_identity: AuditIdentityLink,
}

impl AuthorizedWriteOperation {
    pub fn action(&self) -> Action {
        self.action
    }

    pub fn transport(&self) -> TransportLayer {
        self.transport
    }

    pub fn actor(&self) -> &AuthenticatedActorContext {
        &self.actor
    }

    pub fn audit_identity(&self) -> &AuditIdentityLink {
        &self.audit_identity
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum AuthorizationError {
    MissingAuthenticatedActorContext {
        action: Action,
        transport: TransportLayer,
    },
    RoleNotPermitted {
        role: Role,
        action: Action,
    },
    TargetPlantOutOfScope {
        actor_id: crate::identity::ActorId,
        target_plant: PlantId,
    },
    UnknownHttpOperationId {
        operation_id: String,
    },
    HttpOperationIsNotWriteOperation {
        operation_id: String,
    },
    HttpOperationActionMismatch {
        operation_id: String,
        expected_action: Action,
        provided_action: Action,
    },
    ActionIsNotWriteOperation(Action),
    InvalidOperationId,
}

impl fmt::Display for AuthorizationError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::MissingAuthenticatedActorContext { action, transport } => write!(
                f,
                "missing authenticated actor context for {transport:?} write action {action:?}"
            ),
            Self::RoleNotPermitted { role, action } => {
                write!(f, "role {role:?} is not allowed to execute {action:?}")
            }
            Self::TargetPlantOutOfScope {
                actor_id,
                target_plant,
            } => write!(
                f,
                "actor {actor_id} is not authorized for plant {}",
                target_plant.as_str()
            ),
            Self::UnknownHttpOperationId { operation_id } => {
                write!(f, "http operation id {operation_id} is not defined in the contract")
            }
            Self::HttpOperationIsNotWriteOperation { operation_id } => {
                write!(f, "http operation id {operation_id} is not declared as a write operation")
            }
            Self::HttpOperationActionMismatch {
                operation_id,
                expected_action,
                provided_action,
            } => write!(
                f,
                "http operation id {operation_id} expects action {expected_action:?}, got {provided_action:?}"
            ),
            Self::ActionIsNotWriteOperation(action) => {
                write!(f, "action {action:?} is not registered as write operation")
            }
            Self::InvalidOperationId => f.write_str("operation id must not be empty"),
        }
    }
}

impl std::error::Error for AuthorizationError {}
