use crate::access::{
    AccessController, Action, AuthorizationError, AuthorizedWriteOperation, TransportLayer,
};
use crate::contract::{HttpMethod, HttpOperation};
use crate::identity::{AuthenticatedActorContext, PlantId};
use crate::menu_supply_window::{
    MenuSupplyPolicy, MenuSupplyWindowError, OrderId, OrderLineItemRequest, OrderMutation,
    VendorMenuItem,
};
use crate::observability::{TelemetryOutcome, TelemetryService};
use crate::vendor_compliance::{VendorComplianceLifecycle, VendorId};
use crate::vendor_delivery_mapping::{
    TaipeiBusinessMoment, VendorPlantDeliveryError, VendorPlantDeliveryPolicy,
};

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

const RUNTIME_HTTP_ROUTES: [RuntimeHttpRoute; 14] = [
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
        "/api/v1/admin/vendor-plant-delivery-mappings",
        "listVendorPlantDeliveryMappings",
    ),
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
        HttpMethod::Put,
        "/api/v1/admin/vendors/{vendorId}/plant-delivery-mappings/{mappingId}",
        "upsertVendorPlantDeliveryMapping",
    ),
    RuntimeHttpRoute::new(
        HttpMethod::Delete,
        "/api/v1/admin/vendors/{vendorId}/plant-delivery-mappings/{mappingId}",
        "deleteVendorPlantDeliveryMapping",
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
        let telemetry = TelemetryService::HttpApi.begin_internal_operation(
            operation.operation_id(),
            actor.map(|value| value.actor_id().as_str()),
            target_plant.map(PlantId::as_str),
        );

        let result = (|| {
            let expected_action = operation.write_action().ok_or(
                AuthorizationError::HttpOperationIsNotWriteOperation {
                    operation_id: operation_id.clone(),
                },
            )?;
            if expected_action != action {
                return Err(AuthorizationError::HttpOperationActionMismatch {
                    operation_id: operation_id.clone(),
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
        })();

        telemetry.finish(if result.is_ok() {
            TelemetryOutcome::Success
        } else {
            TelemetryOutcome::Error
        });
        result
    }
}

pub struct HttpDeliveryExecutionGateway<'a> {
    compliance_lifecycle: &'a VendorComplianceLifecycle,
    delivery_policy: &'a VendorPlantDeliveryPolicy,
}

impl<'a> HttpDeliveryExecutionGateway<'a> {
    pub fn new(
        compliance_lifecycle: &'a VendorComplianceLifecycle,
        delivery_policy: &'a VendorPlantDeliveryPolicy,
    ) -> Self {
        Self {
            compliance_lifecycle,
            delivery_policy,
        }
    }

    pub fn execute_list_employee_menus_for_browse(
        &self,
        plant_id: &PlantId,
        at: TaipeiBusinessMoment,
    ) -> Vec<VendorId> {
        let telemetry = TelemetryService::HttpApi.begin_internal_operation(
            "listEmployeeMenus:browse",
            None,
            Some(plant_id.as_str()),
        );
        let result = self.delivery_policy.employee_visible_vendor_ids_for_browse(
            self.compliance_lifecycle,
            plant_id,
            at,
        );
        telemetry.finish(TelemetryOutcome::Success);
        result
    }

    pub fn execute_list_employee_menus_for_search(
        &self,
        plant_id: &PlantId,
        at: TaipeiBusinessMoment,
    ) -> Vec<VendorId> {
        let telemetry = TelemetryService::HttpApi.begin_internal_operation(
            "listEmployeeMenus:search",
            None,
            Some(plant_id.as_str()),
        );
        let result = self.delivery_policy.employee_visible_vendor_ids_for_search(
            self.compliance_lifecycle,
            plant_id,
            at,
        );
        telemetry.finish(TelemetryOutcome::Success);
        result
    }

    pub fn execute_create_employee_order_deliverability_check(
        &self,
        vendor_id: &VendorId,
        plant_id: &PlantId,
        at: TaipeiBusinessMoment,
    ) -> Result<(), VendorPlantDeliveryError> {
        let telemetry = TelemetryService::HttpApi.begin_internal_operation(
            "createEmployeeOrder:deliverability",
            None,
            Some(plant_id.as_str()),
        );
        let result = self.delivery_policy.ensure_vendor_deliverable_for_order(
            self.compliance_lifecycle,
            vendor_id,
            plant_id,
            at,
        );
        telemetry.finish(if result.is_ok() {
            TelemetryOutcome::Success
        } else {
            TelemetryOutcome::Error
        });
        result
    }

    pub fn execute_update_employee_order_deliverability_check(
        &self,
        vendor_id: &VendorId,
        plant_id: &PlantId,
        at: TaipeiBusinessMoment,
    ) -> Result<(), VendorPlantDeliveryError> {
        let telemetry = TelemetryService::HttpApi.begin_internal_operation(
            "updateEmployeeOrder:deliverability",
            None,
            Some(plant_id.as_str()),
        );
        let result = self.delivery_policy.ensure_vendor_deliverable_for_order(
            self.compliance_lifecycle,
            vendor_id,
            plant_id,
            at,
        );
        telemetry.finish(if result.is_ok() {
            TelemetryOutcome::Success
        } else {
            TelemetryOutcome::Error
        });
        result
    }
}

pub struct HttpOrderingExecutionGateway<'a> {
    compliance_lifecycle: &'a VendorComplianceLifecycle,
    delivery_policy: &'a VendorPlantDeliveryPolicy,
    menu_supply_policy: &'a MenuSupplyPolicy,
}

impl<'a> HttpOrderingExecutionGateway<'a> {
    pub fn new(
        compliance_lifecycle: &'a VendorComplianceLifecycle,
        delivery_policy: &'a VendorPlantDeliveryPolicy,
        menu_supply_policy: &'a MenuSupplyPolicy,
    ) -> Self {
        Self {
            compliance_lifecycle,
            delivery_policy,
            menu_supply_policy,
        }
    }

    pub fn execute_create_employee_order(
        &self,
        order_id: OrderId,
        vendor_id: &VendorId,
        plant_id: &PlantId,
        delivery_epoch_day: i32,
        line_items: Vec<OrderLineItemRequest>,
        at: TaipeiBusinessMoment,
    ) -> Result<(), HttpOrderExecutionError> {
        let telemetry = TelemetryService::HttpApi.begin_internal_operation(
            "createEmployeeOrder",
            None,
            Some(plant_id.as_str()),
        );
        let result = (|| {
            self.delivery_policy
                .ensure_vendor_deliverable_for_order(
                    self.compliance_lifecycle,
                    vendor_id,
                    plant_id,
                    at,
                )
                .map_err(HttpOrderExecutionError::Deliverability)?;

            self.menu_supply_policy
                .create_order(order_id, vendor_id, delivery_epoch_day, line_items, at)
                .map_err(HttpOrderExecutionError::MenuSupply)?;

            Ok(())
        })();
        telemetry.finish(if result.is_ok() {
            TelemetryOutcome::Success
        } else {
            TelemetryOutcome::Error
        });
        result
    }

    pub fn execute_update_employee_order(
        &self,
        order_id: &OrderId,
        vendor_id: &VendorId,
        plant_id: &PlantId,
        mutation: OrderMutation,
        at: TaipeiBusinessMoment,
    ) -> Result<(), HttpOrderExecutionError> {
        let telemetry = TelemetryService::HttpApi.begin_internal_operation(
            "updateEmployeeOrder",
            None,
            Some(plant_id.as_str()),
        );
        let result = (|| {
            if !mutation.is_employee_patch_operation() {
                return Err(HttpOrderExecutionError::UnsupportedEmployeeMutation {
                    operation: mutation.operation_name(),
                });
            }

            self.delivery_policy
                .ensure_vendor_deliverable_for_order(
                    self.compliance_lifecycle,
                    vendor_id,
                    plant_id,
                    at,
                )
                .map_err(HttpOrderExecutionError::Deliverability)?;

            self.menu_supply_policy
                .update_order(order_id, mutation, at)
                .map_err(HttpOrderExecutionError::MenuSupply)?;

            Ok(())
        })();
        telemetry.finish(if result.is_ok() {
            TelemetryOutcome::Success
        } else {
            TelemetryOutcome::Error
        });
        result
    }
}

pub struct HttpVendorMenuExecutionGateway<'a> {
    menu_supply_policy: &'a MenuSupplyPolicy,
}

impl<'a> HttpVendorMenuExecutionGateway<'a> {
    pub fn new(menu_supply_policy: &'a MenuSupplyPolicy) -> Self {
        Self { menu_supply_policy }
    }

    pub fn execute_upsert_vendor_menu_item(
        &self,
        actor: &AuthenticatedActorContext,
        menu_item: VendorMenuItem,
    ) -> Result<(), MenuSupplyWindowError> {
        let telemetry = TelemetryService::HttpApi.begin_internal_operation(
            "upsertVendorMenuItem",
            Some(actor.actor_id().as_str()),
            None,
        );
        let result = self.menu_supply_policy.upsert_menu_item(actor, menu_item);
        telemetry.finish(if result.is_ok() {
            TelemetryOutcome::Success
        } else {
            TelemetryOutcome::Error
        });
        result
    }
}

#[derive(Debug)]
pub enum HttpOrderExecutionError {
    Deliverability(VendorPlantDeliveryError),
    MenuSupply(MenuSupplyWindowError),
    UnsupportedEmployeeMutation { operation: &'static str },
}

impl std::fmt::Display for HttpOrderExecutionError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::Deliverability(error) => write!(f, "deliverability enforcement failed: {error}"),
            Self::MenuSupply(error) => write!(f, "menu supply enforcement failed: {error}"),
            Self::UnsupportedEmployeeMutation { operation } => write!(
                f,
                "employee HTTP update path does not allow lifecycle operation {operation}"
            ),
        }
    }
}

impl std::error::Error for HttpOrderExecutionError {}
