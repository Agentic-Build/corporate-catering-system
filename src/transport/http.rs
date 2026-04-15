use crate::access::{
    AccessController, Action, AuthorizationError, AuthorizedWriteOperation, TransportLayer,
};
use crate::identity::{AuthenticatedActorContext, PlantId};

#[derive(Clone)]
pub struct HttpAuthorizationGateway {
    access_controller: AccessController,
}

impl HttpAuthorizationGateway {
    pub fn new(access_controller: AccessController) -> Self {
        Self { access_controller }
    }

    pub fn authorize_write(
        &self,
        actor: Option<&AuthenticatedActorContext>,
        action: Action,
        target_plant: Option<&PlantId>,
        operation_id: impl Into<String>,
    ) -> Result<AuthorizedWriteOperation, AuthorizationError> {
        self.access_controller.authorize_write(
            actor,
            action,
            target_plant,
            TransportLayer::Http,
            operation_id,
        )
    }
}
