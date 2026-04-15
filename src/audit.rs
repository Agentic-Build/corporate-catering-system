use crate::identity::{ActorId, AuthenticatedActorContext, AuthenticationSource, PlantScope, Role};

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AuditIdentityLink {
    actor_id: ActorId,
    role: Role,
    authentication_source: AuthenticationSource,
    plant_scope: PlantScope,
    operation_id: String,
}

impl AuditIdentityLink {
    pub fn from_actor(actor: &AuthenticatedActorContext, operation_id: impl Into<String>) -> Self {
        Self {
            actor_id: actor.actor_id().clone(),
            role: actor.role(),
            authentication_source: actor.authentication_source(),
            plant_scope: actor.plant_scope().clone(),
            operation_id: operation_id.into(),
        }
    }

    pub fn actor_id(&self) -> &ActorId {
        &self.actor_id
    }

    pub fn role(&self) -> Role {
        self.role
    }

    pub fn authentication_source(&self) -> AuthenticationSource {
        self.authentication_source
    }

    pub fn plant_scope(&self) -> &PlantScope {
        &self.plant_scope
    }

    pub fn operation_id(&self) -> &str {
        &self.operation_id
    }
}
