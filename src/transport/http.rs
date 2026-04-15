use crate::access::{
    AccessController, Action, AuthorizationError, AuthorizedWriteOperation, TransportLayer,
};
use crate::contract::{HttpMethod, HttpOperation};
use crate::identity::{AuthenticatedActorContext, PlantId};

#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub struct RuntimeHttpRoute {
    method: HttpMethod,
    path: &'static str,
    operation_id: &'static str,
}

impl RuntimeHttpRoute {
    pub const fn new(method: HttpMethod, path: &'static str, operation_id: &'static str) -> Self {
        Self {
            method,
            path,
            operation_id,
        }
    }

    pub const fn method(self) -> HttpMethod {
        self.method
    }

    pub const fn path(self) -> &'static str {
        self.path
    }

    pub const fn operation_id(self) -> &'static str {
        self.operation_id
    }
}

const RUNTIME_HTTP_ROUTES: [RuntimeHttpRoute; 11] = [
    RuntimeHttpRoute::new(
        HttpMethod::Get,
        "/api/v1/employee/menus",
        "listEmployeeMenus",
    ),
    RuntimeHttpRoute::new(
        HttpMethod::Post,
        "/api/v1/employee/orders",
        "createEmployeeOrder",
    ),
    RuntimeHttpRoute::new(
        HttpMethod::Patch,
        "/api/v1/employee/orders/{orderId}",
        "updateEmployeeOrder",
    ),
    RuntimeHttpRoute::new(HttpMethod::Get, "/api/v1/vendor/orders", "listVendorOrders"),
    RuntimeHttpRoute::new(
        HttpMethod::Put,
        "/api/v1/vendor/menu-items/{menuItemId}",
        "upsertVendorMenuItem",
    ),
    RuntimeHttpRoute::new(HttpMethod::Get, "/api/v1/admin/vendors", "listAdminVendors"),
    RuntimeHttpRoute::new(
        HttpMethod::Get,
        "/api/v1/admin/compliance/document-templates",
        "listComplianceDocumentTemplates",
    ),
    RuntimeHttpRoute::new(
        HttpMethod::Put,
        "/api/v1/admin/compliance/document-templates/{vendorCategory}/{templateId}",
        "upsertComplianceDocumentTemplate",
    ),
    RuntimeHttpRoute::new(
        HttpMethod::Post,
        "/api/v1/admin/vendors/{vendorId}/reviews",
        "reviewVendorApplication",
    ),
    RuntimeHttpRoute::new(
        HttpMethod::Post,
        "/api/v1/admin/compliance/lifecycle/executions",
        "runVendorComplianceLifecycle",
    ),
    RuntimeHttpRoute::new(
        HttpMethod::Get,
        "/api/v1/integrations/payroll/deductions",
        "exportPayrollDeductions",
    ),
];

pub fn runtime_http_routes() -> &'static [RuntimeHttpRoute] {
    &RUNTIME_HTTP_ROUTES
}

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
