use std::fs;
use std::path::{Path, PathBuf};

use serde_json::{json, Value};

use crate::access::Action;

#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum HttpAudience {
    Employee,
    Vendor,
    Admin,
    Integration,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum HttpMethod {
    Get,
    Post,
    Patch,
    Put,
    Delete,
}

impl HttpMethod {
    pub const fn as_openapi_verb(self) -> &'static str {
        match self {
            Self::Get => "get",
            Self::Post => "post",
            Self::Patch => "patch",
            Self::Put => "put",
            Self::Delete => "delete",
        }
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum HttpOperation {
    ListEmployeeMenus,
    CreateEmployeeOrder,
    UpdateEmployeeOrder,
    VerifyPickupOrder,
    GetEmployeeOrderPayrollLedger,
    CreateEmployeeOrderDispute,
    ListVendorOrders,
    ListVendorFulfillmentBoard,
    UpsertVendorMenuItem,
    AdvanceVendorFulfillmentDeliveryStatus,
    CreateVendorFulfillmentExportBatch,
    GetVendorFulfillmentExportBatch,
    ListAdminVendors,
    ListVendorPlantDeliveryMappings,
    ListComplianceDocumentTemplates,
    UpsertComplianceDocumentTemplate,
    UpsertVendorPlantDeliveryMapping,
    DeleteVendorPlantDeliveryMapping,
    ReviewVendorApplication,
    RunVendorComplianceLifecycle,
    QueryAuditInvestigations,
    QueryAuditResponsibilities,
    PurgeAuditEvidence,
    PurgeOrderData,
    ListAnomalyRules,
    UpsertAnomalyRule,
    EvaluateAnomalyAlerts,
    ListAnomalyAlerts,
    UpdateAdminAnomalyAlert,
    UpdateAdminPayrollDispute,
    PurgePayrollData,
    CloseMonthlyPayrollSettlement,
    LockPayrollSettlementCycle,
    UnlockPayrollSettlementCycle,
    ExportPayrollDeductions,
    SyncPayrollHrApiAdjunct,
}

impl HttpOperation {
    pub const ALL: [Self; 36] = [
        Self::ListEmployeeMenus,
        Self::CreateEmployeeOrder,
        Self::UpdateEmployeeOrder,
        Self::VerifyPickupOrder,
        Self::GetEmployeeOrderPayrollLedger,
        Self::CreateEmployeeOrderDispute,
        Self::ListVendorOrders,
        Self::ListVendorFulfillmentBoard,
        Self::UpsertVendorMenuItem,
        Self::AdvanceVendorFulfillmentDeliveryStatus,
        Self::CreateVendorFulfillmentExportBatch,
        Self::GetVendorFulfillmentExportBatch,
        Self::ListAdminVendors,
        Self::ListVendorPlantDeliveryMappings,
        Self::ListComplianceDocumentTemplates,
        Self::UpsertComplianceDocumentTemplate,
        Self::UpsertVendorPlantDeliveryMapping,
        Self::DeleteVendorPlantDeliveryMapping,
        Self::ReviewVendorApplication,
        Self::RunVendorComplianceLifecycle,
        Self::QueryAuditInvestigations,
        Self::QueryAuditResponsibilities,
        Self::PurgeAuditEvidence,
        Self::PurgeOrderData,
        Self::ListAnomalyRules,
        Self::UpsertAnomalyRule,
        Self::EvaluateAnomalyAlerts,
        Self::ListAnomalyAlerts,
        Self::UpdateAdminAnomalyAlert,
        Self::UpdateAdminPayrollDispute,
        Self::PurgePayrollData,
        Self::CloseMonthlyPayrollSettlement,
        Self::LockPayrollSettlementCycle,
        Self::UnlockPayrollSettlementCycle,
        Self::ExportPayrollDeductions,
        Self::SyncPayrollHrApiAdjunct,
    ];

    pub const fn operation_id(self) -> &'static str {
        match self {
            Self::ListEmployeeMenus => "listEmployeeMenus",
            Self::CreateEmployeeOrder => "createEmployeeOrder",
            Self::UpdateEmployeeOrder => "updateEmployeeOrder",
            Self::VerifyPickupOrder => "verifyPickupOrder",
            Self::GetEmployeeOrderPayrollLedger => "getEmployeeOrderPayrollLedger",
            Self::CreateEmployeeOrderDispute => "createEmployeeOrderDispute",
            Self::ListVendorOrders => "listVendorOrders",
            Self::ListVendorFulfillmentBoard => "listVendorFulfillmentBoard",
            Self::UpsertVendorMenuItem => "upsertVendorMenuItem",
            Self::AdvanceVendorFulfillmentDeliveryStatus => {
                "advanceVendorFulfillmentDeliveryStatus"
            }
            Self::CreateVendorFulfillmentExportBatch => "createVendorFulfillmentExportBatch",
            Self::GetVendorFulfillmentExportBatch => "getVendorFulfillmentExportBatch",
            Self::ListAdminVendors => "listAdminVendors",
            Self::ListVendorPlantDeliveryMappings => "listVendorPlantDeliveryMappings",
            Self::ListComplianceDocumentTemplates => "listComplianceDocumentTemplates",
            Self::UpsertComplianceDocumentTemplate => "upsertComplianceDocumentTemplate",
            Self::UpsertVendorPlantDeliveryMapping => "upsertVendorPlantDeliveryMapping",
            Self::DeleteVendorPlantDeliveryMapping => "deleteVendorPlantDeliveryMapping",
            Self::ReviewVendorApplication => "reviewVendorApplication",
            Self::RunVendorComplianceLifecycle => "runVendorComplianceLifecycle",
            Self::QueryAuditInvestigations => "queryAuditInvestigations",
            Self::QueryAuditResponsibilities => "queryAuditResponsibilities",
            Self::PurgeAuditEvidence => "purgeAuditEvidence",
            Self::PurgeOrderData => "purgeOrderData",
            Self::ListAnomalyRules => "listAnomalyRules",
            Self::UpsertAnomalyRule => "upsertAnomalyRule",
            Self::EvaluateAnomalyAlerts => "evaluateAnomalyAlerts",
            Self::ListAnomalyAlerts => "listAnomalyAlerts",
            Self::UpdateAdminAnomalyAlert => "updateAdminAnomalyAlert",
            Self::UpdateAdminPayrollDispute => "updateAdminPayrollDispute",
            Self::PurgePayrollData => "purgePayrollData",
            Self::CloseMonthlyPayrollSettlement => "closePayrollMonthlySettlement",
            Self::LockPayrollSettlementCycle => "lockPayrollSettlementCycle",
            Self::UnlockPayrollSettlementCycle => "unlockPayrollSettlementCycle",
            Self::ExportPayrollDeductions => "exportPayrollDeductions",
            Self::SyncPayrollHrApiAdjunct => "syncPayrollHrApiAdjunct",
        }
    }

    pub const fn method(self) -> HttpMethod {
        match self {
            Self::ListEmployeeMenus
            | Self::GetEmployeeOrderPayrollLedger
            | Self::ListVendorOrders
            | Self::ListVendorFulfillmentBoard
            | Self::GetVendorFulfillmentExportBatch
            | Self::ListAdminVendors
            | Self::ListVendorPlantDeliveryMappings
            | Self::ListComplianceDocumentTemplates
            | Self::QueryAuditInvestigations
            | Self::QueryAuditResponsibilities
            | Self::ExportPayrollDeductions => HttpMethod::Get,
            Self::CreateEmployeeOrder
            | Self::CreateEmployeeOrderDispute
            | Self::VerifyPickupOrder
            | Self::AdvanceVendorFulfillmentDeliveryStatus
            | Self::CreateVendorFulfillmentExportBatch
            | Self::ReviewVendorApplication
            | Self::RunVendorComplianceLifecycle
            | Self::PurgeAuditEvidence
            | Self::PurgeOrderData
            | Self::EvaluateAnomalyAlerts
            | Self::PurgePayrollData
            | Self::CloseMonthlyPayrollSettlement
            | Self::LockPayrollSettlementCycle
            | Self::UnlockPayrollSettlementCycle
            | Self::SyncPayrollHrApiAdjunct => HttpMethod::Post,
            Self::UpdateEmployeeOrder
            | Self::UpdateAdminPayrollDispute
            | Self::UpdateAdminAnomalyAlert => HttpMethod::Patch,
            Self::UpsertVendorMenuItem
            | Self::UpsertComplianceDocumentTemplate
            | Self::UpsertVendorPlantDeliveryMapping
            | Self::UpsertAnomalyRule => HttpMethod::Put,
            Self::ListAnomalyRules | Self::ListAnomalyAlerts => HttpMethod::Get,
            Self::DeleteVendorPlantDeliveryMapping => HttpMethod::Delete,
        }
    }

    pub const fn path(self) -> &'static str {
        match self {
            Self::ListEmployeeMenus => "/api/v1/employee/menus",
            Self::CreateEmployeeOrder => "/api/v1/employee/orders",
            Self::UpdateEmployeeOrder => "/api/v1/employee/orders/{orderId}",
            Self::VerifyPickupOrder => "/api/v1/employee/orders/{orderId}/pickup-verifications",
            Self::GetEmployeeOrderPayrollLedger => {
                "/api/v1/employee/orders/{orderId}/payroll-ledger"
            }
            Self::CreateEmployeeOrderDispute => "/api/v1/employee/orders/{orderId}/disputes",
            Self::ListVendorOrders => "/api/v1/vendor/orders",
            Self::ListVendorFulfillmentBoard => "/api/v1/vendor/fulfillment-board",
            Self::UpsertVendorMenuItem => "/api/v1/vendor/menu-items/{menuItemId}",
            Self::AdvanceVendorFulfillmentDeliveryStatus => {
                "/api/v1/vendor/orders/{orderId}/delivery-status"
            }
            Self::CreateVendorFulfillmentExportBatch => "/api/v1/vendor/fulfillment-batches",
            Self::GetVendorFulfillmentExportBatch => "/api/v1/vendor/fulfillment-batches/{batchId}",
            Self::ListAdminVendors => "/api/v1/admin/vendors",
            Self::ListVendorPlantDeliveryMappings => "/api/v1/admin/vendor-plant-delivery-mappings",
            Self::ListComplianceDocumentTemplates => "/api/v1/admin/compliance/document-templates",
            Self::UpsertComplianceDocumentTemplate => {
                "/api/v1/admin/compliance/document-templates/{vendorCategory}/{templateId}"
            }
            Self::UpsertVendorPlantDeliveryMapping => {
                "/api/v1/admin/vendors/{vendorId}/plant-delivery-mappings/{mappingId}"
            }
            Self::DeleteVendorPlantDeliveryMapping => {
                "/api/v1/admin/vendors/{vendorId}/plant-delivery-mappings/{mappingId}"
            }
            Self::ReviewVendorApplication => "/api/v1/admin/vendors/{vendorId}/reviews",
            Self::RunVendorComplianceLifecycle => "/api/v1/admin/compliance/lifecycle/executions",
            Self::QueryAuditInvestigations => "/api/v1/admin/audit/investigations",
            Self::QueryAuditResponsibilities => "/api/v1/admin/audit/responsibilities",
            Self::PurgeAuditEvidence => "/api/v1/admin/audit/retention-purge",
            Self::PurgeOrderData => "/api/v1/admin/orders/retention-purge",
            Self::ListAnomalyRules => "/api/v1/admin/anomaly/rules",
            Self::UpsertAnomalyRule => "/api/v1/admin/anomaly/rules/{ruleId}",
            Self::EvaluateAnomalyAlerts => "/api/v1/admin/anomaly/alerts/evaluations",
            Self::ListAnomalyAlerts => "/api/v1/admin/anomaly/alerts",
            Self::UpdateAdminAnomalyAlert => "/api/v1/admin/anomaly/alerts/{alertId}",
            Self::UpdateAdminPayrollDispute => "/api/v1/admin/payroll/disputes/{disputeId}",
            Self::PurgePayrollData => "/api/v1/admin/payroll/retention-purge",
            Self::CloseMonthlyPayrollSettlement => {
                "/api/v1/admin/payroll/monthly-settlements/close"
            }
            Self::LockPayrollSettlementCycle => {
                "/api/v1/admin/payroll/monthly-settlements/{cycleKey}/lock"
            }
            Self::UnlockPayrollSettlementCycle => {
                "/api/v1/admin/payroll/monthly-settlements/{cycleKey}/unlock"
            }
            Self::ExportPayrollDeductions => "/api/v1/integrations/payroll/deductions",
            Self::SyncPayrollHrApiAdjunct => {
                "/api/v1/integrations/payroll/sftp-batches/{batchId}/hr-api-sync"
            }
        }
    }

    pub const fn audience(self) -> HttpAudience {
        match self {
            Self::ListEmployeeMenus
            | Self::CreateEmployeeOrder
            | Self::UpdateEmployeeOrder
            | Self::VerifyPickupOrder
            | Self::GetEmployeeOrderPayrollLedger
            | Self::CreateEmployeeOrderDispute => HttpAudience::Employee,
            Self::ListVendorOrders
            | Self::ListVendorFulfillmentBoard
            | Self::UpsertVendorMenuItem
            | Self::AdvanceVendorFulfillmentDeliveryStatus
            | Self::CreateVendorFulfillmentExportBatch
            | Self::GetVendorFulfillmentExportBatch => HttpAudience::Vendor,
            Self::ListAdminVendors
            | Self::ListVendorPlantDeliveryMappings
            | Self::ListComplianceDocumentTemplates
            | Self::UpsertComplianceDocumentTemplate
            | Self::UpsertVendorPlantDeliveryMapping
            | Self::DeleteVendorPlantDeliveryMapping
            | Self::ReviewVendorApplication
            | Self::RunVendorComplianceLifecycle
            | Self::QueryAuditInvestigations
            | Self::QueryAuditResponsibilities
            | Self::PurgeAuditEvidence
            | Self::PurgeOrderData
            | Self::ListAnomalyRules
            | Self::UpsertAnomalyRule
            | Self::EvaluateAnomalyAlerts
            | Self::ListAnomalyAlerts
            | Self::UpdateAdminAnomalyAlert
            | Self::UpdateAdminPayrollDispute
            | Self::PurgePayrollData
            | Self::CloseMonthlyPayrollSettlement
            | Self::LockPayrollSettlementCycle
            | Self::UnlockPayrollSettlementCycle => HttpAudience::Admin,
            Self::ExportPayrollDeductions | Self::SyncPayrollHrApiAdjunct => {
                HttpAudience::Integration
            }
        }
    }

    pub const fn write_action(self) -> Option<Action> {
        match self {
            Self::CreateEmployeeOrder
            | Self::UpdateEmployeeOrder
            | Self::VerifyPickupOrder
            | Self::CreateEmployeeOrderDispute => Some(Action::PlaceEmployeeOrder),
            Self::UpsertVendorMenuItem
            | Self::AdvanceVendorFulfillmentDeliveryStatus
            | Self::CreateVendorFulfillmentExportBatch => Some(Action::ManageVendorMenu),
            Self::UpsertComplianceDocumentTemplate
            | Self::UpsertVendorPlantDeliveryMapping
            | Self::DeleteVendorPlantDeliveryMapping
            | Self::ReviewVendorApplication
            | Self::RunVendorComplianceLifecycle
            | Self::PurgeAuditEvidence
            | Self::PurgeOrderData
            | Self::UpsertAnomalyRule
            | Self::EvaluateAnomalyAlerts
            | Self::UpdateAdminAnomalyAlert
            | Self::PurgePayrollData
            | Self::LockPayrollSettlementCycle
            | Self::UnlockPayrollSettlementCycle => Some(Action::ManageVendorComplianceLifecycle),
            Self::UpdateAdminPayrollDispute
            | Self::CloseMonthlyPayrollSettlement
            | Self::SyncPayrollHrApiAdjunct => Some(Action::ExportPayrollDeductions),
            Self::ListEmployeeMenus
            | Self::GetEmployeeOrderPayrollLedger
            | Self::ListVendorOrders
            | Self::ListVendorFulfillmentBoard
            | Self::GetVendorFulfillmentExportBatch
            | Self::ListAdminVendors
            | Self::ListVendorPlantDeliveryMappings
            | Self::ListComplianceDocumentTemplates
            | Self::QueryAuditInvestigations
            | Self::QueryAuditResponsibilities
            | Self::ListAnomalyRules
            | Self::ListAnomalyAlerts
            | Self::ExportPayrollDeductions => None,
        }
    }

    pub const fn is_write_operation(self) -> bool {
        self.write_action().is_some()
    }

    pub fn from_operation_id(value: &str) -> Option<Self> {
        match value {
            "listEmployeeMenus" => Some(Self::ListEmployeeMenus),
            "createEmployeeOrder" => Some(Self::CreateEmployeeOrder),
            "updateEmployeeOrder" => Some(Self::UpdateEmployeeOrder),
            "verifyPickupOrder" => Some(Self::VerifyPickupOrder),
            "getEmployeeOrderPayrollLedger" => Some(Self::GetEmployeeOrderPayrollLedger),
            "createEmployeeOrderDispute" => Some(Self::CreateEmployeeOrderDispute),
            "listVendorOrders" => Some(Self::ListVendorOrders),
            "listVendorFulfillmentBoard" => Some(Self::ListVendorFulfillmentBoard),
            "upsertVendorMenuItem" => Some(Self::UpsertVendorMenuItem),
            "advanceVendorFulfillmentDeliveryStatus" => {
                Some(Self::AdvanceVendorFulfillmentDeliveryStatus)
            }
            "createVendorFulfillmentExportBatch" => Some(Self::CreateVendorFulfillmentExportBatch),
            "getVendorFulfillmentExportBatch" => Some(Self::GetVendorFulfillmentExportBatch),
            "listAdminVendors" => Some(Self::ListAdminVendors),
            "listVendorPlantDeliveryMappings" => Some(Self::ListVendorPlantDeliveryMappings),
            "listComplianceDocumentTemplates" => Some(Self::ListComplianceDocumentTemplates),
            "upsertComplianceDocumentTemplate" => Some(Self::UpsertComplianceDocumentTemplate),
            "upsertVendorPlantDeliveryMapping" => Some(Self::UpsertVendorPlantDeliveryMapping),
            "deleteVendorPlantDeliveryMapping" => Some(Self::DeleteVendorPlantDeliveryMapping),
            "reviewVendorApplication" => Some(Self::ReviewVendorApplication),
            "runVendorComplianceLifecycle" => Some(Self::RunVendorComplianceLifecycle),
            "queryAuditInvestigations" => Some(Self::QueryAuditInvestigations),
            "queryAuditResponsibilities" => Some(Self::QueryAuditResponsibilities),
            "purgeAuditEvidence" => Some(Self::PurgeAuditEvidence),
            "purgeOrderData" => Some(Self::PurgeOrderData),
            "listAnomalyRules" => Some(Self::ListAnomalyRules),
            "upsertAnomalyRule" => Some(Self::UpsertAnomalyRule),
            "evaluateAnomalyAlerts" => Some(Self::EvaluateAnomalyAlerts),
            "listAnomalyAlerts" => Some(Self::ListAnomalyAlerts),
            "updateAdminAnomalyAlert" => Some(Self::UpdateAdminAnomalyAlert),
            "updateAdminPayrollDispute" => Some(Self::UpdateAdminPayrollDispute),
            "purgePayrollData" => Some(Self::PurgePayrollData),
            "closePayrollMonthlySettlement" => Some(Self::CloseMonthlyPayrollSettlement),
            "lockPayrollSettlementCycle" => Some(Self::LockPayrollSettlementCycle),
            "unlockPayrollSettlementCycle" => Some(Self::UnlockPayrollSettlementCycle),
            "exportPayrollDeductions" => Some(Self::ExportPayrollDeductions),
            "syncPayrollHrApiAdjunct" => Some(Self::SyncPayrollHrApiAdjunct),
            _ => None,
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct OpenApiArtifactPaths {
    pub openapi_json: PathBuf,
    pub openapi_yaml: PathBuf,
    pub docs_html: PathBuf,
}

#[derive(Debug)]
pub enum OpenApiContractError {
    Io(std::io::Error),
    Json(serde_json::Error),
    Yaml(serde_yaml::Error),
}

impl From<std::io::Error> for OpenApiContractError {
    fn from(value: std::io::Error) -> Self {
        Self::Io(value)
    }
}

impl From<serde_json::Error> for OpenApiContractError {
    fn from(value: serde_json::Error) -> Self {
        Self::Json(value)
    }
}

impl From<serde_yaml::Error> for OpenApiContractError {
    fn from(value: serde_yaml::Error) -> Self {
        Self::Yaml(value)
    }
}

impl std::fmt::Display for OpenApiContractError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::Io(error) => write!(f, "io error: {error}"),
            Self::Json(error) => write!(f, "json serialization error: {error}"),
            Self::Yaml(error) => write!(f, "yaml serialization error: {error}"),
        }
    }
}

impl std::error::Error for OpenApiContractError {}

pub fn canonical_openapi_spec() -> Value {
    json!({
      "openapi": "3.1.0",
      "jsonSchemaDialect": "https://spec.openapis.org/oas/3.1/dialect/base",
      "info": {
        "title": "Corporate Catering System API",
        "summary": "Canonical machine-verifiable HTTP contract",
        "version": "1.0.0",
        "description": "Contract-first API definition for employee, vendor, admin, and integration APIs."
      },
      "servers": [
        {
          "url": "https://api.corporate-catering.example.com",
          "description": "Production"
        }
      ],
      "tags": [
        { "name": "Employee", "description": "Employee ordering and menu access endpoints." },
        { "name": "Vendor", "description": "Vendor menu and fulfillment endpoints." },
        { "name": "Admin", "description": "Committee administrator governance endpoints." },
        { "name": "Integration", "description": "Internal enterprise integration endpoints." }
      ],
      "paths": {
        "/api/v1/employee/menus": {
          "get": {
            "tags": ["Employee"],
            "summary": "List discoverable menus for multi-day preorder",
            "operationId": HttpOperation::ListEmployeeMenus.operation_id(),
            "x-deliverability-enforcement": {
              "enabled": true,
              "apiContexts": ["BROWSE", "SEARCH"],
              "ruleSource": "VENDOR_PLANT_DELIVERY_MAPPING",
              "timezone": "Asia/Taipei"
            },
            "x-discovery-governance": {
              "timezone": "Asia/Taipei",
              "deterministicFiltering": true,
              "recommendationDefaultEnabled": false,
              "recommendationAppliedInMvp": false,
              "remainingQuantitySource": "MENU_SUPPLY_POLICY_ALLOCATED_COUNTER",
              "supportedViews": ["week", "calendar"]
            },
            "security": [{ "corporateSsoBearer": [] }],
            "parameters": [
              { "$ref": "#/components/parameters/PlantIdQuery" },
              { "$ref": "#/components/parameters/DiscoveryViewQuery" },
              { "$ref": "#/components/parameters/MenuDateQuery" },
              { "$ref": "#/components/parameters/FromDateQuery" },
              { "$ref": "#/components/parameters/ToDateQuery" },
              { "$ref": "#/components/parameters/PageQuery" },
              { "$ref": "#/components/parameters/PageSizeQuery" },
              { "$ref": "#/components/parameters/MenuSortByQuery" },
              { "$ref": "#/components/parameters/SortOrderQuery" },
              { "$ref": "#/components/parameters/MenuSearchQuery" },
              { "$ref": "#/components/parameters/MenuTypeFilterQuery" },
              { "$ref": "#/components/parameters/HealthTagFilterQuery" },
              { "$ref": "#/components/parameters/PriceMinMinorQuery" },
              { "$ref": "#/components/parameters/PriceMaxMinorQuery" },
              { "$ref": "#/components/parameters/RemainingQuantityFilterQuery" },
              { "$ref": "#/components/parameters/RecommendationEnabledQuery" }
            ],
            "responses": {
              "200": {
                "description": "Deterministic multi-day menu discovery page",
                "content": {
                  "application/json": {
                    "schema": { "$ref": "#/components/schemas/MenuPage" }
                  }
                }
              },
              "400": { "$ref": "#/components/responses/BadRequest" },
              "401": { "$ref": "#/components/responses/Unauthorized" },
              "403": { "$ref": "#/components/responses/Forbidden" },
              "500": { "$ref": "#/components/responses/InternalServerError" }
            }
          }
        },
        "/api/v1/employee/orders": {
          "post": {
            "tags": ["Employee"],
            "summary": "Create a meal order",
            "operationId": HttpOperation::CreateEmployeeOrder.operation_id(),
            "x-deliverability-enforcement": {
              "enabled": true,
              "apiContexts": ["ORDER"],
              "ruleSource": "VENDOR_PLANT_DELIVERY_MAPPING",
              "timezone": "Asia/Taipei"
            },
            "x-order-governance": {
              "timezone": "Asia/Taipei",
              "strictLifecycle": true,
              "inventoryReservationMode": "ATOMIC_IDEMPOTENT",
              "preorderWindow": {
                "defaultOpenDaysAhead": 7,
                "vendorOverrideBounds": {
                  "minimum": 1,
                  "maximum": 7
                }
              },
              "modifyCancelCutoff": {
                "defaultRule": {
                  "relativeDayFromDelivery": -1,
                  "minuteOfDay": 1020
                },
                "vendorOverrideBounds": {
                  "minimumMinuteOfDay": 900,
                  "maximumMinuteOfDay": 1200
                }
              },
              "specialRequestPolicy": {
                "mode": "CONTROLLED_ENUM_ONLY",
                "allowFreeText": false
              },
              "timeline": {
                "required": true,
                "includes": [
                  "CREATED",
                  "MODIFIED",
                  "CANCELLED",
                  "SOLD_OUT",
                  "REFUND_PENDING",
                  "REFUNDED",
                  "FULFILLED"
                ]
              }
            },
            "security": [{ "corporateSsoBearer": [] }],
            "requestBody": {
              "required": true,
              "content": {
                "application/json": {
                  "schema": { "$ref": "#/components/schemas/EmployeeOrderCreateRequest" }
                }
              }
            },
            "responses": {
              "201": {
                "description": "Order created",
                "content": {
                  "application/json": {
                    "schema": { "$ref": "#/components/schemas/EmployeeOrder" }
                  }
                }
              },
              "400": { "$ref": "#/components/responses/BadRequest" },
              "401": { "$ref": "#/components/responses/Unauthorized" },
              "403": { "$ref": "#/components/responses/Forbidden" },
              "409": { "$ref": "#/components/responses/Conflict" },
              "422": { "$ref": "#/components/responses/ValidationFailed" },
              "500": { "$ref": "#/components/responses/InternalServerError" }
            }
          }
        },
        "/api/v1/employee/orders/{orderId}": {
          "patch": {
            "tags": ["Employee"],
            "summary": "Modify an existing order before cutoff",
            "operationId": HttpOperation::UpdateEmployeeOrder.operation_id(),
            "x-order-governance": {
              "timezone": "Asia/Taipei",
              "strictLifecycle": true,
              "inventoryReservationMode": "ATOMIC_IDEMPOTENT",
              "supportedOperations": ["REPLACE_LINE_ITEMS", "CANCEL"],
              "modifyCancelCutoff": {
                "defaultRule": {
                  "relativeDayFromDelivery": -1,
                  "minuteOfDay": 1020
                },
                "vendorOverrideBounds": {
                  "minimumMinuteOfDay": 900,
                  "maximumMinuteOfDay": 1200
                }
              }
            },
            "security": [{ "corporateSsoBearer": [] }],
            "parameters": [
              { "$ref": "#/components/parameters/OrderIdPath" }
            ],
            "requestBody": {
              "required": true,
              "content": {
                "application/json": {
                  "schema": { "$ref": "#/components/schemas/EmployeeOrderPatchRequest" }
                }
              }
            },
            "responses": {
              "200": {
                "description": "Order updated",
                "content": {
                  "application/json": {
                    "schema": { "$ref": "#/components/schemas/EmployeeOrder" }
                  }
                }
              },
              "400": { "$ref": "#/components/responses/BadRequest" },
              "401": { "$ref": "#/components/responses/Unauthorized" },
              "403": { "$ref": "#/components/responses/Forbidden" },
              "404": { "$ref": "#/components/responses/NotFound" },
              "409": { "$ref": "#/components/responses/Conflict" },
              "422": { "$ref": "#/components/responses/ValidationFailed" },
              "500": { "$ref": "#/components/responses/InternalServerError" }
            }
          }
        },
        "/api/v1/employee/orders/{orderId}/pickup-verifications": {
          "post": {
            "tags": ["Employee"],
            "summary": "Verify order pickup handoff",
            "operationId": HttpOperation::VerifyPickupOrder.operation_id(),
            "x-order-governance": {
              "timezone": "Asia/Taipei",
              "strictLifecycle": true,
              "inventoryReservationMode": "ATOMIC_IDEMPOTENT",
              "pickupVerification": {
                "required": true,
                "mechanism": "TOTP_QR_SINGLE_USE",
                "stepSeconds": 30,
                "maxClockSkewSteps": 1
              }
            },
            "security": [{ "corporateSsoBearer": [] }],
            "parameters": [
              { "$ref": "#/components/parameters/OrderIdPath" }
            ],
            "requestBody": {
              "required": true,
              "content": {
                "application/json": {
                  "schema": { "$ref": "#/components/schemas/PickupVerificationRequest" }
                }
              }
            },
            "responses": {
              "200": {
                "description": "Pickup verification accepted",
                "content": {
                  "application/json": {
                    "schema": { "$ref": "#/components/schemas/PickupVerificationResponse" }
                  }
                }
              },
              "400": { "$ref": "#/components/responses/BadRequest" },
              "401": { "$ref": "#/components/responses/Unauthorized" },
              "403": { "$ref": "#/components/responses/Forbidden" },
              "404": { "$ref": "#/components/responses/NotFound" },
              "409": { "$ref": "#/components/responses/Conflict" },
              "500": { "$ref": "#/components/responses/InternalServerError" }
            }
          }
        },
        "/api/v1/employee/orders/{orderId}/payroll-ledger": {
          "get": {
            "tags": ["Employee"],
            "summary": "Get immutable payroll ledger and dispute state for an order",
            "operationId": HttpOperation::GetEmployeeOrderPayrollLedger.operation_id(),
            "security": [{ "corporateSsoBearer": [] }],
            "parameters": [
              { "$ref": "#/components/parameters/OrderIdPath" }
            ],
            "responses": {
              "200": {
                "description": "Per-order payroll ledger, adjustments, refunds, and disputes",
                "content": {
                  "application/json": {
                    "schema": { "$ref": "#/components/schemas/EmployeeOrderPayrollLedger" }
                  }
                }
              },
              "400": { "$ref": "#/components/responses/BadRequest" },
              "401": { "$ref": "#/components/responses/Unauthorized" },
              "403": { "$ref": "#/components/responses/Forbidden" },
              "404": { "$ref": "#/components/responses/NotFound" },
              "500": { "$ref": "#/components/responses/InternalServerError" }
            }
          }
        },
        "/api/v1/employee/orders/{orderId}/disputes": {
          "post": {
            "tags": ["Employee"],
            "summary": "Open a payroll dispute for an order deduction",
            "operationId": HttpOperation::CreateEmployeeOrderDispute.operation_id(),
            "security": [{ "corporateSsoBearer": [] }],
            "parameters": [
              { "$ref": "#/components/parameters/OrderIdPath" }
            ],
            "requestBody": {
              "required": true,
              "content": {
                "application/json": {
                  "schema": { "$ref": "#/components/schemas/EmployeePayrollDisputeCreateRequest" }
                }
              }
            },
            "responses": {
              "201": {
                "description": "Payroll dispute opened with immutable trace seed",
                "content": {
                  "application/json": {
                    "schema": { "$ref": "#/components/schemas/PayrollDispute" }
                  }
                }
              },
              "400": { "$ref": "#/components/responses/BadRequest" },
              "401": { "$ref": "#/components/responses/Unauthorized" },
              "403": { "$ref": "#/components/responses/Forbidden" },
              "404": { "$ref": "#/components/responses/NotFound" },
              "409": { "$ref": "#/components/responses/Conflict" },
              "500": { "$ref": "#/components/responses/InternalServerError" }
            }
          }
        },
        "/api/v1/vendor/orders": {
          "get": {
            "tags": ["Vendor"],
            "summary": "List vendor order board entries",
            "operationId": HttpOperation::ListVendorOrders.operation_id(),
            "security": [{ "vendorMfaBearer": [] }],
            "parameters": [
              { "$ref": "#/components/parameters/PlantIdQuery" },
              { "$ref": "#/components/parameters/FromDateQuery" },
              { "$ref": "#/components/parameters/ToDateQuery" },
              { "$ref": "#/components/parameters/PageQuery" },
              { "$ref": "#/components/parameters/PageSizeQuery" },
              { "$ref": "#/components/parameters/VendorOrderSortByQuery" },
              { "$ref": "#/components/parameters/SortOrderQuery" },
              { "$ref": "#/components/parameters/OrderStatusFilterQuery" }
            ],
            "responses": {
              "200": {
                "description": "Paginated vendor order board",
                "content": {
                  "application/json": {
                    "schema": { "$ref": "#/components/schemas/VendorOrderPage" }
                  }
                }
              },
              "400": { "$ref": "#/components/responses/BadRequest" },
              "401": { "$ref": "#/components/responses/Unauthorized" },
              "403": { "$ref": "#/components/responses/Forbidden" }
            }
          }
        },
        "/api/v1/vendor/fulfillment-board": {
          "get": {
            "tags": ["Vendor"],
            "summary": "Get real-time vendor fulfillment board with per-plant operational metrics",
            "operationId": HttpOperation::ListVendorFulfillmentBoard.operation_id(),
            "x-fulfillment-governance": {
              "nearRealTime": true,
              "specialRequestStructure": "CONTROLLED_ENUM_ONLY",
              "statusTransitionAudit": true
            },
            "security": [{ "vendorMfaBearer": [] }],
            "parameters": [
              { "$ref": "#/components/parameters/DeliveryDateQuery" },
              { "$ref": "#/components/parameters/PlantIdFilterQuery" },
              { "$ref": "#/components/parameters/IncludeAuditTransitionsQuery" }
            ],
            "responses": {
              "200": {
                "description": "Vendor fulfillment operations board",
                "content": {
                  "application/json": {
                    "schema": { "$ref": "#/components/schemas/VendorFulfillmentBoard" }
                  }
                }
              },
              "400": { "$ref": "#/components/responses/BadRequest" },
              "401": { "$ref": "#/components/responses/Unauthorized" },
              "403": { "$ref": "#/components/responses/Forbidden" }
            }
          }
        },
        "/api/v1/vendor/menu-items/{menuItemId}": {
          "put": {
            "tags": ["Vendor"],
            "summary": "Create or update a vendor menu item",
            "operationId": HttpOperation::UpsertVendorMenuItem.operation_id(),
            "security": [{ "vendorMfaBearer": [] }],
            "parameters": [
              { "$ref": "#/components/parameters/MenuItemIdPath" }
            ],
            "requestBody": {
              "required": true,
              "content": {
                "application/json": {
                  "schema": { "$ref": "#/components/schemas/VendorMenuItemUpsertRequest" }
                }
              }
            },
            "responses": {
              "200": {
                "description": "Menu item upserted",
                "content": {
                  "application/json": {
                    "schema": { "$ref": "#/components/schemas/VendorMenuItem" }
                  }
                }
              },
              "400": { "$ref": "#/components/responses/BadRequest" },
              "401": { "$ref": "#/components/responses/Unauthorized" },
              "403": { "$ref": "#/components/responses/Forbidden" },
              "422": { "$ref": "#/components/responses/ValidationFailed" }
            }
          }
        },
        "/api/v1/vendor/orders/{orderId}/delivery-status": {
          "post": {
            "tags": ["Vendor"],
            "summary": "Advance delivery execution status for a vendor order",
            "operationId": HttpOperation::AdvanceVendorFulfillmentDeliveryStatus.operation_id(),
            "security": [{ "vendorMfaBearer": [] }],
            "parameters": [
              { "$ref": "#/components/parameters/OrderIdPath" }
            ],
            "requestBody": {
              "required": true,
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/VendorFulfillmentDeliveryStatusTransitionRequest"
                  }
                }
              }
            },
            "responses": {
              "202": {
                "description": "Delivery execution status transition accepted",
                "content": {
                  "application/json": {
                    "schema": {
                      "$ref": "#/components/schemas/VendorFulfillmentDeliveryStatusTransitionResult"
                    }
                  }
                }
              },
              "400": { "$ref": "#/components/responses/BadRequest" },
              "401": { "$ref": "#/components/responses/Unauthorized" },
              "403": { "$ref": "#/components/responses/Forbidden" },
              "404": { "$ref": "#/components/responses/NotFound" },
              "409": { "$ref": "#/components/responses/Conflict" },
              "422": { "$ref": "#/components/responses/ValidationFailed" }
            }
          }
        },
        "/api/v1/vendor/fulfillment-batches": {
          "post": {
            "tags": ["Vendor"],
            "summary": "Create immutable fulfillment export batch snapshot",
            "operationId": HttpOperation::CreateVendorFulfillmentExportBatch.operation_id(),
            "x-fulfillment-governance": {
              "snapshotMode": "IMMUTABLE_DETERMINISTIC",
              "artifactTypes": ["DAILY_SUMMARY", "PLANT_PARTITION_SHEET", "LABELS", "BASKET_LIST"]
            },
            "security": [{ "vendorMfaBearer": [] }],
            "requestBody": {
              "required": true,
              "content": {
                "application/json": {
                  "schema": { "$ref": "#/components/schemas/VendorFulfillmentBatchCreateRequest" }
                }
              }
            },
            "responses": {
              "201": {
                "description": "Fulfillment export batch snapshot created",
                "content": {
                  "application/json": {
                    "schema": { "$ref": "#/components/schemas/VendorFulfillmentExportBatch" }
                  }
                }
              },
              "400": { "$ref": "#/components/responses/BadRequest" },
              "401": { "$ref": "#/components/responses/Unauthorized" },
              "403": { "$ref": "#/components/responses/Forbidden" },
              "409": { "$ref": "#/components/responses/Conflict" },
              "422": { "$ref": "#/components/responses/ValidationFailed" }
            }
          }
        },
        "/api/v1/vendor/fulfillment-batches/{batchId}": {
          "get": {
            "tags": ["Vendor"],
            "summary": "Read immutable fulfillment export batch snapshot",
            "operationId": HttpOperation::GetVendorFulfillmentExportBatch.operation_id(),
            "security": [{ "vendorMfaBearer": [] }],
            "parameters": [
              { "$ref": "#/components/parameters/BatchIdPath" }
            ],
            "responses": {
              "200": {
                "description": "Fulfillment export batch snapshot",
                "content": {
                  "application/json": {
                    "schema": { "$ref": "#/components/schemas/VendorFulfillmentExportBatch" }
                  }
                }
              },
              "400": { "$ref": "#/components/responses/BadRequest" },
              "401": { "$ref": "#/components/responses/Unauthorized" },
              "403": { "$ref": "#/components/responses/Forbidden" },
              "404": { "$ref": "#/components/responses/NotFound" }
            }
          }
        },
        "/api/v1/admin/vendors": {
          "get": {
            "tags": ["Admin"],
            "summary": "List vendor enrollments",
            "operationId": HttpOperation::ListAdminVendors.operation_id(),
            "security": [{ "corporateSsoBearer": [] }],
            "parameters": [
              { "$ref": "#/components/parameters/PageQuery" },
              { "$ref": "#/components/parameters/PageSizeQuery" },
              { "$ref": "#/components/parameters/VendorSortByQuery" },
              { "$ref": "#/components/parameters/SortOrderQuery" },
              { "$ref": "#/components/parameters/VendorStatusFilterQuery" }
            ],
            "responses": {
              "200": {
                "description": "Paginated vendor enrollments",
                "content": {
                  "application/json": {
                    "schema": { "$ref": "#/components/schemas/VendorEnrollmentPage" }
                  }
                }
              },
              "400": { "$ref": "#/components/responses/BadRequest" },
              "401": { "$ref": "#/components/responses/Unauthorized" },
              "403": { "$ref": "#/components/responses/Forbidden" }
            }
          }
        },
        "/api/v1/admin/vendor-plant-delivery-mappings": {
          "get": {
            "tags": ["Admin"],
            "summary": "List vendor plant delivery mappings and their audit history",
            "operationId": HttpOperation::ListVendorPlantDeliveryMappings.operation_id(),
            "security": [{ "corporateSsoBearer": [] }],
            "parameters": [
              { "$ref": "#/components/parameters/VendorIdFilterQuery" },
              { "$ref": "#/components/parameters/PlantIdFilterQuery" },
              { "$ref": "#/components/parameters/ServiceWindowActiveAtQuery" },
              { "$ref": "#/components/parameters/PageQuery" },
              { "$ref": "#/components/parameters/PageSizeQuery" }
            ],
            "responses": {
              "200": {
                "description": "Paginated vendor plant delivery mappings",
                "content": {
                  "application/json": {
                    "schema": { "$ref": "#/components/schemas/VendorPlantDeliveryMappingPage" }
                  }
                }
              },
              "400": { "$ref": "#/components/responses/BadRequest" },
              "401": { "$ref": "#/components/responses/Unauthorized" },
              "403": { "$ref": "#/components/responses/Forbidden" }
            }
          }
        },
        "/api/v1/admin/vendors/{vendorId}/plant-delivery-mappings/{mappingId}": {
          "put": {
            "tags": ["Admin"],
            "summary": "Create or update a vendor plant delivery mapping",
            "operationId": HttpOperation::UpsertVendorPlantDeliveryMapping.operation_id(),
            "security": [{ "corporateSsoBearer": [] }],
            "parameters": [
              { "$ref": "#/components/parameters/VendorIdPath" },
              { "$ref": "#/components/parameters/MappingIdPath" }
            ],
            "requestBody": {
              "required": true,
              "content": {
                "application/json": {
                  "schema": { "$ref": "#/components/schemas/VendorPlantDeliveryMappingUpsertRequest" }
                }
              }
            },
            "responses": {
              "200": {
                "description": "Vendor plant delivery mapping upserted",
                "content": {
                  "application/json": {
                    "schema": { "$ref": "#/components/schemas/VendorPlantDeliveryMapping" }
                  }
                }
              },
              "400": { "$ref": "#/components/responses/BadRequest" },
              "401": { "$ref": "#/components/responses/Unauthorized" },
              "403": { "$ref": "#/components/responses/Forbidden" },
              "404": { "$ref": "#/components/responses/NotFound" },
              "422": { "$ref": "#/components/responses/ValidationFailed" }
            }
          },
          "delete": {
            "tags": ["Admin"],
            "summary": "Delete a vendor plant delivery mapping",
            "operationId": HttpOperation::DeleteVendorPlantDeliveryMapping.operation_id(),
            "security": [{ "corporateSsoBearer": [] }],
            "parameters": [
              { "$ref": "#/components/parameters/VendorIdPath" },
              { "$ref": "#/components/parameters/MappingIdPath" }
            ],
            "responses": {
              "204": {
                "description": "Vendor plant delivery mapping deleted"
              },
              "400": { "$ref": "#/components/responses/BadRequest" },
              "401": { "$ref": "#/components/responses/Unauthorized" },
              "403": { "$ref": "#/components/responses/Forbidden" },
              "404": { "$ref": "#/components/responses/NotFound" }
            }
          }
        },
        "/api/v1/admin/compliance/document-templates": {
          "get": {
            "tags": ["Admin"],
            "summary": "List vendor compliance document templates by category",
            "operationId": HttpOperation::ListComplianceDocumentTemplates.operation_id(),
            "security": [{ "corporateSsoBearer": [] }],
            "parameters": [
              { "$ref": "#/components/parameters/VendorCategoryFilterQuery" }
            ],
            "responses": {
              "200": {
                "description": "Compliance document templates",
                "content": {
                  "application/json": {
                    "schema": {
                      "$ref": "#/components/schemas/VendorComplianceDocumentTemplatePage"
                    }
                  }
                }
              },
              "400": { "$ref": "#/components/responses/BadRequest" },
              "401": { "$ref": "#/components/responses/Unauthorized" },
              "403": { "$ref": "#/components/responses/Forbidden" }
            }
          }
        },
        "/api/v1/admin/compliance/document-templates/{vendorCategory}/{templateId}": {
          "put": {
            "tags": ["Admin"],
            "summary": "Create or update a compliance document template for a vendor category",
            "operationId": HttpOperation::UpsertComplianceDocumentTemplate.operation_id(),
            "security": [{ "corporateSsoBearer": [] }],
            "parameters": [
              { "$ref": "#/components/parameters/VendorCategoryPath" },
              { "$ref": "#/components/parameters/TemplateIdPath" }
            ],
            "requestBody": {
              "required": true,
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/VendorComplianceDocumentTemplateUpsertRequest"
                  }
                }
              }
            },
            "responses": {
              "200": {
                "description": "Template upserted",
                "content": {
                  "application/json": {
                    "schema": {
                      "$ref": "#/components/schemas/VendorComplianceDocumentTemplate"
                    }
                  }
                }
              },
              "400": { "$ref": "#/components/responses/BadRequest" },
              "401": { "$ref": "#/components/responses/Unauthorized" },
              "403": { "$ref": "#/components/responses/Forbidden" },
              "422": { "$ref": "#/components/responses/ValidationFailed" }
            }
          }
        },
        "/api/v1/admin/vendors/{vendorId}/reviews": {
          "post": {
            "tags": ["Admin"],
            "summary": "Approve, reject, or request fixes for vendor application",
            "operationId": HttpOperation::ReviewVendorApplication.operation_id(),
            "security": [{ "corporateSsoBearer": [] }],
            "parameters": [
              { "$ref": "#/components/parameters/VendorIdPath" }
            ],
            "requestBody": {
              "required": true,
              "content": {
                "application/json": {
                  "schema": { "$ref": "#/components/schemas/AdminVendorReviewRequest" }
                }
              }
            },
            "responses": {
              "202": {
                "description": "Vendor enrollment decision accepted",
                "content": {
                  "application/json": {
                    "schema": { "$ref": "#/components/schemas/VendorEnrollment" }
                  }
                }
              },
              "400": { "$ref": "#/components/responses/BadRequest" },
              "401": { "$ref": "#/components/responses/Unauthorized" },
              "403": { "$ref": "#/components/responses/Forbidden" },
              "404": { "$ref": "#/components/responses/NotFound" },
              "422": { "$ref": "#/components/responses/ValidationFailed" }
            }
          }
        },
        "/api/v1/admin/compliance/lifecycle/executions": {
          "post": {
            "tags": ["Admin"],
            "summary": "Run automated compliance lifecycle evaluation",
            "operationId": HttpOperation::RunVendorComplianceLifecycle.operation_id(),
            "security": [{ "corporateSsoBearer": [] }],
            "requestBody": {
              "required": true,
              "content": {
                "application/json": {
                  "schema": {
                    "$ref": "#/components/schemas/VendorComplianceLifecycleExecutionRequest"
                  }
                }
              }
            },
            "responses": {
              "202": {
                "description": "Lifecycle evaluation accepted",
                "content": {
                  "application/json": {
                    "schema": {
                      "$ref": "#/components/schemas/VendorComplianceLifecycleExecutionResult"
                    }
                  }
                }
              },
              "400": { "$ref": "#/components/responses/BadRequest" },
              "401": { "$ref": "#/components/responses/Unauthorized" },
              "403": { "$ref": "#/components/responses/Forbidden" },
              "422": { "$ref": "#/components/responses/ValidationFailed" }
            }
          }
        },
        "/api/v1/admin/audit/investigations": {
          "get": {
            "tags": ["Admin"],
            "summary": "Query immutable audit evidence for investigations",
            "operationId": HttpOperation::QueryAuditInvestigations.operation_id(),
            "security": [{ "corporateSsoBearer": [] }],
            "parameters": [
              { "$ref": "#/components/parameters/AuditActorIdFilterQuery" },
              { "$ref": "#/components/parameters/AuditActionFilterQuery" },
              { "$ref": "#/components/parameters/AuditEntityTypeFilterQuery" },
              { "$ref": "#/components/parameters/AuditEntityIdFilterQuery" },
              { "$ref": "#/components/parameters/AuditOccurredFromEpochDayQuery" },
              { "$ref": "#/components/parameters/AuditOccurredToEpochDayQuery" },
              { "$ref": "#/components/parameters/AuditCorrelationIdFilterQuery" }
            ],
            "responses": {
              "200": {
                "description": "Immutable audit evidence matching investigation filters",
                "content": {
                  "application/json": {
                    "schema": { "$ref": "#/components/schemas/AuditInvestigationResponse" }
                  }
                }
              },
              "400": { "$ref": "#/components/responses/BadRequest" },
              "401": { "$ref": "#/components/responses/Unauthorized" },
              "403": { "$ref": "#/components/responses/Forbidden" },
              "500": { "$ref": "#/components/responses/InternalServerError" }
            }
          }
        },
        "/api/v1/admin/audit/responsibilities": {
          "get": {
            "tags": ["Admin"],
            "summary": "Attribute investigation responsibility by actor identity",
            "operationId": HttpOperation::QueryAuditResponsibilities.operation_id(),
            "security": [{ "corporateSsoBearer": [] }],
            "parameters": [
              { "$ref": "#/components/parameters/AuditActorIdFilterQuery" },
              { "$ref": "#/components/parameters/AuditActionFilterQuery" },
              { "$ref": "#/components/parameters/AuditEntityTypeFilterQuery" },
              { "$ref": "#/components/parameters/AuditEntityIdFilterQuery" },
              { "$ref": "#/components/parameters/AuditOccurredFromEpochDayQuery" },
              { "$ref": "#/components/parameters/AuditOccurredToEpochDayQuery" },
              { "$ref": "#/components/parameters/AuditCorrelationIdFilterQuery" }
            ],
            "responses": {
              "200": {
                "description": "Investigation responsibility attribution grouped by actor",
                "content": {
                  "application/json": {
                    "schema": { "$ref": "#/components/schemas/AuditResponsibilityResponse" }
                  }
                }
              },
              "400": { "$ref": "#/components/responses/BadRequest" },
              "401": { "$ref": "#/components/responses/Unauthorized" },
              "403": { "$ref": "#/components/responses/Forbidden" },
              "500": { "$ref": "#/components/responses/InternalServerError" }
            }
          }
        },
        "/api/v1/admin/audit/retention-purge": {
          "post": {
            "tags": ["Admin"],
            "summary": "Execute audit evidence retention purge by policy",
            "operationId": HttpOperation::PurgeAuditEvidence.operation_id(),
            "security": [{ "corporateSsoBearer": [] }],
            "requestBody": {
              "required": true,
              "content": {
                "application/json": {
                  "schema": { "$ref": "#/components/schemas/AuditRetentionPurgeRequest" }
                }
              }
            },
            "responses": {
              "200": {
                "description": "Audit evidence retention purge result",
                "content": {
                  "application/json": {
                    "schema": { "$ref": "#/components/schemas/AuditRetentionPurgeResponse" }
                  }
                }
              },
              "400": { "$ref": "#/components/responses/BadRequest" },
              "401": { "$ref": "#/components/responses/Unauthorized" },
              "403": { "$ref": "#/components/responses/Forbidden" },
              "500": { "$ref": "#/components/responses/InternalServerError" }
            }
          }
        },
        "/api/v1/admin/orders/retention-purge": {
          "post": {
            "tags": ["Admin"],
            "summary": "Execute order retention purge by policy",
            "operationId": HttpOperation::PurgeOrderData.operation_id(),
            "security": [{ "corporateSsoBearer": [] }],
            "requestBody": {
              "required": true,
              "content": {
                "application/json": {
                  "schema": { "$ref": "#/components/schemas/OrderRetentionPurgeRequest" }
                }
              }
            },
            "responses": {
              "200": {
                "description": "Order retention purge result",
                "content": {
                  "application/json": {
                    "schema": { "$ref": "#/components/schemas/OrderRetentionPurgeResponse" }
                  }
                }
              },
              "400": { "$ref": "#/components/responses/BadRequest" },
              "401": { "$ref": "#/components/responses/Unauthorized" },
              "403": { "$ref": "#/components/responses/Forbidden" },
              "500": { "$ref": "#/components/responses/InternalServerError" }
            }
          }
        },
        "/api/v1/admin/anomaly/rules": {
          "get": {
            "tags": ["Admin"],
            "summary": "List anomaly detection governance rules",
            "operationId": HttpOperation::ListAnomalyRules.operation_id(),
            "security": [{ "corporateSsoBearer": [] }],
            "responses": {
              "200": {
                "description": "Configured anomaly detection rules",
                "content": {
                  "application/json": {
                    "schema": { "$ref": "#/components/schemas/AnomalyRuleListResponse" }
                  }
                }
              },
              "401": { "$ref": "#/components/responses/Unauthorized" },
              "403": { "$ref": "#/components/responses/Forbidden" },
              "500": { "$ref": "#/components/responses/InternalServerError" }
            }
          }
        },
        "/api/v1/admin/anomaly/rules/{ruleId}": {
          "put": {
            "tags": ["Admin"],
            "summary": "Upsert anomaly detection governance rule",
            "operationId": HttpOperation::UpsertAnomalyRule.operation_id(),
            "security": [{ "corporateSsoBearer": [] }],
            "parameters": [
              { "$ref": "#/components/parameters/AnomalyRuleIdPath" }
            ],
            "requestBody": {
              "required": true,
              "content": {
                "application/json": {
                  "schema": { "$ref": "#/components/schemas/AnomalyRuleUpsertRequest" }
                }
              }
            },
            "responses": {
              "200": {
                "description": "Upserted anomaly rule",
                "content": {
                  "application/json": {
                    "schema": { "$ref": "#/components/schemas/AnomalyRule" }
                  }
                }
              },
              "400": { "$ref": "#/components/responses/BadRequest" },
              "401": { "$ref": "#/components/responses/Unauthorized" },
              "403": { "$ref": "#/components/responses/Forbidden" },
              "500": { "$ref": "#/components/responses/InternalServerError" }
            }
          }
        },
        "/api/v1/admin/anomaly/alerts/evaluations": {
          "post": {
            "tags": ["Admin"],
            "summary": "Evaluate anomaly rules and trigger tracked remediation alerts",
            "operationId": HttpOperation::EvaluateAnomalyAlerts.operation_id(),
            "security": [{ "corporateSsoBearer": [] }],
            "requestBody": {
              "required": true,
              "content": {
                "application/json": {
                  "schema": { "$ref": "#/components/schemas/AnomalyAlertEvaluationRequest" }
                }
              }
            },
            "responses": {
              "200": {
                "description": "Anomaly evaluation outcome with triggered alerts",
                "content": {
                  "application/json": {
                    "schema": { "$ref": "#/components/schemas/AnomalyAlertEvaluationResponse" }
                  }
                }
              },
              "400": { "$ref": "#/components/responses/BadRequest" },
              "401": { "$ref": "#/components/responses/Unauthorized" },
              "403": { "$ref": "#/components/responses/Forbidden" },
              "500": { "$ref": "#/components/responses/InternalServerError" }
            }
          }
        },
        "/api/v1/admin/anomaly/alerts": {
          "get": {
            "tags": ["Admin"],
            "summary": "Query anomaly alerts with escalation and SLA state",
            "operationId": HttpOperation::ListAnomalyAlerts.operation_id(),
            "security": [{ "corporateSsoBearer": [] }],
            "parameters": [
              { "$ref": "#/components/parameters/VendorIdFilterQuery" },
              { "$ref": "#/components/parameters/AnomalyOwnerActorIdFilterQuery" },
              { "$ref": "#/components/parameters/AnomalyAlertStatusFilterQuery" },
              { "$ref": "#/components/parameters/AnomalyEscalatedOnlyFilterQuery" },
              { "$ref": "#/components/parameters/AnomalySlaStatusFilterQuery" },
              { "$ref": "#/components/parameters/AnomalyAsOfEpochDayQuery" },
              { "$ref": "#/components/parameters/AnomalyAsOfMinuteOfDayQuery" }
            ],
            "responses": {
              "200": {
                "description": "Anomaly alerts that satisfy the supplied filters",
                "content": {
                  "application/json": {
                    "schema": { "$ref": "#/components/schemas/AnomalyAlertListResponse" }
                  }
                }
              },
              "400": { "$ref": "#/components/responses/BadRequest" },
              "401": { "$ref": "#/components/responses/Unauthorized" },
              "403": { "$ref": "#/components/responses/Forbidden" },
              "500": { "$ref": "#/components/responses/InternalServerError" }
            }
          }
        },
        "/api/v1/admin/anomaly/alerts/{alertId}": {
          "patch": {
            "tags": ["Admin"],
            "summary": "Assign owner and advance anomaly remediation lifecycle",
            "operationId": HttpOperation::UpdateAdminAnomalyAlert.operation_id(),
            "security": [{ "corporateSsoBearer": [] }],
            "parameters": [
              { "$ref": "#/components/parameters/AnomalyAlertIdPath" }
            ],
            "requestBody": {
              "required": true,
              "content": {
                "application/json": {
                  "schema": { "$ref": "#/components/schemas/AdminAnomalyAlertPatchRequest" }
                }
              }
            },
            "responses": {
              "200": {
                "description": "Updated anomaly alert lifecycle record",
                "content": {
                  "application/json": {
                    "schema": { "$ref": "#/components/schemas/AnomalyAlert" }
                  }
                }
              },
              "400": { "$ref": "#/components/responses/BadRequest" },
              "401": { "$ref": "#/components/responses/Unauthorized" },
              "403": { "$ref": "#/components/responses/Forbidden" },
              "404": { "$ref": "#/components/responses/NotFound" },
              "409": { "$ref": "#/components/responses/Conflict" },
              "500": { "$ref": "#/components/responses/InternalServerError" }
            }
          }
        },
        "/api/v1/admin/payroll/disputes/{disputeId}": {
          "patch": {
            "tags": ["Admin"],
            "summary": "Assign and resolve payroll disputes with immutable trace",
            "operationId": HttpOperation::UpdateAdminPayrollDispute.operation_id(),
            "security": [{ "corporateSsoBearer": [] }],
            "parameters": [
              { "$ref": "#/components/parameters/DisputeIdPath" }
            ],
            "requestBody": {
              "required": true,
              "content": {
                "application/json": {
                  "schema": { "$ref": "#/components/schemas/AdminPayrollDisputePatchRequest" }
                }
              }
            },
            "responses": {
              "200": {
                "description": "Updated payroll dispute lifecycle record",
                "content": {
                  "application/json": {
                    "schema": { "$ref": "#/components/schemas/PayrollDispute" }
                  }
                }
              },
              "400": { "$ref": "#/components/responses/BadRequest" },
              "401": { "$ref": "#/components/responses/Unauthorized" },
              "403": { "$ref": "#/components/responses/Forbidden" },
              "404": { "$ref": "#/components/responses/NotFound" },
              "409": { "$ref": "#/components/responses/Conflict" },
              "500": { "$ref": "#/components/responses/InternalServerError" }
            }
          }
        },
        "/api/v1/admin/payroll/retention-purge": {
          "post": {
            "tags": ["Admin"],
            "summary": "Execute payroll and dispute retention purge by policy",
            "operationId": HttpOperation::PurgePayrollData.operation_id(),
            "security": [{ "corporateSsoBearer": [] }],
            "requestBody": {
              "required": true,
              "content": {
                "application/json": {
                  "schema": { "$ref": "#/components/schemas/PayrollRetentionPurgeRequest" }
                }
              }
            },
            "responses": {
              "200": {
                "description": "Payroll retention purge result",
                "content": {
                  "application/json": {
                    "schema": { "$ref": "#/components/schemas/PayrollRetentionPurgeResponse" }
                  }
                }
              },
              "400": { "$ref": "#/components/responses/BadRequest" },
              "401": { "$ref": "#/components/responses/Unauthorized" },
              "403": { "$ref": "#/components/responses/Forbidden" },
              "500": { "$ref": "#/components/responses/InternalServerError" }
            }
          }
        },
        "/api/v1/admin/payroll/monthly-settlements/close": {
          "post": {
            "tags": ["Admin"],
            "summary": "Close previous Taipei monthly payroll settlement cycle and emit SFTP snapshot",
            "operationId": HttpOperation::CloseMonthlyPayrollSettlement.operation_id(),
            "x-payroll-exchange-governance": {
              "coreExchangePath": "SFTP_BATCH",
              "optionalAdjunctPath": "HR_API_SYNC",
              "ledgerMode": "APPEND_ONLY",
              "cycleBoundaryTimezone": "Asia/Taipei"
            },
            "security": [{ "corporateSsoBearer": [] }],
            "requestBody": {
              "required": false,
              "content": {
                "application/json": {
                  "schema": { "$ref": "#/components/schemas/PayrollMonthlySettlementCloseRequest" }
                }
              }
            },
            "responses": {
              "200": {
                "description": "Monthly payroll settlement snapshot",
                "content": {
                  "application/json": {
                    "schema": { "$ref": "#/components/schemas/PayrollDeductionPage" }
                  }
                }
              },
              "400": { "$ref": "#/components/responses/BadRequest" },
              "401": { "$ref": "#/components/responses/Unauthorized" },
              "403": { "$ref": "#/components/responses/Forbidden" },
              "409": { "$ref": "#/components/responses/Conflict" },
              "500": { "$ref": "#/components/responses/InternalServerError" }
            }
          }
        },
        "/api/v1/admin/payroll/monthly-settlements/{cycleKey}/lock": {
          "post": {
            "tags": ["Admin"],
            "summary": "Lock a monthly payroll settlement cycle with explicit reason",
            "operationId": HttpOperation::LockPayrollSettlementCycle.operation_id(),
            "security": [{ "corporateSsoBearer": [] }],
            "parameters": [
              { "$ref": "#/components/parameters/PayrollSettlementCycleKeyPath" }
            ],
            "requestBody": {
              "required": true,
              "content": {
                "application/json": {
                  "schema": { "$ref": "#/components/schemas/PayrollSettlementCycleLockRequest" }
                }
              }
            },
            "responses": {
              "200": {
                "description": "Settlement cycle lock state",
                "content": {
                  "application/json": {
                    "schema": { "$ref": "#/components/schemas/PayrollSettlementCycleLockResponse" }
                  }
                }
              },
              "400": { "$ref": "#/components/responses/BadRequest" },
              "401": { "$ref": "#/components/responses/Unauthorized" },
              "403": { "$ref": "#/components/responses/Forbidden" },
              "404": { "$ref": "#/components/responses/NotFound" },
              "409": { "$ref": "#/components/responses/Conflict" },
              "500": { "$ref": "#/components/responses/InternalServerError" }
            }
          }
        },
        "/api/v1/admin/payroll/monthly-settlements/{cycleKey}/unlock": {
          "post": {
            "tags": ["Admin"],
            "summary": "Unlock a monthly payroll settlement cycle for authorized recomputation",
            "operationId": HttpOperation::UnlockPayrollSettlementCycle.operation_id(),
            "security": [{ "corporateSsoBearer": [] }],
            "parameters": [
              { "$ref": "#/components/parameters/PayrollSettlementCycleKeyPath" }
            ],
            "requestBody": {
              "required": true,
              "content": {
                "application/json": {
                  "schema": { "$ref": "#/components/schemas/PayrollSettlementCycleLockRequest" }
                }
              }
            },
            "responses": {
              "200": {
                "description": "Settlement cycle lock state",
                "content": {
                  "application/json": {
                    "schema": { "$ref": "#/components/schemas/PayrollSettlementCycleLockResponse" }
                  }
                }
              },
              "400": { "$ref": "#/components/responses/BadRequest" },
              "401": { "$ref": "#/components/responses/Unauthorized" },
              "403": { "$ref": "#/components/responses/Forbidden" },
              "404": { "$ref": "#/components/responses/NotFound" },
              "409": { "$ref": "#/components/responses/Conflict" },
              "500": { "$ref": "#/components/responses/InternalServerError" }
            }
          }
        },
        "/api/v1/integrations/payroll/deductions": {
          "get": {
            "tags": ["Integration"],
            "summary": "Export payroll deduction records",
            "operationId": HttpOperation::ExportPayrollDeductions.operation_id(),
            "x-payroll-exchange-governance": {
              "coreExchangePath": "SFTP_BATCH",
              "optionalAdjunctPath": "HR_API_SYNC",
              "ledgerMode": "APPEND_ONLY"
            },
            "security": [{ "corporateSsoBearer": [] }],
            "parameters": [
              { "$ref": "#/components/parameters/PayPeriodQuery" },
              { "$ref": "#/components/parameters/PayrollCycleKeyQuery" },
              { "$ref": "#/components/parameters/PageQuery" },
              { "$ref": "#/components/parameters/PageSizeQuery" },
              { "$ref": "#/components/parameters/PayrollSortByQuery" },
              { "$ref": "#/components/parameters/SortOrderQuery" }
            ],
            "responses": {
              "200": {
                "description": "Payroll deduction export page",
                "content": {
                  "application/json": {
                    "schema": { "$ref": "#/components/schemas/PayrollDeductionPage" }
                  }
                }
              },
              "400": { "$ref": "#/components/responses/BadRequest" },
              "401": { "$ref": "#/components/responses/Unauthorized" },
              "403": { "$ref": "#/components/responses/Forbidden" },
              "500": { "$ref": "#/components/responses/InternalServerError" }
            }
          }
        },
        "/api/v1/integrations/payroll/sftp-batches/{batchId}/hr-api-sync": {
          "post": {
            "tags": ["Integration"],
            "summary": "Trigger optional HR API adjunct sync for an SFTP payroll batch",
            "operationId": HttpOperation::SyncPayrollHrApiAdjunct.operation_id(),
            "security": [{ "corporateSsoBearer": [] }],
            "parameters": [
              { "$ref": "#/components/parameters/PayrollExchangeBatchIdPath" }
            ],
            "requestBody": {
              "required": true,
              "content": {
                "application/json": {
                  "schema": { "$ref": "#/components/schemas/PayrollHrApiSyncRequest" }
                }
              }
            },
            "responses": {
              "200": {
                "description": "Batch HR API adjunct sync status",
                "content": {
                  "application/json": {
                    "schema": { "$ref": "#/components/schemas/PayrollHrApiSyncResponse" }
                  }
                }
              },
              "400": { "$ref": "#/components/responses/BadRequest" },
              "401": { "$ref": "#/components/responses/Unauthorized" },
              "403": { "$ref": "#/components/responses/Forbidden" },
              "404": { "$ref": "#/components/responses/NotFound" },
              "500": { "$ref": "#/components/responses/InternalServerError" }
            }
          }
        },
        "/mcp/v1/tools": {
          "get": {
            "tags": ["MCP"],
            "summary": "List MCP tools granted to the authenticated OAuth service account",
            "operationId": "listMcpTools",
            "security": [{ "mcpOAuthServiceAccountBearer": [] }],
            "responses": {
              "200": {
                "description": "MCP tool catalog scoped to granted tools",
                "content": {
                  "application/json": {
                    "schema": { "$ref": "#/components/schemas/McpToolCatalogResponse" }
                  }
                }
              },
              "401": { "$ref": "#/components/responses/Unauthorized" },
              "500": { "$ref": "#/components/responses/InternalServerError" }
            }
          }
        },
        "/mcp/v1/resources": {
          "get": {
            "tags": ["MCP"],
            "summary": "List MCP resources available for granted capability domains",
            "operationId": "listMcpResources",
            "security": [{ "mcpOAuthServiceAccountBearer": [] }],
            "responses": {
              "200": {
                "description": "MCP resource catalog scoped by granted domains",
                "content": {
                  "application/json": {
                    "schema": { "$ref": "#/components/schemas/McpResourceCatalogResponse" }
                  }
                }
              },
              "401": { "$ref": "#/components/responses/Unauthorized" },
              "500": { "$ref": "#/components/responses/InternalServerError" }
            }
          }
        },
        "/mcp/v1/tools/{toolName}/invoke": {
          "post": {
            "tags": ["MCP"],
            "summary": "Invoke an MCP tool with tool-level RBAC and shared-domain execution",
            "operationId": "invokeMcpTool",
            "security": [{ "mcpOAuthServiceAccountBearer": [] }],
            "parameters": [
              { "$ref": "#/components/parameters/McpToolNamePath" }
            ],
            "requestBody": {
              "required": true,
              "content": {
                "application/json": {
                  "schema": { "$ref": "#/components/schemas/McpToolInvocationRequest" }
                }
              }
            },
            "responses": {
              "200": {
                "description": "MCP tool invocation result",
                "content": {
                  "application/json": {
                    "schema": { "$ref": "#/components/schemas/McpToolInvocationResponse" }
                  }
                }
              },
              "400": { "$ref": "#/components/responses/BadRequest" },
              "401": { "$ref": "#/components/responses/Unauthorized" },
              "403": { "$ref": "#/components/responses/Forbidden" },
              "404": { "$ref": "#/components/responses/NotFound" },
              "409": { "$ref": "#/components/responses/Conflict" },
              "500": { "$ref": "#/components/responses/InternalServerError" }
            }
          }
        }
      },
      "components": {
        "securitySchemes": {
          "corporateSsoBearer": {
            "type": "http",
            "scheme": "bearer",
            "bearerFormat": "JWT",
            "description": "Corporate SSO issued bearer token.",
            "x-authentication-source": "CORPORATE_SSO"
          },
          "vendorMfaBearer": {
            "type": "http",
            "scheme": "bearer",
            "bearerFormat": "JWT",
            "description": "Vendor account token issued only after MFA challenge.",
            "x-authentication-source": "VENDOR_ACCOUNT_MFA"
          },
          "mcpOAuthServiceAccountBearer": {
            "type": "http",
            "scheme": "bearer",
            "bearerFormat": "JWT",
            "description": "OAuth service-account bearer token for MCP tool and resource access.",
            "x-authentication-source": "OAUTH_SERVICE_ACCOUNT"
          }
        },
        "parameters": {
          "PlantIdQuery": {
            "name": "plantId",
            "in": "query",
            "required": true,
            "description": "Target plant for scoping.",
            "schema": { "$ref": "#/components/schemas/PlantId" }
          },
          "MenuDateQuery": {
            "name": "menuDate",
            "in": "query",
            "required": false,
            "description": "Anchor date for week/calendar discovery windows in Asia/Taipei.",
            "schema": {
              "type": "string",
              "format": "date"
            }
          },
          "DeliveryDateQuery": {
            "name": "deliveryDate",
            "in": "query",
            "required": true,
            "description": "Target delivery date in Asia/Taipei for fulfillment board and export snapshots.",
            "schema": {
              "type": "string",
              "format": "date"
            }
          },
          "DiscoveryViewQuery": {
            "name": "view",
            "in": "query",
            "required": false,
            "schema": { "$ref": "#/components/schemas/MenuDiscoveryView" }
          },
          "FromDateQuery": {
            "name": "fromDate",
            "in": "query",
            "required": false,
            "schema": {
              "type": "string",
              "format": "date"
            }
          },
          "ToDateQuery": {
            "name": "toDate",
            "in": "query",
            "required": false,
            "schema": {
              "type": "string",
              "format": "date"
            }
          },
          "PayPeriodQuery": {
            "name": "payPeriod",
            "in": "query",
            "required": true,
            "schema": {
              "type": "string",
              "pattern": "^[0-9]{4}-[0-9]{2}$",
              "examples": ["2026-04"]
            }
          },
          "PayrollCycleKeyQuery": {
            "name": "cycleKey",
            "in": "query",
            "required": true,
            "schema": {
              "type": "string",
              "pattern": "^[A-Za-z0-9._-]{1,64}$"
            }
          },
          "PayrollSettlementCycleKeyPath": {
            "name": "cycleKey",
            "in": "path",
            "required": true,
            "schema": {
              "type": "string",
              "pattern": "^[A-Za-z0-9._-]{1,64}$"
            }
          },
          "PageQuery": {
            "name": "page",
            "in": "query",
            "required": false,
            "schema": {
              "type": "integer",
              "minimum": 1,
              "default": 1
            }
          },
          "PageSizeQuery": {
            "name": "pageSize",
            "in": "query",
            "required": false,
            "schema": {
              "type": "integer",
              "minimum": 1,
              "maximum": 200,
              "default": 20
            }
          },
          "SortOrderQuery": {
            "name": "sortOrder",
            "in": "query",
            "required": false,
            "schema": { "$ref": "#/components/schemas/SortOrder" }
          },
          "MenuSortByQuery": {
            "name": "sortBy",
            "in": "query",
            "required": false,
            "schema": { "$ref": "#/components/schemas/MenuSortField" }
          },
          "VendorOrderSortByQuery": {
            "name": "sortBy",
            "in": "query",
            "required": false,
            "schema": { "$ref": "#/components/schemas/VendorOrderSortField" }
          },
          "VendorSortByQuery": {
            "name": "sortBy",
            "in": "query",
            "required": false,
            "schema": { "$ref": "#/components/schemas/VendorSortField" }
          },
          "PayrollSortByQuery": {
            "name": "sortBy",
            "in": "query",
            "required": false,
            "schema": { "$ref": "#/components/schemas/PayrollSortField" }
          },
          "MenuSearchQuery": {
            "name": "search",
            "in": "query",
            "required": false,
            "schema": {
              "type": "string",
              "minLength": 1,
              "maxLength": 120
            }
          },
          "MenuTypeFilterQuery": {
            "name": "menuType",
            "in": "query",
            "required": false,
            "schema": { "$ref": "#/components/schemas/MenuType" }
          },
          "HealthTagFilterQuery": {
            "name": "healthTag",
            "in": "query",
            "required": false,
            "schema": { "$ref": "#/components/schemas/MenuHealthTag" }
          },
          "PriceMinMinorQuery": {
            "name": "priceMinMinor",
            "in": "query",
            "required": false,
            "schema": {
              "type": "integer",
              "minimum": 0
            }
          },
          "PriceMaxMinorQuery": {
            "name": "priceMaxMinor",
            "in": "query",
            "required": false,
            "schema": {
              "type": "integer",
              "minimum": 0
            }
          },
          "RemainingQuantityFilterQuery": {
            "name": "remainingQuantity",
            "in": "query",
            "required": false,
            "description": "Exact inventory counter filter. Matches only items whose remaining quantity equals this value.",
            "schema": {
              "type": "integer",
              "minimum": 0,
              "maximum": 2000
            }
          },
          "RecommendationEnabledQuery": {
            "name": "recommendationEnabled",
            "in": "query",
            "required": false,
            "description": "Recommendation flag is accepted for forward compatibility but deterministic filters remain authoritative in MVP.",
            "schema": {
              "type": "boolean",
              "default": false
            }
          },
          "IncludeAuditTransitionsQuery": {
            "name": "includeAuditTransitions",
            "in": "query",
            "required": false,
            "description": "When false, omits status transition audit entries from fulfillment board payload.",
            "schema": {
              "type": "boolean",
              "default": true
            }
          },
          "OrderStatusFilterQuery": {
            "name": "status",
            "in": "query",
            "required": false,
            "schema": { "$ref": "#/components/schemas/EmployeeOrderStatus" }
          },
          "VendorStatusFilterQuery": {
            "name": "status",
            "in": "query",
            "required": false,
            "schema": { "$ref": "#/components/schemas/VendorStatus" }
          },
          "VendorCategoryFilterQuery": {
            "name": "vendorCategory",
            "in": "query",
            "required": false,
            "schema": { "$ref": "#/components/schemas/VendorCategory" }
          },
          "VendorIdFilterQuery": {
            "name": "vendorId",
            "in": "query",
            "required": false,
            "schema": {
              "type": "string",
              "pattern": "^ven-[a-z0-9]{8,32}$"
            }
          },
          "PlantIdFilterQuery": {
            "name": "plantId",
            "in": "query",
            "required": false,
            "schema": { "$ref": "#/components/schemas/PlantId" }
          },
          "ServiceWindowActiveAtQuery": {
            "name": "activeAt",
            "in": "query",
            "required": false,
            "description": "Evaluate mappings active at this fixed Asia/Taipei business timestamp.",
            "schema": { "$ref": "#/components/schemas/TaipeiBusinessDateTime" }
          },
          "AuditActorIdFilterQuery": {
            "name": "actorId",
            "in": "query",
            "required": false,
            "schema": { "$ref": "#/components/schemas/ActorId" }
          },
          "AuditActionFilterQuery": {
            "name": "action",
            "in": "query",
            "required": false,
            "schema": { "$ref": "#/components/schemas/AuditAction" }
          },
          "AuditEntityTypeFilterQuery": {
            "name": "entityType",
            "in": "query",
            "required": false,
            "schema": { "$ref": "#/components/schemas/AuditEntityType" }
          },
          "AuditEntityIdFilterQuery": {
            "name": "entityId",
            "in": "query",
            "required": false,
            "schema": {
              "type": "string",
              "minLength": 1,
              "maxLength": 128
            }
          },
          "AuditOccurredFromEpochDayQuery": {
            "name": "occurredFromEpochDay",
            "in": "query",
            "required": false,
            "schema": {
              "type": "integer"
            }
          },
          "AuditOccurredToEpochDayQuery": {
            "name": "occurredToEpochDay",
            "in": "query",
            "required": false,
            "schema": {
              "type": "integer"
            }
          },
          "AuditCorrelationIdFilterQuery": {
            "name": "correlationId",
            "in": "query",
            "required": false,
            "schema": {
              "type": "string",
              "minLength": 1,
              "maxLength": 256
            }
          },
          "AnomalyOwnerActorIdFilterQuery": {
            "name": "ownerActorId",
            "in": "query",
            "required": false,
            "schema": { "$ref": "#/components/schemas/ActorId" }
          },
          "AnomalyAlertStatusFilterQuery": {
            "name": "status",
            "in": "query",
            "required": false,
            "schema": { "$ref": "#/components/schemas/AnomalyAlertStatus" }
          },
          "AnomalyEscalatedOnlyFilterQuery": {
            "name": "escalatedOnly",
            "in": "query",
            "required": false,
            "schema": {
              "type": "boolean"
            }
          },
          "AnomalySlaStatusFilterQuery": {
            "name": "slaStatus",
            "in": "query",
            "required": false,
            "schema": { "$ref": "#/components/schemas/AnomalySlaStatus" }
          },
          "AnomalyAsOfEpochDayQuery": {
            "name": "asOfEpochDay",
            "in": "query",
            "required": false,
            "schema": {
              "type": "integer"
            }
          },
          "AnomalyAsOfMinuteOfDayQuery": {
            "name": "asOfMinuteOfDay",
            "in": "query",
            "required": false,
            "schema": {
              "type": "integer",
              "minimum": 0,
              "maximum": 1439
            }
          },
          "OrderIdPath": {
            "name": "orderId",
            "in": "path",
            "required": true,
            "schema": {
              "type": "string",
              "pattern": "^ord-[a-z0-9]{8,32}$"
            }
          },
          "AnomalyRuleIdPath": {
            "name": "ruleId",
            "in": "path",
            "required": true,
            "schema": {
              "type": "string",
              "pattern": "^rule-[a-z0-9-]{3,64}$"
            }
          },
          "AnomalyAlertIdPath": {
            "name": "alertId",
            "in": "path",
            "required": true,
            "schema": {
              "type": "string",
              "pattern": "^alt-[0-9a-f]{16}$"
            }
          },
          "DisputeIdPath": {
            "name": "disputeId",
            "in": "path",
            "required": true,
            "schema": {
              "type": "string",
              "pattern": "^dsp-[0-9a-f]{16}$"
            }
          },
          "MenuItemIdPath": {
            "name": "menuItemId",
            "in": "path",
            "required": true,
            "schema": {
              "type": "string",
              "pattern": "^menu-[a-z0-9]{8,32}$"
            }
          },
          "VendorIdPath": {
            "name": "vendorId",
            "in": "path",
            "required": true,
            "schema": {
              "type": "string",
              "pattern": "^ven-[a-z0-9]{8,32}$"
            }
          },
          "VendorCategoryPath": {
            "name": "vendorCategory",
            "in": "path",
            "required": true,
            "schema": { "$ref": "#/components/schemas/VendorCategory" }
          },
          "TemplateIdPath": {
            "name": "templateId",
            "in": "path",
            "required": true,
            "schema": {
              "type": "string",
              "pattern": "^tmpl-[a-z0-9-]{3,64}$"
            }
          },
          "MappingIdPath": {
            "name": "mappingId",
            "in": "path",
            "required": true,
            "schema": {
              "type": "string",
              "pattern": "^map-[a-z0-9-]{3,64}$"
            }
          },
          "BatchIdPath": {
            "name": "batchId",
            "in": "path",
            "required": true,
            "schema": {
              "type": "string",
              "pattern": "^fbatch-[0-9-]{3,64}$"
            }
          },
          "PayrollExchangeBatchIdPath": {
            "name": "batchId",
            "in": "path",
            "required": true,
            "schema": {
              "type": "string",
              "pattern": "^sftp-[0-9]{6}-[0-9a-f]{16}$"
            }
          },
          "McpToolNamePath": {
            "name": "toolName",
            "in": "path",
            "required": true,
            "description": "MCP tool name, for example `ordering.create_employee_order`.",
            "schema": {
              "type": "string",
              "minLength": 1,
              "maxLength": 128,
              "pattern": "^[a-z]+(?:\\.[a-z0-9_]+)+$"
            }
          }
        },
        "responses": {
          "BadRequest": {
            "description": "Request payload or query is invalid.",
            "content": {
              "application/json": {
                "schema": { "$ref": "#/components/schemas/ErrorResponse" }
              }
            }
          },
          "Unauthorized": {
            "description": "Authentication token is missing or invalid.",
            "content": {
              "application/json": {
                "schema": { "$ref": "#/components/schemas/ErrorResponse" }
              }
            }
          },
          "Forbidden": {
            "description": "Authenticated actor is not authorized to perform this operation.",
            "content": {
              "application/json": {
                "schema": { "$ref": "#/components/schemas/ErrorResponse" }
              }
            }
          },
          "NotFound": {
            "description": "Requested resource was not found.",
            "content": {
              "application/json": {
                "schema": { "$ref": "#/components/schemas/ErrorResponse" }
              }
            }
          },
          "Conflict": {
            "description": "Request conflicts with business constraints.",
            "content": {
              "application/json": {
                "schema": { "$ref": "#/components/schemas/ErrorResponse" }
              }
            }
          },
          "ValidationFailed": {
            "description": "Request is syntactically valid but violates business validation rules.",
            "content": {
              "application/json": {
                "schema": { "$ref": "#/components/schemas/ErrorResponse" }
              }
            }
          },
          "InternalServerError": {
            "description": "Internal server error while processing request.",
            "content": {
              "application/json": {
                "schema": { "$ref": "#/components/schemas/ErrorResponse" }
              }
            }
          }
        },
        "schemas": {
          "ActorId": {
            "type": "string",
            "pattern": "^[a-z0-9][a-z0-9-]{2,63}$"
          },
          "PlantId": {
            "type": "string",
            "pattern": "^[a-z0-9][a-z0-9-]{1,31}$"
          },
          "Role": {
            "type": "string",
            "enum": [
              "EMPLOYEE",
              "VENDOR_OPERATOR",
              "COMMITTEE_ADMIN",
              "PAYROLL_OPERATOR"
            ]
          },
          "AuthenticationSource": {
            "type": "string",
            "enum": [
              "CORPORATE_SSO",
              "VENDOR_ACCOUNT_MFA",
              "OAUTH_SERVICE_ACCOUNT"
            ]
          },
          "McpCapabilityDomain": {
            "type": "string",
            "enum": [
              "ordering",
              "verification",
              "compliance-review",
              "settlement",
              "anomaly"
            ]
          },
          "McpToolRisk": {
            "type": "string",
            "enum": [
              "READ_ONLY",
              "WRITE",
              "HIGH_RISK_WRITE"
            ]
          },
          "McpToolCatalogItem": {
            "type": "object",
            "required": ["name", "operationId", "capabilityDomain", "risk"],
            "properties": {
              "name": {
                "type": "string",
                "minLength": 1,
                "maxLength": 128
              },
              "operationId": {
                "type": "string",
                "minLength": 1,
                "maxLength": 128
              },
              "capabilityDomain": { "$ref": "#/components/schemas/McpCapabilityDomain" },
              "risk": { "$ref": "#/components/schemas/McpToolRisk" }
            },
            "additionalProperties": false
          },
          "McpToolCatalogResponse": {
            "type": "object",
            "required": ["tools"],
            "properties": {
              "tools": {
                "type": "array",
                "items": { "$ref": "#/components/schemas/McpToolCatalogItem" }
              }
            },
            "additionalProperties": false
          },
          "McpResourceCatalogItem": {
            "type": "object",
            "required": ["uri", "capabilityDomain"],
            "properties": {
              "uri": {
                "type": "string",
                "minLength": 1,
                "maxLength": 128
              },
              "capabilityDomain": { "$ref": "#/components/schemas/McpCapabilityDomain" }
            },
            "additionalProperties": false
          },
          "McpResourceCatalogResponse": {
            "type": "object",
            "required": ["resources"],
            "properties": {
              "resources": {
                "type": "array",
                "items": { "$ref": "#/components/schemas/McpResourceCatalogItem" }
              }
            },
            "additionalProperties": false
          },
          "McpToolInvocationRequest": {
            "type": "object",
            "properties": {
              "args": {
                "description": "Tool arguments encoded as JSON. Shape depends on the selected MCP tool.",
                "default": {}
              }
            },
            "additionalProperties": false
          },
          "McpToolInvocationResponse": {
            "type": "object",
            "required": ["toolName", "capabilityDomain", "risk", "result"],
            "properties": {
              "toolName": {
                "type": "string",
                "minLength": 1,
                "maxLength": 128
              },
              "capabilityDomain": { "$ref": "#/components/schemas/McpCapabilityDomain" },
              "risk": { "$ref": "#/components/schemas/McpToolRisk" },
              "result": {
                "description": "Tool-specific JSON response payload."
              }
            },
            "additionalProperties": false
          },
          "AuditAction": {
            "type": "string",
            "enum": [
              "CREATE_EMPLOYEE_ORDER",
              "UPDATE_EMPLOYEE_ORDER",
              "VERIFY_PICKUP_ORDER",
              "MARK_ORDER_SOLD_OUT",
              "MARK_ORDER_REFUND_PENDING",
              "MARK_ORDER_REFUNDED",
              "UPSERT_VENDOR_MENU_ITEM",
              "UPSERT_VENDOR_ORDERING_POLICY",
              "ADVANCE_VENDOR_FULFILLMENT_DELIVERY_STATUS",
              "CREATE_VENDOR_FULFILLMENT_EXPORT_BATCH",
              "UPSERT_VENDOR_PLANT_DELIVERY_MAPPING",
              "DELETE_VENDOR_PLANT_DELIVERY_MAPPING",
              "UPSERT_COMPLIANCE_DOCUMENT_TEMPLATE",
              "REGISTER_VENDOR_APPLICATION",
              "SUBMIT_VENDOR_COMPLIANCE_DOCUMENT",
              "REVIEW_VENDOR_APPLICATION",
              "RUN_VENDOR_COMPLIANCE_LIFECYCLE",
              "PURGE_AUDIT_EVIDENCE",
              "PRUNE_VENDOR_COMPLIANCE_HISTORY",
              "EXPORT_PAYROLL_DEDUCTIONS",
              "APPEND_PAYROLL_LEDGER_ENTRY",
              "OPEN_PAYROLL_DISPUTE",
              "ASSIGN_PAYROLL_DISPUTE_OWNER",
              "RESOLVE_PAYROLL_DISPUTE",
              "EXPORT_PAYROLL_SFTP_BATCH",
              "LOCK_PAYROLL_SETTLEMENT_CYCLE",
              "UNLOCK_PAYROLL_SETTLEMENT_CYCLE",
              "SYNC_PAYROLL_HR_API_ADJUNCT",
              "PURGE_PAYROLL_DATA",
              "PURGE_ORDER_DATA",
              "UPSERT_ANOMALY_DETECTION_RULE",
              "TRIGGER_ANOMALY_ALERT",
              "ASSIGN_ANOMALY_ALERT_OWNER",
              "ADVANCE_ANOMALY_ALERT_STATUS",
              "CLOSE_ANOMALY_ALERT"
            ]
          },
          "AuditEntityType": {
            "type": "string",
            "enum": [
              "ORDER",
              "MENU_ITEM",
              "VENDOR",
              "DELIVERY_MAPPING",
              "COMPLIANCE_DOCUMENT_TEMPLATE",
              "FULFILLMENT_BATCH",
              "SETTLEMENT",
              "VENDOR_ORDERING_POLICY",
              "AUDIT_TRAIL",
              "PAYROLL_LEDGER_ENTRY",
              "PAYROLL_DISPUTE",
              "PAYROLL_EXCHANGE_BATCH",
              "PAYROLL_DATA_RETENTION",
              "ANOMALY_RULE",
              "ANOMALY_ALERT"
            ]
          },
          "AuditEntityRef": {
            "type": "object",
            "required": ["entityType", "entityId"],
            "properties": {
              "entityType": { "$ref": "#/components/schemas/AuditEntityType" },
              "entityId": { "type": "string", "minLength": 1, "maxLength": 128 }
            },
            "additionalProperties": false
          },
          "AuditEvidence": {
            "type": "object",
            "required": [
              "evidenceId",
              "occurredAt",
              "actorId",
              "actorRole",
              "authenticationSource",
              "operationId",
              "action",
              "entityType",
              "entityId",
              "reason",
              "correlationId"
            ],
            "properties": {
              "evidenceId": { "type": "integer", "minimum": 1 },
              "occurredAt": { "$ref": "#/components/schemas/TaipeiBusinessDateTime" },
              "actorId": { "$ref": "#/components/schemas/ActorId" },
              "actorRole": { "$ref": "#/components/schemas/Role" },
              "authenticationSource": { "$ref": "#/components/schemas/AuthenticationSource" },
              "operationId": { "type": "string", "minLength": 1, "maxLength": 128 },
              "action": { "$ref": "#/components/schemas/AuditAction" },
              "entityType": { "$ref": "#/components/schemas/AuditEntityType" },
              "entityId": { "type": "string", "minLength": 1, "maxLength": 128 },
              "reason": { "type": "string", "minLength": 1, "maxLength": 280 },
              "correlationId": { "type": "string", "minLength": 1, "maxLength": 256 }
            },
            "additionalProperties": false
          },
          "AuditInvestigationResponse": {
            "type": "object",
            "required": ["items"],
            "properties": {
              "items": {
                "type": "array",
                "items": { "$ref": "#/components/schemas/AuditEvidence" }
              }
            },
            "additionalProperties": false
          },
          "AuditResponsibilityAttribution": {
            "type": "object",
            "required": [
              "actorId",
              "role",
              "authenticationSource",
              "eventCount",
              "actions",
              "entities"
            ],
            "properties": {
              "actorId": { "$ref": "#/components/schemas/ActorId" },
              "role": { "$ref": "#/components/schemas/Role" },
              "authenticationSource": { "$ref": "#/components/schemas/AuthenticationSource" },
              "eventCount": { "type": "integer", "minimum": 0 },
              "actions": {
                "type": "array",
                "items": { "$ref": "#/components/schemas/AuditAction" }
              },
              "entities": {
                "type": "array",
                "items": { "$ref": "#/components/schemas/AuditEntityRef" }
              }
            },
            "additionalProperties": false
          },
          "AuditResponsibilityResponse": {
            "type": "object",
            "required": ["items"],
            "properties": {
              "items": {
                "type": "array",
                "items": { "$ref": "#/components/schemas/AuditResponsibilityAttribution" }
              }
            },
            "additionalProperties": false
          },
          "AuditRetentionPurgeRequest": {
            "type": "object",
            "properties": {
              "asOfEpochDay": { "type": "integer" }
            },
            "additionalProperties": false
          },
          "AuditRetentionPurgeResponse": {
            "type": "object",
            "required": ["purgedEvents", "asOfEpochDay"],
            "properties": {
              "purgedEvents": { "type": "integer", "minimum": 0 },
              "asOfEpochDay": { "type": "integer" }
            },
            "additionalProperties": false
          },
          "OrderRetentionPurgeRequest": {
            "type": "object",
            "properties": {
              "asOfEpochDay": { "type": "integer" }
            },
            "additionalProperties": false
          },
          "OrderRetentionPurgeResponse": {
            "type": "object",
            "required": ["purgedOrders", "asOfEpochDay"],
            "properties": {
              "purgedOrders": { "type": "integer", "minimum": 0 },
              "asOfEpochDay": { "type": "integer" }
            },
            "additionalProperties": false
          },
          "SortOrder": {
            "type": "string",
            "enum": ["asc", "desc"],
            "default": "asc"
          },
          "MenuSortField": {
            "type": "string",
            "enum": ["name", "priceMinor", "remainingQuantity", "deliveryDate"]
          },
          "MenuDiscoveryView": {
            "type": "string",
            "enum": ["week", "calendar"],
            "default": "week"
          },
          "MenuType": {
            "type": "string",
            "pattern": "^[A-Z][A-Z0-9_]{0,31}$"
          },
          "VendorOrderSortField": {
            "type": "string",
            "enum": ["deliveryDate", "plantId", "status", "createdAt"]
          },
          "VendorSortField": {
            "type": "string",
            "enum": ["createdAt", "status", "displayName", "vendorCategory"]
          },
          "PayrollSortField": {
            "type": "string",
            "enum": ["employeeActorId", "amountMinor", "deliveryDate"]
          },
          "MenuHealthTag": {
            "type": "string",
            "enum": ["LOW_CALORIE", "HIGH_PROTEIN", "VEGETARIAN", "VEGAN", "GLUTEN_FREE"]
          },
          "SpecialRequestOption": {
            "type": "string",
            "enum": [
              "LESS_RICE",
              "NO_GREEN_ONION",
              "SAUCE_ON_SIDE",
              "NO_UTENSILS",
              "EXTRA_SPICY"
            ],
            "description": "Controlled special-request options. Free-text instructions are intentionally disabled pending final policy clarification."
          },
          "EmployeeOrderStatus": {
            "type": "string",
            "enum": [
              "PENDING",
              "MODIFIED",
              "CANCELLED",
              "SOLD_OUT",
              "REFUND_PENDING",
              "REFUNDED",
              "FULFILLED"
            ]
          },
          "OrderTimelineEventType": {
            "type": "string",
            "enum": [
              "CREATED",
              "MODIFIED",
              "CANCELLED",
              "SOLD_OUT",
              "REFUND_PENDING",
              "REFUNDED",
              "FULFILLED"
            ]
          },
          "OrderTimelineEvent": {
            "type": "object",
            "required": ["occurredAt", "eventType", "status"],
            "properties": {
              "occurredAt": { "$ref": "#/components/schemas/TaipeiBusinessDateTime" },
              "eventType": { "$ref": "#/components/schemas/OrderTimelineEventType" },
              "status": { "$ref": "#/components/schemas/EmployeeOrderStatus" }
            },
            "additionalProperties": false
          },
          "VendorStatus": {
            "type": "string",
            "enum": [
              "PENDING_REVIEW",
              "FIX_REQUESTED",
              "APPROVED",
              "REJECTED",
              "SUSPENDED"
            ]
          },
          "VendorCategory": {
            "type": "string",
            "enum": ["RESTAURANT", "BEVERAGE", "DESSERT", "HEALTHY_MEAL", "SNACK"]
          },
          "VendorReviewDecision": {
            "type": "string",
            "enum": ["APPROVED", "REJECTED", "REQUEST_FIX"]
          },
          "VendorLifecycleEventType": {
            "type": "string",
            "enum": [
              "APPLICATION_SUBMITTED",
              "DOCUMENT_SUBMITTED",
              "REVIEW_DECISION",
              "EXPIRY_REMINDER_ISSUED",
              "SUSPENDED",
              "REINSTATED"
            ]
          },
          "VendorSuspensionReasonCode": {
            "type": "string",
            "enum": ["MISSING_REQUIRED_DOCUMENT", "EXPIRED_REQUIRED_DOCUMENT"]
          },
          "VendorComplianceDocumentStatus": {
            "type": "string",
            "enum": ["VALID", "EXPIRING_SOON", "EXPIRED", "MISSING"]
          },
          "TaipeiBusinessDateTime": {
            "type": "string",
            "format": "date-time",
            "pattern": "^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}\\+08:00$",
            "description": "Fixed Asia/Taipei business timestamp. Always serialize with +08:00 offset."
          },
          "VendorPlantDeliveryRuleEffect": {
            "type": "string",
            "enum": ["ALLOW", "DENY"]
          },
          "VendorPlantDeliveryServiceWindow": {
            "type": "object",
            "required": ["startsAt", "endsAt"],
            "properties": {
              "startsAt": { "$ref": "#/components/schemas/TaipeiBusinessDateTime" },
              "endsAt": { "$ref": "#/components/schemas/TaipeiBusinessDateTime" }
            },
            "additionalProperties": false
          },
          "VendorPlantDeliveryMappingUpsertRequest": {
            "type": "object",
            "required": ["plantId", "serviceWindow", "effect", "precedence"],
            "properties": {
              "plantId": { "$ref": "#/components/schemas/PlantId" },
              "serviceWindow": {
                "$ref": "#/components/schemas/VendorPlantDeliveryServiceWindow"
              },
              "effect": { "$ref": "#/components/schemas/VendorPlantDeliveryRuleEffect" },
              "precedence": { "type": "integer", "minimum": 0, "maximum": 65535 }
            },
            "additionalProperties": false
          },
          "VendorPlantDeliveryMapping": {
            "type": "object",
            "required": [
              "mappingId",
              "vendorId",
              "plantId",
              "serviceWindow",
              "effect",
              "precedence",
              "updatedAt",
              "updatedByActorId"
            ],
            "properties": {
              "mappingId": { "type": "string", "pattern": "^map-[a-z0-9-]{3,64}$" },
              "vendorId": { "type": "string", "pattern": "^ven-[a-z0-9]{8,32}$" },
              "plantId": { "$ref": "#/components/schemas/PlantId" },
              "serviceWindow": {
                "$ref": "#/components/schemas/VendorPlantDeliveryServiceWindow"
              },
              "effect": { "$ref": "#/components/schemas/VendorPlantDeliveryRuleEffect" },
              "precedence": { "type": "integer", "minimum": 0, "maximum": 65535 },
              "updatedAt": { "$ref": "#/components/schemas/TaipeiBusinessDateTime" },
              "updatedByActorId": { "$ref": "#/components/schemas/ActorId" }
            },
            "additionalProperties": false
          },
          "VendorPlantDeliveryMappingAuditEventType": {
            "type": "string",
            "enum": ["UPSERTED", "REMOVED"]
          },
          "VendorPlantDeliveryMappingAuditEntry": {
            "type": "object",
            "required": [
              "occurredAt",
              "actorId",
              "actorRole",
              "operationId",
              "eventType",
              "mapping"
            ],
            "properties": {
              "occurredAt": { "$ref": "#/components/schemas/TaipeiBusinessDateTime" },
              "actorId": { "$ref": "#/components/schemas/ActorId" },
              "actorRole": { "$ref": "#/components/schemas/Role" },
              "operationId": { "type": "string", "minLength": 1, "maxLength": 128 },
              "eventType": {
                "$ref": "#/components/schemas/VendorPlantDeliveryMappingAuditEventType"
              },
              "mapping": { "$ref": "#/components/schemas/VendorPlantDeliveryMapping" }
            },
            "additionalProperties": false
          },
          "VendorPlantDeliveryMappingPage": {
            "type": "object",
            "required": ["items", "auditTrail", "page"],
            "properties": {
              "items": {
                "type": "array",
                "items": { "$ref": "#/components/schemas/VendorPlantDeliveryMapping" }
              },
              "auditTrail": {
                "type": "array",
                "items": { "$ref": "#/components/schemas/VendorPlantDeliveryMappingAuditEntry" }
              },
              "page": { "$ref": "#/components/schemas/PageMeta" }
            },
            "additionalProperties": false
          },
          "PageMeta": {
            "type": "object",
            "required": ["page", "pageSize", "totalItems", "totalPages"],
            "properties": {
              "page": { "type": "integer", "minimum": 1 },
              "pageSize": { "type": "integer", "minimum": 1, "maximum": 200 },
              "totalItems": { "type": "integer", "minimum": 0 },
              "totalPages": { "type": "integer", "minimum": 0 }
            },
            "additionalProperties": false
          },
          "Money": {
            "type": "object",
            "required": ["currency", "amountMinor"],
            "properties": {
              "currency": { "type": "string", "pattern": "^[A-Z]{3}$", "examples": ["TWD"] },
              "amountMinor": { "type": "integer", "minimum": 0 }
            },
            "additionalProperties": false
          },
          "MenuListItem": {
            "type": "object",
            "required": [
              "menuItemId",
              "vendorId",
              "name",
              "description",
              "menuType",
              "healthTags",
              "price",
              "remainingQuantity",
              "preorderOpen",
              "preorderOpenDaysAhead",
              "modifyCancelCutoffMinuteOfDay",
              "deliveryDate",
              "earliestDeliveryDate",
              "latestDeliveryDate",
              "cutoffDate"
            ],
            "properties": {
              "menuItemId": { "type": "string", "pattern": "^menu-[a-z0-9]{8,32}$" },
              "vendorId": { "type": "string", "pattern": "^ven-[a-z0-9]{8,32}$" },
              "name": { "type": "string", "minLength": 1, "maxLength": 80 },
              "description": { "type": "string", "maxLength": 280 },
              "menuType": { "$ref": "#/components/schemas/MenuType" },
              "healthTags": {
                "type": "array",
                "items": { "$ref": "#/components/schemas/MenuHealthTag" },
                "uniqueItems": true
              },
              "imageUrl": {
                "type": "string",
                "format": "uri",
                "maxLength": 512
              },
              "price": { "$ref": "#/components/schemas/Money" },
              "remainingQuantity": { "type": "integer", "minimum": 0, "maximum": 2000 },
              "preorderOpen": { "type": "boolean" },
              "preorderOpenDaysAhead": { "type": "integer", "minimum": 1, "maximum": 7 },
              "modifyCancelCutoffMinuteOfDay": {
                "type": "integer",
                "minimum": 900,
                "maximum": 1200
              },
              "deliveryDate": { "type": "string", "format": "date" },
              "earliestDeliveryDate": { "type": "string", "format": "date" },
              "latestDeliveryDate": { "type": "string", "format": "date" },
              "cutoffDate": { "type": "string", "format": "date" }
            },
            "additionalProperties": false
          },
          "MenuDiscoveryDay": {
            "type": "object",
            "required": ["deliveryDate", "items"],
            "properties": {
              "deliveryDate": { "type": "string", "format": "date" },
              "items": {
                "type": "array",
                "items": { "$ref": "#/components/schemas/MenuListItem" }
              }
            },
            "additionalProperties": false
          },
          "MenuPage": {
            "type": "object",
            "required": [
              "timezone",
              "view",
              "recommendationRequested",
              "recommendationApplied",
              "fromDate",
              "toDate",
              "days",
              "items",
              "page"
            ],
            "properties": {
              "timezone": {
                "type": "string",
                "enum": ["Asia/Taipei"]
              },
              "view": { "$ref": "#/components/schemas/MenuDiscoveryView" },
              "recommendationRequested": { "type": "boolean" },
              "recommendationApplied": { "type": "boolean" },
              "fromDate": { "type": "string", "format": "date" },
              "toDate": { "type": "string", "format": "date" },
              "days": {
                "type": "array",
                "items": { "$ref": "#/components/schemas/MenuDiscoveryDay" }
              },
              "items": {
                "type": "array",
                "items": { "$ref": "#/components/schemas/MenuListItem" }
              },
              "page": { "$ref": "#/components/schemas/PageMeta" }
            },
            "additionalProperties": false
          },
          "OrderLineItemRequest": {
            "type": "object",
            "required": ["menuItemId", "quantity"],
            "properties": {
              "menuItemId": { "type": "string", "pattern": "^menu-[a-z0-9]{8,32}$" },
              "quantity": { "type": "integer", "minimum": 1, "maximum": 20 },
              "specialRequests": {
                "type": "array",
                "items": { "$ref": "#/components/schemas/SpecialRequestOption" },
                "uniqueItems": true,
                "maxItems": 3
              }
            },
            "additionalProperties": false
          },
          "EmployeeOrderCreateRequest": {
            "type": "object",
            "required": ["plantId", "deliveryDate", "lineItems"],
            "properties": {
              "plantId": { "$ref": "#/components/schemas/PlantId" },
              "deliveryDate": { "type": "string", "format": "date" },
              "lineItems": {
                "type": "array",
                "items": { "$ref": "#/components/schemas/OrderLineItemRequest" },
                "minItems": 1,
                "maxItems": 10
              },
              "employeeNote": { "type": "string", "maxLength": 200 }
            },
            "additionalProperties": false
          },
          "EmployeeOrderReplaceLineItemsPatchRequest": {
            "type": "object",
            "required": ["operation", "lineItems"],
            "properties": {
              "operation": {
                "type": "string",
                "enum": ["REPLACE_LINE_ITEMS"]
              },
              "lineItems": {
                "type": "array",
                "items": { "$ref": "#/components/schemas/OrderLineItemRequest" },
                "minItems": 1,
                "maxItems": 10
              }
            },
            "additionalProperties": false
          },
          "EmployeeOrderCancelPatchRequest": {
            "type": "object",
            "required": ["operation", "cancelReason"],
            "properties": {
              "operation": {
                "type": "string",
                "enum": ["CANCEL"]
              },
              "cancelReason": {
                "type": "string",
                "minLength": 5,
                "maxLength": 200
              }
            },
            "additionalProperties": false
          },
          "EmployeeOrderPatchRequest": {
            "oneOf": [
              { "$ref": "#/components/schemas/EmployeeOrderReplaceLineItemsPatchRequest" },
              { "$ref": "#/components/schemas/EmployeeOrderCancelPatchRequest" }
            ],
            "description": "Order patch command. Supports line-item replacement and cancellation under the same cutoff governance."
          },
          "PickupVerificationRequest": {
            "type": "object",
            "required": ["verificationCode"],
            "properties": {
              "verificationCode": {
                "type": "string",
                "minLength": 15,
                "maxLength": 64,
                "pattern": "^TOTP1:[0-9]{1,20}:[0-9]{6}$",
                "description": "Single-use pickup TOTP QR payload bound to orderId and fixed Asia/Taipei 30-second step boundaries."
              }
            },
            "additionalProperties": false
          },
          "PickupVerificationResponse": {
            "type": "object",
            "required": ["orderId", "verified"],
            "properties": {
              "orderId": { "type": "string", "pattern": "^ord-[a-z0-9]{8,32}$" },
              "verified": { "type": "boolean" }
            },
            "additionalProperties": false
          },
          "OrderLineItem": {
            "type": "object",
            "required": ["menuItemId", "quantity", "pricePerUnit"],
            "properties": {
              "menuItemId": { "type": "string", "pattern": "^menu-[a-z0-9]{8,32}$" },
              "quantity": { "type": "integer", "minimum": 1, "maximum": 20 },
              "pricePerUnit": { "$ref": "#/components/schemas/Money" }
            },
            "additionalProperties": false
          },
          "EmployeeOrder": {
            "type": "object",
            "required": [
              "orderId",
              "employeeActorId",
              "plantId",
              "deliveryDate",
              "status",
              "lineItems",
              "total",
              "timeline"
            ],
            "properties": {
              "orderId": { "type": "string", "pattern": "^ord-[a-z0-9]{8,32}$" },
              "employeeActorId": { "$ref": "#/components/schemas/ActorId" },
              "plantId": { "$ref": "#/components/schemas/PlantId" },
              "deliveryDate": { "type": "string", "format": "date" },
              "status": { "$ref": "#/components/schemas/EmployeeOrderStatus" },
              "lineItems": {
                "type": "array",
                "items": { "$ref": "#/components/schemas/OrderLineItem" },
                "minItems": 1
              },
              "total": { "$ref": "#/components/schemas/Money" },
              "timeline": {
                "type": "array",
                "items": { "$ref": "#/components/schemas/OrderTimelineEvent" },
                "minItems": 1
              },
              "createdAt": { "type": "string", "format": "date-time" }
            },
            "additionalProperties": false
          },
          "VendorOrderBoardEntry": {
            "type": "object",
            "required": ["orderId", "plantId", "deliveryDate", "status", "lineItems", "timeline"],
            "properties": {
              "orderId": { "type": "string", "pattern": "^ord-[a-z0-9]{8,32}$" },
              "plantId": { "$ref": "#/components/schemas/PlantId" },
              "deliveryDate": { "type": "string", "format": "date" },
              "status": { "$ref": "#/components/schemas/EmployeeOrderStatus" },
              "lineItems": {
                "type": "array",
                "items": { "$ref": "#/components/schemas/OrderLineItem" },
                "minItems": 1
              },
              "timeline": {
                "type": "array",
                "items": { "$ref": "#/components/schemas/OrderTimelineEvent" },
                "minItems": 1
              }
            },
            "additionalProperties": false
          },
          "VendorOrderPage": {
            "type": "object",
            "required": ["items", "page"],
            "properties": {
              "items": {
                "type": "array",
                "items": { "$ref": "#/components/schemas/VendorOrderBoardEntry" }
              },
              "page": { "$ref": "#/components/schemas/PageMeta" }
            },
            "additionalProperties": false
          },
          "VendorFulfillmentDeliveryStatus": {
            "type": "string",
            "enum": [
              "PENDING_PREP",
              "PREPARING",
              "PACKED",
              "OUT_FOR_DELIVERY",
              "DELIVERED",
              "CANCELLED"
            ]
          },
          "VendorFulfillmentStatusCount": {
            "type": "object",
            "required": ["status", "count"],
            "properties": {
              "status": { "$ref": "#/components/schemas/VendorFulfillmentDeliveryStatus" },
              "count": { "type": "integer", "minimum": 0 }
            },
            "additionalProperties": false
          },
          "SpecialRequestCount": {
            "type": "object",
            "required": ["specialRequest", "count"],
            "properties": {
              "specialRequest": { "$ref": "#/components/schemas/SpecialRequestOption" },
              "count": { "type": "integer", "minimum": 0 }
            },
            "additionalProperties": false
          },
          "VendorFulfillmentOrderLineItem": {
            "type": "object",
            "required": ["menuItemId", "quantity", "specialRequests"],
            "properties": {
              "menuItemId": { "type": "string", "pattern": "^menu-[a-z0-9]{8,32}$" },
              "quantity": { "type": "integer", "minimum": 1, "maximum": 20 },
              "specialRequests": {
                "type": "array",
                "items": { "$ref": "#/components/schemas/SpecialRequestOption" },
                "uniqueItems": true,
                "maxItems": 3
              }
            },
            "additionalProperties": false
          },
          "VendorFulfillmentOrderEntry": {
            "type": "object",
            "required": [
              "orderId",
              "plantId",
              "orderStatus",
              "deliveryStatus",
              "lineItems"
            ],
            "properties": {
              "orderId": { "type": "string", "pattern": "^ord-[a-z0-9]{8,32}$" },
              "plantId": { "$ref": "#/components/schemas/PlantId" },
              "orderStatus": { "$ref": "#/components/schemas/EmployeeOrderStatus" },
              "deliveryStatus": { "$ref": "#/components/schemas/VendorFulfillmentDeliveryStatus" },
              "lineItems": {
                "type": "array",
                "items": { "$ref": "#/components/schemas/VendorFulfillmentOrderLineItem" },
                "minItems": 1
              }
            },
            "additionalProperties": false
          },
          "VendorFulfillmentPlantEntry": {
            "type": "object",
            "required": [
              "plantId",
              "orderCount",
              "portionCount",
              "deliveryStatusCounts",
              "specialRequestCounts"
            ],
            "properties": {
              "plantId": { "$ref": "#/components/schemas/PlantId" },
              "orderCount": { "type": "integer", "minimum": 0 },
              "portionCount": { "type": "integer", "minimum": 0 },
              "deliveryStatusCounts": {
                "type": "array",
                "items": { "$ref": "#/components/schemas/VendorFulfillmentStatusCount" }
              },
              "specialRequestCounts": {
                "type": "array",
                "items": { "$ref": "#/components/schemas/SpecialRequestCount" }
              }
            },
            "additionalProperties": false
          },
          "VendorFulfillmentStatusTransitionAuditEntry": {
            "type": "object",
            "required": [
              "orderId",
              "occurredAt",
              "actorId",
              "actorRole",
              "operationId",
              "fromStatus",
              "toStatus"
            ],
            "properties": {
              "orderId": { "type": "string", "pattern": "^ord-[a-z0-9]{8,32}$" },
              "occurredAt": { "$ref": "#/components/schemas/TaipeiBusinessDateTime" },
              "actorId": { "$ref": "#/components/schemas/ActorId" },
              "actorRole": { "$ref": "#/components/schemas/Role" },
              "operationId": { "type": "string", "minLength": 1, "maxLength": 128 },
              "fromStatus": { "$ref": "#/components/schemas/VendorFulfillmentDeliveryStatus" },
              "toStatus": { "$ref": "#/components/schemas/VendorFulfillmentDeliveryStatus" }
            },
            "additionalProperties": false
          },
          "VendorFulfillmentBoard": {
            "type": "object",
            "required": [
              "timezone",
              "deliveryDate",
              "generatedAt",
              "plants",
              "orders",
              "statusTransitions"
            ],
            "properties": {
              "timezone": {
                "type": "string",
                "enum": ["Asia/Taipei"]
              },
              "deliveryDate": { "type": "string", "format": "date" },
              "generatedAt": { "$ref": "#/components/schemas/TaipeiBusinessDateTime" },
              "plants": {
                "type": "array",
                "items": { "$ref": "#/components/schemas/VendorFulfillmentPlantEntry" }
              },
              "orders": {
                "type": "array",
                "items": { "$ref": "#/components/schemas/VendorFulfillmentOrderEntry" }
              },
              "statusTransitions": {
                "type": "array",
                "items": {
                  "$ref": "#/components/schemas/VendorFulfillmentStatusTransitionAuditEntry"
                }
              }
            },
            "additionalProperties": false
          },
          "VendorFulfillmentBatchCreateRequest": {
            "type": "object",
            "required": ["deliveryDate"],
            "properties": {
              "deliveryDate": { "type": "string", "format": "date" }
            },
            "additionalProperties": false
          },
          "VendorFulfillmentDailySummaryPlantRow": {
            "type": "object",
            "required": ["plantId", "orderCount", "portionCount"],
            "properties": {
              "plantId": { "$ref": "#/components/schemas/PlantId" },
              "orderCount": { "type": "integer", "minimum": 0 },
              "portionCount": { "type": "integer", "minimum": 0 }
            },
            "additionalProperties": false
          },
          "VendorFulfillmentDailySummaryExport": {
            "type": "object",
            "required": [
              "deliveryDate",
              "totalOrders",
              "totalPortions",
              "totalSpecialRequests",
              "perPlant"
            ],
            "properties": {
              "deliveryDate": { "type": "string", "format": "date" },
              "totalOrders": { "type": "integer", "minimum": 0 },
              "totalPortions": { "type": "integer", "minimum": 0 },
              "totalSpecialRequests": { "type": "integer", "minimum": 0 },
              "perPlant": {
                "type": "array",
                "items": { "$ref": "#/components/schemas/VendorFulfillmentDailySummaryPlantRow" }
              }
            },
            "additionalProperties": false
          },
          "VendorFulfillmentPlantPartitionOrderRow": {
            "type": "object",
            "required": ["orderId", "deliveryStatus", "portionCount", "specialRequests"],
            "properties": {
              "orderId": { "type": "string", "pattern": "^ord-[a-z0-9]{8,32}$" },
              "deliveryStatus": {
                "$ref": "#/components/schemas/VendorFulfillmentDeliveryStatus"
              },
              "portionCount": { "type": "integer", "minimum": 0 },
              "specialRequests": {
                "type": "array",
                "items": { "$ref": "#/components/schemas/SpecialRequestOption" },
                "uniqueItems": true
              }
            },
            "additionalProperties": false
          },
          "VendorFulfillmentPlantPartitionRow": {
            "type": "object",
            "required": [
              "plantId",
              "totalOrders",
              "totalPortions",
              "specialRequestCounts",
              "orders"
            ],
            "properties": {
              "plantId": { "$ref": "#/components/schemas/PlantId" },
              "totalOrders": { "type": "integer", "minimum": 0 },
              "totalPortions": { "type": "integer", "minimum": 0 },
              "specialRequestCounts": {
                "type": "array",
                "items": { "$ref": "#/components/schemas/SpecialRequestCount" }
              },
              "orders": {
                "type": "array",
                "items": {
                  "$ref": "#/components/schemas/VendorFulfillmentPlantPartitionOrderRow"
                }
              }
            },
            "additionalProperties": false
          },
          "VendorFulfillmentPlantPartitionSheetExport": {
            "type": "object",
            "required": ["rows"],
            "properties": {
              "rows": {
                "type": "array",
                "items": { "$ref": "#/components/schemas/VendorFulfillmentPlantPartitionRow" }
              }
            },
            "additionalProperties": false
          },
          "VendorFulfillmentLabelEntry": {
            "type": "object",
            "required": [
              "orderId",
              "plantId",
              "deliveryStatus",
              "menuItemId",
              "quantity",
              "specialRequests"
            ],
            "properties": {
              "orderId": { "type": "string", "pattern": "^ord-[a-z0-9]{8,32}$" },
              "plantId": { "$ref": "#/components/schemas/PlantId" },
              "deliveryStatus": {
                "$ref": "#/components/schemas/VendorFulfillmentDeliveryStatus"
              },
              "menuItemId": { "type": "string", "pattern": "^menu-[a-z0-9]{8,32}$" },
              "quantity": { "type": "integer", "minimum": 1, "maximum": 20 },
              "specialRequests": {
                "type": "array",
                "items": { "$ref": "#/components/schemas/SpecialRequestOption" },
                "uniqueItems": true,
                "maxItems": 3
              }
            },
            "additionalProperties": false
          },
          "VendorFulfillmentLabelSheetExport": {
            "type": "object",
            "required": ["labels"],
            "properties": {
              "labels": {
                "type": "array",
                "items": { "$ref": "#/components/schemas/VendorFulfillmentLabelEntry" }
              }
            },
            "additionalProperties": false
          },
          "VendorFulfillmentBasketEntry": {
            "type": "object",
            "required": ["basketCode", "plantId", "orderIds", "portionCount"],
            "properties": {
              "basketCode": { "type": "string", "minLength": 1, "maxLength": 64 },
              "plantId": { "$ref": "#/components/schemas/PlantId" },
              "orderIds": {
                "type": "array",
                "items": { "type": "string", "pattern": "^ord-[a-z0-9]{8,32}$" },
                "minItems": 1
              },
              "portionCount": { "type": "integer", "minimum": 0 }
            },
            "additionalProperties": false
          },
          "VendorFulfillmentBasketListExport": {
            "type": "object",
            "required": ["basketCapacityPortions", "baskets"],
            "properties": {
              "basketCapacityPortions": { "type": "integer", "minimum": 1, "maximum": 50 },
              "baskets": {
                "type": "array",
                "items": { "$ref": "#/components/schemas/VendorFulfillmentBasketEntry" }
              }
            },
            "additionalProperties": false
          },
          "VendorFulfillmentBatchArtifacts": {
            "type": "object",
            "required": ["dailySummary", "plantPartitionSheet", "labels", "basketList"],
            "properties": {
              "dailySummary": {
                "$ref": "#/components/schemas/VendorFulfillmentDailySummaryExport"
              },
              "plantPartitionSheet": {
                "$ref": "#/components/schemas/VendorFulfillmentPlantPartitionSheetExport"
              },
              "labels": { "$ref": "#/components/schemas/VendorFulfillmentLabelSheetExport" },
              "basketList": { "$ref": "#/components/schemas/VendorFulfillmentBasketListExport" }
            },
            "additionalProperties": false
          },
          "VendorFulfillmentExportBatch": {
            "type": "object",
            "required": [
              "batchId",
              "vendorId",
              "deliveryDate",
              "capturedAt",
              "generatedByActorId",
              "board",
              "artifacts"
            ],
            "properties": {
              "batchId": { "type": "string", "pattern": "^fbatch-[0-9-]{3,64}$" },
              "vendorId": { "type": "string", "pattern": "^ven-[a-z0-9]{8,32}$" },
              "deliveryDate": { "type": "string", "format": "date" },
              "capturedAt": { "$ref": "#/components/schemas/TaipeiBusinessDateTime" },
              "generatedByActorId": { "$ref": "#/components/schemas/ActorId" },
              "board": { "$ref": "#/components/schemas/VendorFulfillmentBoard" },
              "artifacts": { "$ref": "#/components/schemas/VendorFulfillmentBatchArtifacts" }
            },
            "additionalProperties": false
          },
          "VendorFulfillmentDeliveryStatusTransitionRequest": {
            "type": "object",
            "required": ["toStatus", "occurredAt"],
            "properties": {
              "toStatus": { "$ref": "#/components/schemas/VendorFulfillmentDeliveryStatus" },
              "occurredAt": { "$ref": "#/components/schemas/TaipeiBusinessDateTime" }
            },
            "additionalProperties": false
          },
          "VendorFulfillmentDeliveryStatusTransitionResult": {
            "type": "object",
            "required": ["orderId", "fromStatus", "toStatus", "occurredAt"],
            "properties": {
              "orderId": { "type": "string", "pattern": "^ord-[a-z0-9]{8,32}$" },
              "fromStatus": { "$ref": "#/components/schemas/VendorFulfillmentDeliveryStatus" },
              "toStatus": { "$ref": "#/components/schemas/VendorFulfillmentDeliveryStatus" },
              "occurredAt": { "$ref": "#/components/schemas/TaipeiBusinessDateTime" }
            },
            "additionalProperties": false
          },
          "VendorMenuItemUpsertRequest": {
            "type": "object",
            "required": [
              "name",
              "description",
              "menuType",
              "price",
              "maxDailyQuantity",
              "deliveryDate"
            ],
            "properties": {
              "name": { "type": "string", "minLength": 1, "maxLength": 80 },
              "description": { "type": "string", "minLength": 1, "maxLength": 280 },
              "menuType": { "$ref": "#/components/schemas/MenuType" },
              "imageUrl": {
                "type": "string",
                "format": "uri",
                "maxLength": 512
              },
              "price": { "$ref": "#/components/schemas/Money" },
              "maxDailyQuantity": { "type": "integer", "minimum": 1, "maximum": 2000 },
              "deliveryDate": { "type": "string", "format": "date" },
              "preorderOpenDaysAheadOverride": {
                "type": "integer",
                "minimum": 1,
                "maximum": 7,
                "description": "Optional vendor override for how many days ahead preorder stays open."
              },
              "modifyCancelCutoffMinuteOfDayOverride": {
                "type": "integer",
                "minimum": 900,
                "maximum": 1200,
                "description": "Optional vendor override minute-of-day (Asia/Taipei) for previous-day modify/cancel cutoff."
              },
              "healthTags": {
                "type": "array",
                "items": { "$ref": "#/components/schemas/MenuHealthTag" },
                "uniqueItems": true
              }
            },
            "additionalProperties": false
          },
          "VendorMenuItem": {
            "type": "object",
            "required": [
              "menuItemId",
              "vendorId",
              "name",
              "description",
              "menuType",
              "price",
              "maxDailyQuantity",
              "remainingQuantity",
              "preorderOpenDaysAhead",
              "modifyCancelCutoffMinuteOfDay",
              "deliveryDate",
              "healthTags"
            ],
            "properties": {
              "menuItemId": { "type": "string", "pattern": "^menu-[a-z0-9]{8,32}$" },
              "vendorId": { "type": "string", "pattern": "^ven-[a-z0-9]{8,32}$" },
              "name": { "type": "string", "minLength": 1, "maxLength": 80 },
              "description": { "type": "string", "minLength": 1, "maxLength": 280 },
              "menuType": { "$ref": "#/components/schemas/MenuType" },
              "imageUrl": {
                "type": "string",
                "format": "uri",
                "maxLength": 512
              },
              "price": { "$ref": "#/components/schemas/Money" },
              "maxDailyQuantity": { "type": "integer", "minimum": 1, "maximum": 2000 },
              "remainingQuantity": { "type": "integer", "minimum": 0, "maximum": 2000 },
              "preorderOpenDaysAhead": { "type": "integer", "minimum": 1, "maximum": 7 },
              "modifyCancelCutoffMinuteOfDay": {
                "type": "integer",
                "minimum": 900,
                "maximum": 1200
              },
              "deliveryDate": { "type": "string", "format": "date" },
              "healthTags": {
                "type": "array",
                "items": { "$ref": "#/components/schemas/MenuHealthTag" },
                "uniqueItems": true
              }
            },
            "additionalProperties": false
          },
          "AdminVendorReviewRequest": {
            "type": "object",
            "required": ["decision", "comment"],
            "properties": {
              "decision": { "$ref": "#/components/schemas/VendorReviewDecision" },
              "comment": {
                "type": "string",
                "minLength": 5,
                "maxLength": 280
              }
            },
            "additionalProperties": false
          },
          "VendorComplianceDocumentTemplateUpsertRequest": {
            "type": "object",
            "required": [
              "displayName",
              "required",
              "maxValidityDays",
              "reminderDaysBeforeExpiry",
              "suspensionGraceDays"
            ],
            "properties": {
              "displayName": { "type": "string", "minLength": 1, "maxLength": 120 },
              "required": { "type": "boolean" },
              "maxValidityDays": { "type": "integer", "minimum": 1, "maximum": 3650 },
              "reminderDaysBeforeExpiry": {
                "type": "array",
                "items": { "type": "integer", "minimum": 1, "maximum": 3650 },
                "uniqueItems": true
              },
              "suspensionGraceDays": { "type": "integer", "minimum": 0, "maximum": 365 }
            },
            "additionalProperties": false
          },
          "VendorComplianceDocumentTemplate": {
            "type": "object",
            "required": [
              "templateId",
              "vendorCategory",
              "displayName",
              "required",
              "maxValidityDays",
              "reminderDaysBeforeExpiry",
              "suspensionGraceDays"
            ],
            "properties": {
              "templateId": { "type": "string", "pattern": "^tmpl-[a-z0-9-]{3,64}$" },
              "vendorCategory": { "$ref": "#/components/schemas/VendorCategory" },
              "displayName": { "type": "string", "minLength": 1, "maxLength": 120 },
              "required": { "type": "boolean" },
              "maxValidityDays": { "type": "integer", "minimum": 1, "maximum": 3650 },
              "reminderDaysBeforeExpiry": {
                "type": "array",
                "items": { "type": "integer", "minimum": 1, "maximum": 3650 },
                "uniqueItems": true
              },
              "suspensionGraceDays": { "type": "integer", "minimum": 0, "maximum": 365 },
              "updatedAt": { "type": "string", "format": "date-time" }
            },
            "additionalProperties": false
          },
          "VendorComplianceDocumentTemplatePage": {
            "type": "object",
            "required": ["items", "page"],
            "properties": {
              "items": {
                "type": "array",
                "items": { "$ref": "#/components/schemas/VendorComplianceDocumentTemplate" }
              },
              "page": { "$ref": "#/components/schemas/PageMeta" }
            },
            "additionalProperties": false
          },
          "VendorComplianceDocumentRecord": {
            "type": "object",
            "required": ["templateId", "documentRef", "submittedAt", "expiresOn", "status"],
            "properties": {
              "templateId": { "type": "string", "pattern": "^tmpl-[a-z0-9-]{3,64}$" },
              "documentRef": { "type": "string", "minLength": 1, "maxLength": 128 },
              "submittedAt": { "type": "string", "format": "date-time" },
              "expiresOn": { "type": "string", "format": "date" },
              "status": { "$ref": "#/components/schemas/VendorComplianceDocumentStatus" }
            },
            "additionalProperties": false
          },
          "VendorReviewHistoryEntry": {
            "type": "object",
            "required": ["decidedAt", "decidedByActorId", "decision", "comment"],
            "properties": {
              "decidedAt": { "type": "string", "format": "date-time" },
              "decidedByActorId": { "$ref": "#/components/schemas/ActorId" },
              "decision": { "$ref": "#/components/schemas/VendorReviewDecision" },
              "comment": { "type": "string", "minLength": 5, "maxLength": 280 }
            },
            "additionalProperties": false
          },
          "VendorLifecycleEvent": {
            "type": "object",
            "required": ["occurredAt", "eventType", "actorId", "actorRole", "summary"],
            "properties": {
              "occurredAt": { "type": "string", "format": "date-time" },
              "eventType": { "$ref": "#/components/schemas/VendorLifecycleEventType" },
              "actorId": { "$ref": "#/components/schemas/ActorId" },
              "actorRole": { "$ref": "#/components/schemas/Role" },
              "summary": { "type": "string", "minLength": 1, "maxLength": 280 },
              "templateId": { "type": "string", "pattern": "^tmpl-[a-z0-9-]{3,64}$" },
              "suspensionReasonCode": {
                "$ref": "#/components/schemas/VendorSuspensionReasonCode"
              }
            },
            "additionalProperties": false
          },
          "VendorComplianceRetentionPolicy": {
            "type": "object",
            "required": [
              "reviewHistoryDays",
              "lifecycleHistoryDays",
              "rejectedVendorDeletionDays"
            ],
            "properties": {
              "reviewHistoryDays": { "type": "integer", "minimum": 1, "maximum": 36500 },
              "lifecycleHistoryDays": { "type": "integer", "minimum": 1, "maximum": 36500 },
              "rejectedVendorDeletionDays": { "type": "integer", "minimum": 1, "maximum": 3650 }
            },
            "additionalProperties": false
          },
          "VendorComplianceSummary": {
            "type": "object",
            "required": ["documents", "lifecycleHistory", "retentionPolicy"],
            "properties": {
              "documents": {
                "type": "array",
                "items": { "$ref": "#/components/schemas/VendorComplianceDocumentRecord" }
              },
              "lifecycleHistory": {
                "type": "array",
                "items": { "$ref": "#/components/schemas/VendorLifecycleEvent" }
              },
              "retentionPolicy": {
                "$ref": "#/components/schemas/VendorComplianceRetentionPolicy"
              }
            },
            "additionalProperties": false
          },
          "VendorEnrollment": {
            "type": "object",
            "required": [
              "vendorId",
              "displayName",
              "vendorCategory",
              "status",
              "reviewHistory",
              "compliance",
              "updatedAt"
            ],
            "properties": {
              "vendorId": { "type": "string", "pattern": "^ven-[a-z0-9]{8,32}$" },
              "displayName": { "type": "string", "minLength": 1, "maxLength": 120 },
              "vendorCategory": { "$ref": "#/components/schemas/VendorCategory" },
              "status": { "$ref": "#/components/schemas/VendorStatus" },
              "reviewHistory": {
                "type": "array",
                "items": { "$ref": "#/components/schemas/VendorReviewHistoryEntry" }
              },
              "compliance": { "$ref": "#/components/schemas/VendorComplianceSummary" },
              "updatedAt": { "type": "string", "format": "date-time" }
            },
            "additionalProperties": false
          },
          "VendorEnrollmentPage": {
            "type": "object",
            "required": ["items", "page"],
            "properties": {
              "items": {
                "type": "array",
                "items": { "$ref": "#/components/schemas/VendorEnrollment" }
              },
              "page": { "$ref": "#/components/schemas/PageMeta" }
            },
            "additionalProperties": false
          },
          "VendorComplianceLifecycleExecutionRequest": {
            "type": "object",
            "required": ["runDate"],
            "properties": {
              "runDate": { "type": "string", "format": "date" },
              "dryRun": { "type": "boolean", "default": false }
            },
            "additionalProperties": false
          },
          "VendorComplianceLifecycleExecutionResult": {
            "type": "object",
            "required": [
              "runDate",
              "reminderCount",
              "suspensionCount",
              "reinstatementCount"
            ],
            "properties": {
              "runDate": { "type": "string", "format": "date" },
              "reminderCount": { "type": "integer", "minimum": 0 },
              "suspensionCount": { "type": "integer", "minimum": 0 },
              "reinstatementCount": { "type": "integer", "minimum": 0 }
            },
            "additionalProperties": false
          },
          "AnomalyRuleKind": {
            "type": "string",
            "enum": [
              "EXPIRY_RISK",
              "ON_TIME_DEGRADATION",
              "SATISFACTION_DROP",
              "COMPLAINT_SPIKE"
            ]
          },
          "AnomalyThresholdComparator": {
            "type": "string",
            "enum": ["LT", "LTE", "GT", "GTE"]
          },
          "AnomalyAlertSeverity": {
            "type": "string",
            "enum": ["WARNING", "CRITICAL"]
          },
          "AnomalyAlertStatus": {
            "type": "string",
            "enum": [
              "OPEN",
              "ACKNOWLEDGED",
              "REMEDIATION_IN_PROGRESS",
              "ESCALATED",
              "CLOSED"
            ]
          },
          "AnomalySlaStatus": {
            "type": "string",
            "enum": ["ON_TRACK", "BREACHED"]
          },
          "AnomalyAlertTraceEventType": {
            "type": "string",
            "enum": [
              "TRIGGERED",
              "OWNER_ASSIGNED",
              "STATUS_TRANSITIONED",
              "CLOSED"
            ]
          },
          "AnomalyRule": {
            "type": "object",
            "required": [
              "ruleId",
              "kind",
              "displayName",
              "description",
              "governanceIssueId",
              "enabled",
              "thresholdValue",
              "thresholdComparator",
              "evaluationWindowDays",
              "slaMinutes",
              "severity"
            ],
            "properties": {
              "ruleId": { "type": "string", "pattern": "^rule-[a-z0-9-]{3,64}$" },
              "kind": { "$ref": "#/components/schemas/AnomalyRuleKind" },
              "displayName": { "type": "string", "minLength": 1, "maxLength": 280 },
              "description": { "type": "string", "minLength": 1, "maxLength": 280 },
              "governanceIssueId": { "type": "string", "minLength": 1, "maxLength": 280 },
              "enabled": { "type": "boolean" },
              "thresholdValue": { "type": "number", "minimum": 0 },
              "thresholdComparator": { "$ref": "#/components/schemas/AnomalyThresholdComparator" },
              "evaluationWindowDays": { "type": "integer", "minimum": 1, "maximum": 3650 },
              "slaMinutes": { "type": "integer", "minimum": 1 },
              "severity": { "$ref": "#/components/schemas/AnomalyAlertSeverity" }
            },
            "additionalProperties": false
          },
          "AnomalyRuleListResponse": {
            "type": "object",
            "required": ["items"],
            "properties": {
              "items": {
                "type": "array",
                "items": { "$ref": "#/components/schemas/AnomalyRule" }
              }
            },
            "additionalProperties": false
          },
          "AnomalyRuleUpsertRequest": {
            "type": "object",
            "required": [
              "kind",
              "displayName",
              "description",
              "governanceIssueId",
              "enabled",
              "thresholdValue",
              "thresholdComparator",
              "evaluationWindowDays",
              "slaMinutes",
              "severity"
            ],
            "properties": {
              "kind": { "$ref": "#/components/schemas/AnomalyRuleKind" },
              "displayName": { "type": "string", "minLength": 1, "maxLength": 280 },
              "description": { "type": "string", "minLength": 1, "maxLength": 280 },
              "governanceIssueId": { "type": "string", "minLength": 1, "maxLength": 280 },
              "enabled": { "type": "boolean" },
              "thresholdValue": { "type": "number", "minimum": 0 },
              "thresholdComparator": { "$ref": "#/components/schemas/AnomalyThresholdComparator" },
              "evaluationWindowDays": { "type": "integer", "minimum": 1, "maximum": 3650 },
              "slaMinutes": { "type": "integer", "minimum": 1 },
              "severity": { "$ref": "#/components/schemas/AnomalyAlertSeverity" }
            },
            "additionalProperties": false
          },
          "AnomalyAlertEvaluationRequest": {
            "type": "object",
            "required": ["vendorId"],
            "properties": {
              "vendorId": { "type": "string", "pattern": "^ven-[a-z0-9]{8,32}$" },
              "observedAtEpochDay": { "type": "integer" },
              "observedAtMinuteOfDay": { "type": "integer", "minimum": 0, "maximum": 1439 },
              "daysUntilExpiry": { "type": "number", "minimum": 0 },
              "onTimeRate": { "type": "number", "minimum": 0, "maximum": 1 },
              "satisfactionScore": { "type": "number", "minimum": 0, "maximum": 5 },
              "complaintCount": { "type": "number", "minimum": 0 },
              "defaultOwnerActorId": { "$ref": "#/components/schemas/ActorId" }
            },
            "additionalProperties": false
          },
          "AnomalyAlertTraceEvent": {
            "type": "object",
            "required": ["occurredAt", "actorId", "eventType", "status"],
            "properties": {
              "occurredAt": { "$ref": "#/components/schemas/TaipeiBusinessDateTime" },
              "actorId": { "$ref": "#/components/schemas/ActorId" },
              "eventType": { "$ref": "#/components/schemas/AnomalyAlertTraceEventType" },
              "status": { "$ref": "#/components/schemas/AnomalyAlertStatus" },
              "note": {
                "type": "string",
                "minLength": 1,
                "maxLength": 280,
                "pattern": ".*\\S.*"
              }
            },
            "additionalProperties": false
          },
          "AnomalyAlert": {
            "type": "object",
            "required": [
              "alertId",
              "vendorId",
              "ruleId",
              "ruleKind",
              "ruleDisplayName",
              "governanceIssueId",
              "status",
              "ownerActorId",
              "severity",
              "observedValue",
              "thresholdValue",
              "thresholdComparator",
              "observedAt",
              "openedAt",
              "updatedAt",
              "slaDueAt",
              "slaStatus",
              "closureEvidenceRefs",
              "trace"
            ],
            "properties": {
              "alertId": { "type": "string", "pattern": "^alt-[0-9a-f]{16}$" },
              "vendorId": { "type": "string", "pattern": "^ven-[a-z0-9]{8,32}$" },
              "ruleId": { "type": "string", "pattern": "^rule-[a-z0-9-]{3,64}$" },
              "ruleKind": { "$ref": "#/components/schemas/AnomalyRuleKind" },
              "ruleDisplayName": { "type": "string", "minLength": 1, "maxLength": 280 },
              "governanceIssueId": { "type": "string", "minLength": 1, "maxLength": 280 },
              "status": { "$ref": "#/components/schemas/AnomalyAlertStatus" },
              "ownerActorId": { "$ref": "#/components/schemas/ActorId" },
              "severity": { "$ref": "#/components/schemas/AnomalyAlertSeverity" },
              "observedValue": { "type": "number" },
              "thresholdValue": { "type": "number", "minimum": 0 },
              "thresholdComparator": { "$ref": "#/components/schemas/AnomalyThresholdComparator" },
              "observedAt": { "$ref": "#/components/schemas/TaipeiBusinessDateTime" },
              "openedAt": { "$ref": "#/components/schemas/TaipeiBusinessDateTime" },
              "updatedAt": { "$ref": "#/components/schemas/TaipeiBusinessDateTime" },
              "slaDueAt": { "$ref": "#/components/schemas/TaipeiBusinessDateTime" },
              "slaStatus": { "$ref": "#/components/schemas/AnomalySlaStatus" },
              "escalatedAt": { "$ref": "#/components/schemas/TaipeiBusinessDateTime" },
              "closedAt": { "$ref": "#/components/schemas/TaipeiBusinessDateTime" },
              "closureNote": {
                "type": "string",
                "minLength": 1,
                "maxLength": 280,
                "pattern": ".*\\S.*"
              },
              "closureEvidenceRefs": {
                "type": "array",
                "items": {
                  "type": "string",
                  "minLength": 1,
                  "maxLength": 280,
                  "pattern": ".*\\S.*"
                }
              },
              "ticketReference": { "type": "string", "maxLength": 128 },
              "trace": {
                "type": "array",
                "items": { "$ref": "#/components/schemas/AnomalyAlertTraceEvent" },
                "minItems": 1
              }
            },
            "additionalProperties": false
          },
          "AnomalyAlertEvaluationResponse": {
            "type": "object",
            "required": ["triggeredAlerts"],
            "properties": {
              "triggeredAlerts": {
                "type": "array",
                "items": { "$ref": "#/components/schemas/AnomalyAlert" }
              }
            },
            "additionalProperties": false
          },
          "AnomalyAlertListResponse": {
            "type": "object",
            "required": ["items"],
            "properties": {
              "items": {
                "type": "array",
                "items": { "$ref": "#/components/schemas/AnomalyAlert" }
              }
            },
            "additionalProperties": false
          },
          "AdminAnomalyAlertAssignOwnerPatchRequest": {
            "type": "object",
            "required": ["operation", "ownerActorId"],
            "properties": {
              "operation": { "type": "string", "enum": ["ASSIGN_OWNER"] },
              "ownerActorId": { "$ref": "#/components/schemas/ActorId" },
              "note": {
                "type": "string",
                "minLength": 1,
                "maxLength": 280,
                "pattern": ".*\\S.*"
              }
            },
            "additionalProperties": false
          },
          "AdminAnomalyAlertAcknowledgePatchRequest": {
            "type": "object",
            "required": ["operation"],
            "properties": {
              "operation": { "type": "string", "enum": ["ACKNOWLEDGE"] },
              "note": {
                "type": "string",
                "minLength": 1,
                "maxLength": 280,
                "pattern": ".*\\S.*"
              }
            },
            "additionalProperties": false
          },
          "AdminAnomalyAlertStartRemediationPatchRequest": {
            "type": "object",
            "required": ["operation"],
            "properties": {
              "operation": { "type": "string", "enum": ["START_REMEDIATION"] },
              "note": {
                "type": "string",
                "minLength": 1,
                "maxLength": 280,
                "pattern": ".*\\S.*"
              }
            },
            "additionalProperties": false
          },
          "AdminAnomalyAlertEscalatePatchRequest": {
            "type": "object",
            "required": ["operation"],
            "properties": {
              "operation": { "type": "string", "enum": ["ESCALATE"] },
              "note": {
                "type": "string",
                "minLength": 1,
                "maxLength": 280,
                "pattern": ".*\\S.*"
              }
            },
            "additionalProperties": false
          },
          "AdminAnomalyAlertClosePatchRequest": {
            "type": "object",
            "required": ["operation", "closureNote", "closureEvidenceRefs"],
            "properties": {
              "operation": { "type": "string", "enum": ["CLOSE"] },
              "note": {
                "type": "string",
                "minLength": 1,
                "maxLength": 280,
                "pattern": ".*\\S.*"
              },
              "closureNote": {
                "type": "string",
                "minLength": 1,
                "maxLength": 280,
                "pattern": ".*\\S.*"
              },
              "closureEvidenceRefs": {
                "type": "array",
                "items": {
                  "type": "string",
                  "minLength": 1,
                  "maxLength": 280,
                  "pattern": ".*\\S.*"
                },
                "minItems": 1
              },
              "ticketReference": { "type": "string", "maxLength": 128 }
            },
            "additionalProperties": false
          },
          "AdminAnomalyAlertPatchRequest": {
            "oneOf": [
              { "$ref": "#/components/schemas/AdminAnomalyAlertAssignOwnerPatchRequest" },
              { "$ref": "#/components/schemas/AdminAnomalyAlertAcknowledgePatchRequest" },
              { "$ref": "#/components/schemas/AdminAnomalyAlertStartRemediationPatchRequest" },
              { "$ref": "#/components/schemas/AdminAnomalyAlertEscalatePatchRequest" },
              { "$ref": "#/components/schemas/AdminAnomalyAlertClosePatchRequest" }
            ],
            "description": "Governed anomaly alert lifecycle command."
          },
          "PayrollLedgerEntryKind": {
            "type": "string",
            "enum": [
              "DEDUCTION",
              "ADJUSTMENT_DEBIT",
              "ADJUSTMENT_CREDIT",
              "REFUND"
            ]
          },
          "PayrollLedgerSourceKind": {
            "type": "string",
            "enum": [
              "ORDER_MUTATION",
              "DISPUTE_WORKFLOW",
              "SFTP_BATCH_EXPORT",
              "HR_API_SYNC_ADJUNCT"
            ]
          },
          "PayrollDisputeStatus": {
            "type": "string",
            "enum": [
              "OPEN",
              "IN_REVIEW",
              "RESOLVED_REFUND_APPROVED",
              "RESOLVED_REJECTED"
            ]
          },
          "PayrollDisputeTraceEventType": {
            "type": "string",
            "enum": [
              "OPENED",
              "OWNER_ASSIGNED",
              "RESOLVED_REFUND_APPROVED",
              "RESOLVED_REJECTED"
            ]
          },
          "PayrollLedgerEntry": {
            "type": "object",
            "required": [
              "ledgerEntryId",
              "kind",
              "amount",
              "occurredAt",
              "sourceEventKind",
              "sourceEventReference"
            ],
            "properties": {
              "ledgerEntryId": { "type": "integer", "minimum": 1 },
              "kind": { "$ref": "#/components/schemas/PayrollLedgerEntryKind" },
              "amount": { "$ref": "#/components/schemas/Money" },
              "occurredAt": { "$ref": "#/components/schemas/TaipeiBusinessDateTime" },
              "sourceEventKind": { "$ref": "#/components/schemas/PayrollLedgerSourceKind" },
              "sourceEventReference": { "type": "string", "minLength": 1, "maxLength": 128 }
            },
            "additionalProperties": false
          },
          "PayrollDisputeTraceEvent": {
            "type": "object",
            "required": [
              "occurredAt",
              "actorId",
              "eventType",
              "status",
              "ownerActorId",
              "sourceEventKind",
              "sourceEventReference"
            ],
            "properties": {
              "occurredAt": { "$ref": "#/components/schemas/TaipeiBusinessDateTime" },
              "actorId": { "$ref": "#/components/schemas/ActorId" },
              "eventType": { "$ref": "#/components/schemas/PayrollDisputeTraceEventType" },
              "status": { "$ref": "#/components/schemas/PayrollDisputeStatus" },
              "ownerActorId": { "$ref": "#/components/schemas/ActorId" },
              "note": { "type": "string", "maxLength": 280 },
              "sourceEventKind": { "$ref": "#/components/schemas/PayrollLedgerSourceKind" },
              "sourceEventReference": { "type": "string", "minLength": 1, "maxLength": 128 },
              "refundLedgerEntryId": { "type": "integer", "minimum": 1 }
            },
            "additionalProperties": false
          },
          "PayrollDispute": {
            "type": "object",
            "required": [
              "disputeId",
              "orderId",
              "employeeActorId",
              "ownerActorId",
              "status",
              "openedAt",
              "updatedAt",
              "trace"
            ],
            "properties": {
              "disputeId": { "type": "string", "pattern": "^dsp-[0-9a-f]{16}$" },
              "orderId": { "type": "string", "pattern": "^ord-[a-z0-9]{8,32}$" },
              "employeeActorId": { "$ref": "#/components/schemas/ActorId" },
              "ownerActorId": { "$ref": "#/components/schemas/ActorId" },
              "status": { "$ref": "#/components/schemas/PayrollDisputeStatus" },
              "openedAt": { "$ref": "#/components/schemas/TaipeiBusinessDateTime" },
              "updatedAt": { "$ref": "#/components/schemas/TaipeiBusinessDateTime" },
              "trace": {
                "type": "array",
                "items": { "$ref": "#/components/schemas/PayrollDisputeTraceEvent" },
                "minItems": 1
              }
            },
            "additionalProperties": false
          },
          "EmployeeOrderPayrollLedger": {
            "type": "object",
            "required": [
              "orderId",
              "employeeActorId",
              "deliveryDate",
              "currency",
              "netAmountMinor",
              "ledgerEntries",
              "disputes"
            ],
            "properties": {
              "orderId": { "type": "string", "pattern": "^ord-[a-z0-9]{8,32}$" },
              "employeeActorId": { "$ref": "#/components/schemas/ActorId" },
              "deliveryDate": { "type": "string", "format": "date" },
              "currency": { "type": "string", "pattern": "^[A-Z]{3}$" },
              "netAmountMinor": { "type": "integer" },
              "ledgerEntries": {
                "type": "array",
                "items": { "$ref": "#/components/schemas/PayrollLedgerEntry" }
              },
              "disputes": {
                "type": "array",
                "items": { "$ref": "#/components/schemas/PayrollDispute" }
              }
            },
            "additionalProperties": false
          },
          "EmployeePayrollDisputeCreateRequest": {
            "type": "object",
            "required": ["reason"],
            "properties": {
              "reason": { "type": "string", "minLength": 1, "maxLength": 280 }
            },
            "additionalProperties": false
          },
          "AdminPayrollDisputeAssignOwnerPatchRequest": {
            "type": "object",
            "required": ["operation", "ownerActorId"],
            "properties": {
              "operation": { "type": "string", "enum": ["ASSIGN_OWNER"] },
              "ownerActorId": { "$ref": "#/components/schemas/ActorId" },
              "note": { "type": "string", "maxLength": 280 }
            },
            "additionalProperties": false
          },
          "AdminPayrollDisputeResolveRefundPatchRequest": {
            "type": "object",
            "required": ["operation", "note"],
            "properties": {
              "operation": { "type": "string", "enum": ["RESOLVE_REFUND"] },
              "note": { "type": "string", "minLength": 1, "maxLength": 280 },
              "refundAmountMinor": { "type": "integer", "minimum": 1 }
            },
            "additionalProperties": false
          },
          "AdminPayrollDisputeResolveRejectedPatchRequest": {
            "type": "object",
            "required": ["operation", "note"],
            "properties": {
              "operation": { "type": "string", "enum": ["RESOLVE_REJECTED"] },
              "note": { "type": "string", "minLength": 1, "maxLength": 280 }
            },
            "additionalProperties": false
          },
          "AdminPayrollDisputePatchRequest": {
            "oneOf": [
              { "$ref": "#/components/schemas/AdminPayrollDisputeAssignOwnerPatchRequest" },
              { "$ref": "#/components/schemas/AdminPayrollDisputeResolveRefundPatchRequest" },
              { "$ref": "#/components/schemas/AdminPayrollDisputeResolveRejectedPatchRequest" }
            ],
            "description": "Immutable dispute workflow command."
          },
          "PayrollRetentionPurgeRequest": {
            "type": "object",
            "properties": {
              "asOfEpochDay": { "type": "integer" }
            },
            "additionalProperties": false
          },
          "PayrollRetentionPurgeResponse": {
            "type": "object",
            "required": [
              "purgedLedgerEntries",
              "purgedDisputes",
              "purgedExchangeBatches",
              "asOfEpochDay"
            ],
            "properties": {
              "purgedLedgerEntries": { "type": "integer", "minimum": 0 },
              "purgedDisputes": { "type": "integer", "minimum": 0 },
              "purgedExchangeBatches": { "type": "integer", "minimum": 0 },
              "asOfEpochDay": { "type": "integer" }
            },
            "additionalProperties": false
          },
          "PayrollMonthlySettlementCloseRequest": {
            "type": "object",
            "properties": {
              "cycleKey": {
                "type": "string",
                "pattern": "^[A-Za-z0-9._-]{1,64}$"
              },
              "page": { "type": "integer", "minimum": 1 },
              "pageSize": { "type": "integer", "minimum": 1, "maximum": 200 },
              "sortBy": { "$ref": "#/components/schemas/PayrollSortField" },
              "sortOrder": { "$ref": "#/components/schemas/SortOrder" }
            },
            "additionalProperties": false
          },
          "PayrollSettlementCycleLockState": {
            "type": "string",
            "enum": ["LOCKED", "UNLOCKED"]
          },
          "PayrollSettlementCycleLockRequest": {
            "type": "object",
            "required": ["reason"],
            "properties": {
              "reason": { "type": "string", "minLength": 1, "maxLength": 280 }
            },
            "additionalProperties": false
          },
          "PayrollSettlementCycleLock": {
            "type": "object",
            "required": [
              "cycleKey",
              "payPeriod",
              "lockState",
              "batchId",
              "snapshotChecksum",
              "reason",
              "changedAt",
              "actorId"
            ],
            "properties": {
              "cycleKey": {
                "type": "string",
                "pattern": "^[A-Za-z0-9._-]{1,64}$"
              },
              "payPeriod": { "type": "string", "pattern": "^[0-9]{4}-[0-9]{2}$" },
              "lockState": { "$ref": "#/components/schemas/PayrollSettlementCycleLockState" },
              "batchId": { "type": "string", "pattern": "^sftp-[0-9]{6}-[0-9a-f]{16}$" },
              "snapshotChecksum": { "type": "string", "pattern": "^[0-9a-f]{64}$" },
              "reason": { "type": "string", "minLength": 1, "maxLength": 280 },
              "changedAt": { "$ref": "#/components/schemas/TaipeiBusinessDateTime" },
              "actorId": { "$ref": "#/components/schemas/ActorId" }
            },
            "additionalProperties": false
          },
          "PayrollSettlementCycleLockResponse": {
            "type": "object",
            "required": ["settlementCycle"],
            "properties": {
              "settlementCycle": { "$ref": "#/components/schemas/PayrollSettlementCycleLock" }
            },
            "additionalProperties": false
          },
          "PayrollReconciliation": {
            "type": "object",
            "required": [
              "totalRecords",
              "totalAmountMinor",
              "totalSourceEntries",
              "readyRecords",
              "lockedRecords",
              "refundedRecords",
              "disputedRecords",
              "deductionFailedRecords",
              "employeeTerminatedRecords",
              "requiredExceptionClasses",
              "presentExceptionClasses"
            ],
            "properties": {
              "totalRecords": { "type": "integer", "minimum": 0 },
              "totalAmountMinor": { "type": "integer", "minimum": 0 },
              "totalSourceEntries": { "type": "integer", "minimum": 0 },
              "readyRecords": { "type": "integer", "minimum": 0 },
              "lockedRecords": { "type": "integer", "minimum": 0 },
              "refundedRecords": { "type": "integer", "minimum": 0 },
              "disputedRecords": { "type": "integer", "minimum": 0 },
              "deductionFailedRecords": { "type": "integer", "minimum": 0 },
              "employeeTerminatedRecords": { "type": "integer", "minimum": 0 },
              "requiredExceptionClasses": {
                "type": "array",
                "items": { "$ref": "#/components/schemas/PayrollExceptionClass" }
              },
              "presentExceptionClasses": {
                "type": "array",
                "items": { "$ref": "#/components/schemas/PayrollExceptionClass" }
              }
            },
            "additionalProperties": false
          },
          "PayrollExchangeBatch": {
            "type": "object",
            "required": [
              "batchId",
              "payPeriod",
              "cycleKey",
              "generatedAt",
              "cycleStartDate",
              "cycleEndDate",
              "snapshotChecksum",
              "reconciliation",
              "exchangePath",
              "hrApiSyncStatus"
            ],
            "properties": {
              "batchId": { "type": "string", "pattern": "^sftp-[0-9]{6}-[0-9a-f]{16}$" },
              "payPeriod": { "type": "string", "pattern": "^[0-9]{4}-[0-9]{2}$" },
              "cycleKey": {
                "type": "string",
                "pattern": "^[A-Za-z0-9._-]{1,64}$"
              },
              "generatedAt": { "$ref": "#/components/schemas/TaipeiBusinessDateTime" },
              "cycleStartDate": { "type": "string", "format": "date" },
              "cycleEndDate": { "type": "string", "format": "date" },
              "snapshotChecksum": {
                "type": "string",
                "pattern": "^[0-9a-f]{64}$"
              },
              "reconciliation": { "$ref": "#/components/schemas/PayrollReconciliation" },
              "exchangePath": { "type": "string", "enum": ["SFTP_BATCH"] },
              "hrApiSyncStatus": {
                "type": "string",
                "enum": ["NOT_SYNCED", "SUCCEEDED", "FAILED"]
              },
              "hrApiSyncedAt": { "$ref": "#/components/schemas/TaipeiBusinessDateTime" }
            },
            "additionalProperties": false
          },
          "PayrollHrApiSyncOutcome": {
            "type": "string",
            "enum": ["SUCCEEDED", "FAILED"]
          },
          "PayrollHrApiSyncRequest": {
            "type": "object",
            "required": ["outcome"],
            "properties": {
              "outcome": { "$ref": "#/components/schemas/PayrollHrApiSyncOutcome" },
              "note": { "type": "string", "minLength": 1, "maxLength": 280 }
            },
            "additionalProperties": false
          },
          "PayrollHrApiSyncResponse": {
            "type": "object",
            "required": ["exchangeBatch"],
            "properties": {
              "exchangeBatch": { "$ref": "#/components/schemas/PayrollExchangeBatch" }
            },
            "additionalProperties": false
          },
          "PayrollDeductionStatus": {
            "type": "string",
            "enum": [
              "READY",
              "LOCKED",
              "REFUNDED",
              "DISPUTED",
              "DEDUCTION_FAILED",
              "EMPLOYEE_TERMINATED"
            ]
          },
          "PayrollExceptionClass": {
            "type": "string",
            "enum": [
              "DISPUTED",
              "DEDUCTION_FAILED",
              "EMPLOYEE_TERMINATED",
              "REFUNDED"
            ]
          },
          "PayrollDeductionRecord": {
            "type": "object",
            "required": [
              "employeeActorCiphertext",
              "orderIdCiphertext",
              "deliveryDate",
              "amountCiphertext",
              "payPeriod",
              "status",
              "sourceEntryIds"
            ],
            "properties": {
              "employeeActorCiphertext": {
                "type": "string",
                "minLength": 1,
                "description": "AES-GCM encrypted employee actor identifier envelope (`v1:nonce:ciphertext`) for payroll privacy controls."
              },
              "orderIdCiphertext": {
                "type": "string",
                "minLength": 1,
                "description": "AES-GCM encrypted order identifier envelope (`v1:nonce:ciphertext`) for payroll privacy controls."
              },
              "deliveryDate": { "type": "string", "format": "date" },
              "amountCiphertext": {
                "type": "string",
                "minLength": 1,
                "description": "AES-GCM encrypted serialized money payload envelope (`v1:nonce:ciphertext`)."
              },
              "payPeriod": { "type": "string", "pattern": "^[0-9]{4}-[0-9]{2}$" },
              "status": { "$ref": "#/components/schemas/PayrollDeductionStatus" },
              "disputeStatus": { "$ref": "#/components/schemas/PayrollDisputeStatus" },
              "sourceEntryIds": {
                "type": "array",
                "items": { "type": "integer", "minimum": 1 }
              }
            },
            "additionalProperties": false
          },
          "PayrollDeductionPage": {
            "type": "object",
            "required": ["items", "page", "exchangeBatch"],
            "properties": {
              "items": {
                "type": "array",
                "items": { "$ref": "#/components/schemas/PayrollDeductionRecord" }
              },
              "page": { "$ref": "#/components/schemas/PageMeta" },
              "exchangeBatch": { "$ref": "#/components/schemas/PayrollExchangeBatch" }
            },
            "additionalProperties": false
          },
          "ErrorCode": {
            "type": "string",
            "enum": [
              "BAD_REQUEST",
              "UNAUTHORIZED",
              "FORBIDDEN",
              "NOT_FOUND",
              "CONFLICT",
              "VALIDATION_FAILED",
              "INVALID_ORDER_REQUEST",
              "UNSUPPORTED_VENDOR_ID",
              "TIME_RESOLUTION_FAILED",
              "ORDER_ID_GENERATION_FAILED",
              "ORDER_VENDOR_DELIVERY_REJECTED",
              "ORDER_POLICY_VIOLATION",
              "ORDER_MUTATION_NOT_ALLOWED",
              "INVALID_ORDER_UPDATE_REQUEST",
              "UNSUPPORTED_PLANT_ID",
              "INVALID_MENU_DISCOVERY_QUERY",
              "MENU_DISCOVERY_INTERNAL_ERROR",
              "INVALID_PICKUP_VERIFICATION_REQUEST",
              "PICKUP_VERIFICATION_REPLAYED",
              "PICKUP_VERIFICATION_STATE_CONFLICT",
              "PICKUP_VERIFICATION_EXPIRED",
              "PICKUP_VERIFICATION_INVALID_WINDOW",
              "PICKUP_VERIFICATION_INVALID_CODE",
              "PICKUP_VERIFICATION_INTERNAL_ERROR",
              "ORDER_NOT_FOUND",
              "INVALID_AUDIT_INVESTIGATION_QUERY",
              "AUDIT_INVESTIGATION_INTERNAL_ERROR",
              "AUDIT_RETENTION_PURGE_INTERNAL_ERROR",
              "ORDER_RETENTION_PURGE_INTERNAL_ERROR",
              "PAYROLL_LEDGER_INTERNAL_ERROR",
              "ANOMALY_ALERT_INTERNAL_ERROR",
              "INVALID_MCP_TOOL_NAME",
              "MCP_TOOL_NOT_FOUND",
              "INVALID_MCP_TOOL_ARGUMENTS",
              "MCP_OAUTH_CONFIGURATION_ERROR",
              "MCP_AUTHORIZATION_AUDIT_INTERNAL_ERROR",
              "VENDOR_FULFILLMENT_INVALID_REQUEST",
              "VENDOR_FULFILLMENT_STATUS_CONFLICT",
              "VENDOR_FULFILLMENT_BATCH_NOT_FOUND"
            ]
          },
          "ErrorDetail": {
            "type": "object",
            "required": ["field", "reason"],
            "properties": {
              "field": { "type": "string", "minLength": 1, "maxLength": 128 },
              "reason": { "type": "string", "minLength": 1, "maxLength": 280 }
            },
            "additionalProperties": false
          },
          "ErrorResponse": {
            "type": "object",
            "required": ["code", "message", "requestId"],
            "properties": {
              "code": { "$ref": "#/components/schemas/ErrorCode" },
              "message": { "type": "string", "minLength": 1, "maxLength": 280 },
              "requestId": { "type": "string", "minLength": 1, "maxLength": 128 },
              "details": {
                "type": "array",
                "items": { "$ref": "#/components/schemas/ErrorDetail" }
              }
            },
            "additionalProperties": false
          }
        }
      }
    })
}

pub fn canonical_openapi_json() -> Result<String, serde_json::Error> {
    serde_json::to_string_pretty(&canonical_openapi_spec())
}

pub fn canonical_openapi_yaml() -> Result<String, serde_yaml::Error> {
    serde_yaml::to_string(&canonical_openapi_spec())
}

pub fn render_redoc_html(spec_file_name: &str) -> String {
    format!(
        "<!doctype html>\n\
<html lang=\"en\">\n\
  <head>\n\
    <meta charset=\"utf-8\" />\n\
    <meta name=\"viewport\" content=\"width=device-width, initial-scale=1\" />\n\
    <title>Corporate Catering API Contract</title>\n\
    <style>\n\
      body {{ margin: 0; padding: 0; }}\n\
    </style>\n\
  </head>\n\
  <body>\n\
    <redoc spec-url=\"./{spec_file_name}\"></redoc>\n\
    <script src=\"https://cdn.redoc.ly/redoc/latest/bundles/redoc.standalone.js\"></script>\n\
  </body>\n\
</html>\n"
    )
}

pub fn write_openapi_artifacts(
    output_dir: &Path,
) -> Result<OpenApiArtifactPaths, OpenApiContractError> {
    fs::create_dir_all(output_dir)?;

    let openapi_json = output_dir.join("openapi.json");
    let openapi_yaml = output_dir.join("openapi.yaml");
    let docs_html = output_dir.join("index.html");

    fs::write(&openapi_json, canonical_openapi_json()?)?;
    fs::write(&openapi_yaml, canonical_openapi_yaml()?)?;
    fs::write(&docs_html, render_redoc_html("openapi.json"))?;

    Ok(OpenApiArtifactPaths {
        openapi_json,
        openapi_yaml,
        docs_html,
    })
}
