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
}

impl HttpMethod {
    pub const fn as_openapi_verb(self) -> &'static str {
        match self {
            Self::Get => "get",
            Self::Post => "post",
            Self::Patch => "patch",
            Self::Put => "put",
        }
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum HttpOperation {
    ListEmployeeMenus,
    CreateEmployeeOrder,
    UpdateEmployeeOrder,
    ListVendorOrders,
    UpsertVendorMenuItem,
    ListAdminVendors,
    ApproveVendorEnrollment,
    ExportPayrollDeductions,
}

impl HttpOperation {
    pub const ALL: [Self; 8] = [
        Self::ListEmployeeMenus,
        Self::CreateEmployeeOrder,
        Self::UpdateEmployeeOrder,
        Self::ListVendorOrders,
        Self::UpsertVendorMenuItem,
        Self::ListAdminVendors,
        Self::ApproveVendorEnrollment,
        Self::ExportPayrollDeductions,
    ];

    pub const fn operation_id(self) -> &'static str {
        match self {
            Self::ListEmployeeMenus => "listEmployeeMenus",
            Self::CreateEmployeeOrder => "createEmployeeOrder",
            Self::UpdateEmployeeOrder => "updateEmployeeOrder",
            Self::ListVendorOrders => "listVendorOrders",
            Self::UpsertVendorMenuItem => "upsertVendorMenuItem",
            Self::ListAdminVendors => "listAdminVendors",
            Self::ApproveVendorEnrollment => "approveVendorEnrollment",
            Self::ExportPayrollDeductions => "exportPayrollDeductions",
        }
    }

    pub const fn method(self) -> HttpMethod {
        match self {
            Self::ListEmployeeMenus
            | Self::ListVendorOrders
            | Self::ListAdminVendors
            | Self::ExportPayrollDeductions => HttpMethod::Get,
            Self::CreateEmployeeOrder | Self::ApproveVendorEnrollment => HttpMethod::Post,
            Self::UpdateEmployeeOrder => HttpMethod::Patch,
            Self::UpsertVendorMenuItem => HttpMethod::Put,
        }
    }

    pub const fn path(self) -> &'static str {
        match self {
            Self::ListEmployeeMenus => "/api/v1/employee/menus",
            Self::CreateEmployeeOrder => "/api/v1/employee/orders",
            Self::UpdateEmployeeOrder => "/api/v1/employee/orders/{orderId}",
            Self::ListVendorOrders => "/api/v1/vendor/orders",
            Self::UpsertVendorMenuItem => "/api/v1/vendor/menu-items/{menuItemId}",
            Self::ListAdminVendors => "/api/v1/admin/vendors",
            Self::ApproveVendorEnrollment => "/api/v1/admin/vendors/{vendorId}/approvals",
            Self::ExportPayrollDeductions => "/api/v1/integrations/payroll/deductions",
        }
    }

    pub const fn audience(self) -> HttpAudience {
        match self {
            Self::ListEmployeeMenus | Self::CreateEmployeeOrder | Self::UpdateEmployeeOrder => {
                HttpAudience::Employee
            }
            Self::ListVendorOrders | Self::UpsertVendorMenuItem => HttpAudience::Vendor,
            Self::ListAdminVendors | Self::ApproveVendorEnrollment => HttpAudience::Admin,
            Self::ExportPayrollDeductions => HttpAudience::Integration,
        }
    }

    pub const fn write_action(self) -> Option<Action> {
        match self {
            Self::CreateEmployeeOrder | Self::UpdateEmployeeOrder => {
                Some(Action::PlaceEmployeeOrder)
            }
            Self::UpsertVendorMenuItem => Some(Action::ManageVendorMenu),
            Self::ApproveVendorEnrollment => Some(Action::ApproveVendorEnrollment),
            Self::ListEmployeeMenus
            | Self::ListVendorOrders
            | Self::ListAdminVendors
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
            "listVendorOrders" => Some(Self::ListVendorOrders),
            "upsertVendorMenuItem" => Some(Self::UpsertVendorMenuItem),
            "listAdminVendors" => Some(Self::ListAdminVendors),
            "approveVendorEnrollment" => Some(Self::ApproveVendorEnrollment),
            "exportPayrollDeductions" => Some(Self::ExportPayrollDeductions),
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
            "summary": "List available menus",
            "operationId": HttpOperation::ListEmployeeMenus.operation_id(),
            "security": [{ "corporateSsoBearer": [] }],
            "parameters": [
              { "$ref": "#/components/parameters/PlantIdQuery" },
              { "$ref": "#/components/parameters/MenuDateQuery" },
              { "$ref": "#/components/parameters/PageQuery" },
              { "$ref": "#/components/parameters/PageSizeQuery" },
              { "$ref": "#/components/parameters/MenuSortByQuery" },
              { "$ref": "#/components/parameters/SortOrderQuery" },
              { "$ref": "#/components/parameters/CuisineFilterQuery" },
              { "$ref": "#/components/parameters/HealthTagFilterQuery" }
            ],
            "responses": {
              "200": {
                "description": "Paginated menu list",
                "content": {
                  "application/json": {
                    "schema": { "$ref": "#/components/schemas/MenuPage" }
                  }
                }
              },
              "400": { "$ref": "#/components/responses/BadRequest" },
              "401": { "$ref": "#/components/responses/Unauthorized" },
              "403": { "$ref": "#/components/responses/Forbidden" }
            }
          }
        },
        "/api/v1/employee/orders": {
          "post": {
            "tags": ["Employee"],
            "summary": "Create a meal order",
            "operationId": HttpOperation::CreateEmployeeOrder.operation_id(),
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
              "422": { "$ref": "#/components/responses/ValidationFailed" }
            }
          }
        },
        "/api/v1/employee/orders/{orderId}": {
          "patch": {
            "tags": ["Employee"],
            "summary": "Modify an existing order before cutoff",
            "operationId": HttpOperation::UpdateEmployeeOrder.operation_id(),
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
              "422": { "$ref": "#/components/responses/ValidationFailed" }
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
        "/api/v1/admin/vendors/{vendorId}/approvals": {
          "post": {
            "tags": ["Admin"],
            "summary": "Approve or reject vendor enrollment",
            "operationId": HttpOperation::ApproveVendorEnrollment.operation_id(),
            "security": [{ "corporateSsoBearer": [] }],
            "parameters": [
              { "$ref": "#/components/parameters/VendorIdPath" }
            ],
            "requestBody": {
              "required": true,
              "content": {
                "application/json": {
                  "schema": { "$ref": "#/components/schemas/AdminVendorApprovalRequest" }
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
        "/api/v1/integrations/payroll/deductions": {
          "get": {
            "tags": ["Integration"],
            "summary": "Export payroll deduction records",
            "operationId": HttpOperation::ExportPayrollDeductions.operation_id(),
            "security": [{ "corporateSsoBearer": [] }],
            "parameters": [
              { "$ref": "#/components/parameters/PayPeriodQuery" },
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
              "403": { "$ref": "#/components/responses/Forbidden" }
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
            "required": true,
            "schema": {
              "type": "string",
              "format": "date"
            }
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
          "CuisineFilterQuery": {
            "name": "cuisine",
            "in": "query",
            "required": false,
            "schema": {
              "type": "string",
              "minLength": 2,
              "maxLength": 32
            }
          },
          "HealthTagFilterQuery": {
            "name": "healthTag",
            "in": "query",
            "required": false,
            "schema": { "$ref": "#/components/schemas/MenuHealthTag" }
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
          "OrderIdPath": {
            "name": "orderId",
            "in": "path",
            "required": true,
            "schema": {
              "type": "string",
              "pattern": "^ord-[a-z0-9]{8,32}$"
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
              "VENDOR_ACCOUNT_MFA"
            ]
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
          "VendorOrderSortField": {
            "type": "string",
            "enum": ["deliveryDate", "plantId", "status", "createdAt"]
          },
          "VendorSortField": {
            "type": "string",
            "enum": ["createdAt", "status", "displayName"]
          },
          "PayrollSortField": {
            "type": "string",
            "enum": ["employeeActorId", "amountMinor", "deliveryDate"]
          },
          "MenuHealthTag": {
            "type": "string",
            "enum": ["LOW_CALORIE", "HIGH_PROTEIN", "VEGETARIAN", "VEGAN", "GLUTEN_FREE"]
          },
          "EmployeeOrderStatus": {
            "type": "string",
            "enum": ["PENDING", "CONFIRMED", "CANCELLED", "FULFILLED"]
          },
          "VendorStatus": {
            "type": "string",
            "enum": ["PENDING_REVIEW", "APPROVED", "REJECTED", "SUSPENDED"]
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
              "price",
              "remainingQuantity",
              "deliveryDate",
              "deliverablePlantIds",
              "healthTags"
            ],
            "properties": {
              "menuItemId": { "type": "string", "pattern": "^menu-[a-z0-9]{8,32}$" },
              "vendorId": { "type": "string", "pattern": "^ven-[a-z0-9]{8,32}$" },
              "name": { "type": "string", "minLength": 1, "maxLength": 80 },
              "description": { "type": "string", "maxLength": 280 },
              "price": { "$ref": "#/components/schemas/Money" },
              "remainingQuantity": { "type": "integer", "minimum": 0, "maximum": 2000 },
              "deliveryDate": { "type": "string", "format": "date" },
              "deliverablePlantIds": {
                "type": "array",
                "items": { "$ref": "#/components/schemas/PlantId" },
                "minItems": 1,
                "uniqueItems": true
              },
              "cuisine": { "type": "string", "minLength": 2, "maxLength": 32 },
              "healthTags": {
                "type": "array",
                "items": { "$ref": "#/components/schemas/MenuHealthTag" },
                "uniqueItems": true
              }
            },
            "additionalProperties": false
          },
          "MenuPage": {
            "type": "object",
            "required": ["items", "page"],
            "properties": {
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
              "note": { "type": "string", "maxLength": 120 }
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
          "EmployeeOrderPatchRequest": {
            "type": "object",
            "required": ["status"],
            "properties": {
              "status": {
                "type": "string",
                "enum": ["CANCELLED"]
              },
              "cancelReason": {
                "type": "string",
                "minLength": 5,
                "maxLength": 200
              }
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
              "total"
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
              "createdAt": { "type": "string", "format": "date-time" }
            },
            "additionalProperties": false
          },
          "VendorOrderBoardEntry": {
            "type": "object",
            "required": ["orderId", "plantId", "deliveryDate", "status", "lineItems"],
            "properties": {
              "orderId": { "type": "string", "pattern": "^ord-[a-z0-9]{8,32}$" },
              "plantId": { "$ref": "#/components/schemas/PlantId" },
              "deliveryDate": { "type": "string", "format": "date" },
              "status": { "$ref": "#/components/schemas/EmployeeOrderStatus" },
              "lineItems": {
                "type": "array",
                "items": { "$ref": "#/components/schemas/OrderLineItem" },
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
          "VendorMenuItemUpsertRequest": {
            "type": "object",
            "required": [
              "name",
              "description",
              "price",
              "maxDailyQuantity",
              "deliveryDate",
              "deliverablePlantIds"
            ],
            "properties": {
              "name": { "type": "string", "minLength": 1, "maxLength": 80 },
              "description": { "type": "string", "minLength": 1, "maxLength": 280 },
              "price": { "$ref": "#/components/schemas/Money" },
              "maxDailyQuantity": { "type": "integer", "minimum": 1, "maximum": 2000 },
              "deliveryDate": { "type": "string", "format": "date" },
              "deliverablePlantIds": {
                "type": "array",
                "items": { "$ref": "#/components/schemas/PlantId" },
                "minItems": 1,
                "uniqueItems": true
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
              "price",
              "maxDailyQuantity",
              "deliveryDate",
              "deliverablePlantIds"
            ],
            "properties": {
              "menuItemId": { "type": "string", "pattern": "^menu-[a-z0-9]{8,32}$" },
              "vendorId": { "type": "string", "pattern": "^ven-[a-z0-9]{8,32}$" },
              "name": { "type": "string", "minLength": 1, "maxLength": 80 },
              "description": { "type": "string", "minLength": 1, "maxLength": 280 },
              "price": { "$ref": "#/components/schemas/Money" },
              "maxDailyQuantity": { "type": "integer", "minimum": 1, "maximum": 2000 },
              "deliveryDate": { "type": "string", "format": "date" },
              "deliverablePlantIds": {
                "type": "array",
                "items": { "$ref": "#/components/schemas/PlantId" },
                "minItems": 1,
                "uniqueItems": true
              },
              "healthTags": {
                "type": "array",
                "items": { "$ref": "#/components/schemas/MenuHealthTag" },
                "uniqueItems": true
              }
            },
            "additionalProperties": false
          },
          "AdminVendorApprovalRequest": {
            "type": "object",
            "required": ["decision", "comment"],
            "properties": {
              "decision": {
                "type": "string",
                "enum": ["APPROVED", "REJECTED"]
              },
              "comment": {
                "type": "string",
                "minLength": 5,
                "maxLength": 280
              }
            },
            "additionalProperties": false
          },
          "VendorEnrollment": {
            "type": "object",
            "required": ["vendorId", "displayName", "status", "updatedAt"],
            "properties": {
              "vendorId": { "type": "string", "pattern": "^ven-[a-z0-9]{8,32}$" },
              "displayName": { "type": "string", "minLength": 1, "maxLength": 120 },
              "status": { "$ref": "#/components/schemas/VendorStatus" },
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
          "PayrollDeductionRecord": {
            "type": "object",
            "required": [
              "employeeActorId",
              "orderId",
              "deliveryDate",
              "amount",
              "payPeriod",
              "status"
            ],
            "properties": {
              "employeeActorId": { "$ref": "#/components/schemas/ActorId" },
              "orderId": { "type": "string", "pattern": "^ord-[a-z0-9]{8,32}$" },
              "deliveryDate": { "type": "string", "format": "date" },
              "amount": { "$ref": "#/components/schemas/Money" },
              "payPeriod": { "type": "string", "pattern": "^[0-9]{4}-[0-9]{2}$" },
              "status": { "type": "string", "enum": ["READY", "LOCKED", "REFUNDED"] }
            },
            "additionalProperties": false
          },
          "PayrollDeductionPage": {
            "type": "object",
            "required": ["items", "page"],
            "properties": {
              "items": {
                "type": "array",
                "items": { "$ref": "#/components/schemas/PayrollDeductionRecord" }
              },
              "page": { "$ref": "#/components/schemas/PageMeta" }
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
              "VALIDATION_FAILED"
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
