use crate::access::{
    AccessController, Action, AuthorizationError, AuthorizedWriteOperation, TransportLayer,
};
use crate::contract::HttpOperation;
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
        let operation_id = operation_id.into();
        let operation = HttpOperation::from_operation_id(&operation_id).ok_or(
            AuthorizationError::UnknownHttpOperationId {
                operation_id: operation_id.clone(),
            },
        )?;
        let expected_action = operation.write_action().ok_or(
            AuthorizationError::HttpOperationIsNotWriteOperation {
                operation_id: operation_id.clone(),
            },
        )?;
        if expected_action != action {
            return Err(AuthorizationError::HttpOperationActionMismatch {
                operation_id,
                expected_action,
                provided_action: action,
            });
        }

        self.access_controller.authorize_write(
            actor,
            action,
            target_plant,
            TransportLayer::Http,
            operation.operation_id(),
        )
    }
}
