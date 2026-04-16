use std::collections::BTreeSet;

use crate::access::{
    AccessController, Action, AuthorizationError, AuthorizedWriteOperation, TransportLayer,
};
use crate::audit::{
    AuditInvestigationFilter, AuditPurgeReport, AuditTimestamp, AuditTrailError,
    ImmutableAuditEvidence, ImmutableAuditTrail, ResponsibilityAttribution,
};
use crate::contract::{HttpMethod, HttpOperation};
use crate::identity::{AuthenticatedActorContext, PlantId};
use crate::menu_supply_window::{
    EmployeeMenuDiscoveryEntry, MenuSupplyPolicy, MenuSupplyWindowError, OrderId,
    OrderLineItemRequest, OrderMutation, VendorMenuItem,
};
use crate::observability::{TelemetryOutcome, TelemetryService};
use crate::vendor_compliance::{VendorComplianceLifecycle, VendorId};
use crate::vendor_delivery_mapping::{
    TaipeiBusinessMoment, VendorPlantDeliveryError, VendorPlantDeliveryPolicy,
};
use crate::vendor_fulfillment::{
    FulfillmentBatchId, FulfillmentDeliveryStatus, VendorFulfillmentBatchSnapshot,
    VendorFulfillmentBoardSnapshot, VendorFulfillmentError, VendorFulfillmentPolicy,
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

const RUNTIME_HTTP_ROUTES: [RuntimeHttpRoute; 27] = [
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
    RuntimeHttpRoute::new(
        HttpMethod::Post,
        "/api/v1/employee/orders/{orderId}/pickup-verifications",
        "verifyPickupOrder",
    ),
    RuntimeHttpRoute::new(
        HttpMethod::Get,
        "/api/v1/employee/orders/{orderId}/payroll-ledger",
        "getEmployeeOrderPayrollLedger",
    ),
    RuntimeHttpRoute::new(
        HttpMethod::Post,
        "/api/v1/employee/orders/{orderId}/disputes",
        "createEmployeeOrderDispute",
    ),
    RuntimeHttpRoute::new(HttpMethod::Get, "/api/v1/vendor/orders", "listVendorOrders"),
    RuntimeHttpRoute::new(
        HttpMethod::Get,
        "/api/v1/vendor/fulfillment-board",
        "listVendorFulfillmentBoard",
    ),
    RuntimeHttpRoute::new(
        HttpMethod::Put,
        "/api/v1/vendor/menu-items/{menuItemId}",
        "upsertVendorMenuItem",
    ),
    RuntimeHttpRoute::new(
        HttpMethod::Post,
        "/api/v1/vendor/orders/{orderId}/delivery-status",
        "advanceVendorFulfillmentDeliveryStatus",
    ),
    RuntimeHttpRoute::new(
        HttpMethod::Post,
        "/api/v1/vendor/fulfillment-batches",
        "createVendorFulfillmentExportBatch",
    ),
    RuntimeHttpRoute::new(
        HttpMethod::Get,
        "/api/v1/vendor/fulfillment-batches/{batchId}",
        "getVendorFulfillmentExportBatch",
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
        "/api/v1/admin/audit/investigations",
        "queryAuditInvestigations",
    ),
    RuntimeHttpRoute::new(
        HttpMethod::Get,
        "/api/v1/admin/audit/responsibilities",
        "queryAuditResponsibilities",
    ),
    RuntimeHttpRoute::new(
        HttpMethod::Post,
        "/api/v1/admin/audit/retention-purge",
        "purgeAuditEvidence",
    ),
    RuntimeHttpRoute::new(
        HttpMethod::Patch,
        "/api/v1/admin/payroll/disputes/{disputeId}",
        "updateAdminPayrollDispute",
    ),
    RuntimeHttpRoute::new(
        HttpMethod::Post,
        "/api/v1/admin/payroll/retention-purge",
        "purgePayrollData",
    ),
    RuntimeHttpRoute::new(
        HttpMethod::Get,
        "/api/v1/integrations/payroll/deductions",
        "exportPayrollDeductions",
    ),
    RuntimeHttpRoute::new(
        HttpMethod::Post,
        "/api/v1/integrations/payroll/sftp-batches/{batchId}/hr-api-sync",
        "syncPayrollHrApiAdjunct",
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

pub struct HttpAuditInvestigationExecutionGateway {
    audit_trail: ImmutableAuditTrail,
}

impl HttpAuditInvestigationExecutionGateway {
    pub fn new(audit_trail: ImmutableAuditTrail) -> Self {
        Self { audit_trail }
    }

    pub fn execute_investigation_query(
        &self,
        investigator: &AuthenticatedActorContext,
        filter: &AuditInvestigationFilter,
    ) -> Result<Vec<ImmutableAuditEvidence>, AuditTrailError> {
        let telemetry = TelemetryService::ComplianceWorker.begin_operation(
            "queryAuditInvestigations",
            Some(investigator.actor_id().as_str()),
            None::<&str>,
        );
        let result = self.audit_trail.investigation_query(investigator, filter);
        telemetry.finish(if result.is_ok() {
            TelemetryOutcome::Success
        } else {
            TelemetryOutcome::Error
        });
        result
    }

    pub fn execute_responsibility_query(
        &self,
        investigator: &AuthenticatedActorContext,
        filter: &AuditInvestigationFilter,
    ) -> Result<Vec<ResponsibilityAttribution>, AuditTrailError> {
        let telemetry = TelemetryService::ComplianceWorker.begin_operation(
            "queryAuditResponsibilities",
            Some(investigator.actor_id().as_str()),
            None::<&str>,
        );
        let result = self
            .audit_trail
            .investigation_responsibility_query(investigator, filter);
        telemetry.finish(if result.is_ok() {
            TelemetryOutcome::Success
        } else {
            TelemetryOutcome::Error
        });
        result
    }

    pub fn execute_retention_purge(
        &self,
        actor: &AuthenticatedActorContext,
        as_of: AuditTimestamp,
    ) -> Result<AuditPurgeReport, AuditTrailError> {
        let telemetry = TelemetryService::ComplianceWorker.begin_operation(
            "purgeAuditEvidence",
            Some(actor.actor_id().as_str()),
            None::<&str>,
        );
        let result = self.audit_trail.purge_expired_evidence(actor, as_of);
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
        actor: &AuthenticatedActorContext,
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
                .create_order(
                    actor,
                    order_id,
                    vendor_id,
                    plant_id,
                    delivery_epoch_day,
                    line_items,
                    at,
                )
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
        actor: &AuthenticatedActorContext,
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
                .update_order(actor, order_id, mutation, at)
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

pub struct HttpEmployeeDiscoveryExecutionGateway<'a> {
    compliance_lifecycle: &'a VendorComplianceLifecycle,
    delivery_policy: &'a VendorPlantDeliveryPolicy,
    menu_supply_policy: &'a MenuSupplyPolicy,
}

impl<'a> HttpEmployeeDiscoveryExecutionGateway<'a> {
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

    pub fn execute_discovery_snapshot(
        &self,
        plant_id: &PlantId,
        at: TaipeiBusinessMoment,
        for_search: bool,
    ) -> Result<Vec<EmployeeMenuDiscoveryEntry>, HttpEmployeeDiscoveryError> {
        let telemetry = TelemetryService::HttpApi.begin_internal_operation(
            if for_search {
                "listEmployeeMenus:search"
            } else {
                "listEmployeeMenus:browse"
            },
            None,
            Some(plant_id.as_str()),
        );
        let deliverable_vendor_ids = if for_search {
            self.delivery_policy.employee_visible_vendor_ids_for_search(
                self.compliance_lifecycle,
                plant_id,
                at,
            )
        } else {
            self.delivery_policy.employee_visible_vendor_ids_for_browse(
                self.compliance_lifecycle,
                plant_id,
                at,
            )
        };
        let deliverable_vendor_ids = deliverable_vendor_ids.into_iter().collect::<BTreeSet<_>>();
        let result = self
            .menu_supply_policy
            .employee_discovery_snapshot(&deliverable_vendor_ids, at)
            .map_err(HttpEmployeeDiscoveryError::MenuSupply);
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

pub struct HttpVendorFulfillmentExecutionGateway<'a> {
    fulfillment_policy: &'a VendorFulfillmentPolicy,
    menu_supply_policy: &'a MenuSupplyPolicy,
}

impl<'a> HttpVendorFulfillmentExecutionGateway<'a> {
    pub fn new(
        fulfillment_policy: &'a VendorFulfillmentPolicy,
        menu_supply_policy: &'a MenuSupplyPolicy,
    ) -> Self {
        Self {
            fulfillment_policy,
            menu_supply_policy,
        }
    }

    pub fn execute_vendor_operations_board(
        &self,
        vendor_id: &VendorId,
        delivery_epoch_day: i32,
        at: TaipeiBusinessMoment,
    ) -> Result<VendorFulfillmentBoardSnapshot, VendorFulfillmentError> {
        let telemetry = TelemetryService::HttpApi.begin_internal_operation(
            "listVendorFulfillmentBoard",
            None,
            None,
        );
        let result = self.fulfillment_policy.vendor_operations_board(
            self.menu_supply_policy,
            vendor_id,
            delivery_epoch_day,
            at,
        );
        telemetry.finish(if result.is_ok() {
            TelemetryOutcome::Success
        } else {
            TelemetryOutcome::Error
        });
        result
    }

    pub fn execute_transition_delivery_status(
        &self,
        actor: &AuthenticatedActorContext,
        order_id: &OrderId,
        to_status: FulfillmentDeliveryStatus,
        at: TaipeiBusinessMoment,
    ) -> Result<(), VendorFulfillmentError> {
        let telemetry = TelemetryService::HttpApi.begin_internal_operation(
            "advanceVendorFulfillmentDeliveryStatus",
            Some(actor.actor_id().as_str()),
            None,
        );
        let result = self
            .fulfillment_policy
            .transition_delivery_status(actor, self.menu_supply_policy, order_id, to_status, at)
            .map(|_| ());
        telemetry.finish(if result.is_ok() {
            TelemetryOutcome::Success
        } else {
            TelemetryOutcome::Error
        });
        result
    }

    pub fn execute_create_export_batch(
        &self,
        actor: &AuthenticatedActorContext,
        vendor_id: &VendorId,
        delivery_epoch_day: i32,
        captured_at: TaipeiBusinessMoment,
    ) -> Result<VendorFulfillmentBatchSnapshot, VendorFulfillmentError> {
        let telemetry = TelemetryService::HttpApi.begin_internal_operation(
            "createVendorFulfillmentExportBatch",
            Some(actor.actor_id().as_str()),
            None,
        );
        let result = self.fulfillment_policy.create_export_batch(
            actor,
            self.menu_supply_policy,
            vendor_id,
            delivery_epoch_day,
            captured_at,
        );
        telemetry.finish(if result.is_ok() {
            TelemetryOutcome::Success
        } else {
            TelemetryOutcome::Error
        });
        result
    }

    pub fn execute_get_export_batch(
        &self,
        batch_id: &FulfillmentBatchId,
    ) -> Result<VendorFulfillmentBatchSnapshot, VendorFulfillmentError> {
        let telemetry = TelemetryService::HttpApi.begin_internal_operation(
            "getVendorFulfillmentExportBatch",
            None,
            None,
        );
        let result = self.fulfillment_policy.batch_snapshot(batch_id);
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

#[derive(Debug)]
pub enum HttpEmployeeDiscoveryError {
    MenuSupply(MenuSupplyWindowError),
}

impl std::fmt::Display for HttpEmployeeDiscoveryError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::MenuSupply(error) => write!(f, "menu supply discovery failed: {error}"),
        }
    }
}

impl std::error::Error for HttpEmployeeDiscoveryError {}
