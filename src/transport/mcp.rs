use std::collections::HashSet;

use crate::access::{
    AccessController, Action, AuthorizationError, AuthorizedWriteOperation, TransportLayer,
};
use crate::identity::{AuthenticatedActorContext, PlantId};

#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum McpOperation {
    PlaceEmployeeOrder,
    ManageVendorMenu,
    ApproveVendorEnrollment,
    ExportPayrollDeductions,
}

impl McpOperation {
    pub const ALL: [Self; 4] = [
        Self::PlaceEmployeeOrder,
        Self::ManageVendorMenu,
        Self::ApproveVendorEnrollment,
        Self::ExportPayrollDeductions,
    ];

    pub const fn operation_id(self) -> &'static str {
        match self {
            Self::PlaceEmployeeOrder => "placeEmployeeOrder",
            Self::ManageVendorMenu => "manageVendorMenu",
            Self::ApproveVendorEnrollment => "approveVendorEnrollment",
            Self::ExportPayrollDeductions => "exportPayrollDeductions",
        }
    }

    pub const fn action(self) -> Action {
        match self {
            Self::PlaceEmployeeOrder => Action::PlaceEmployeeOrder,
            Self::ManageVendorMenu => Action::ManageVendorMenu,
            Self::ApproveVendorEnrollment => Action::ApproveVendorEnrollment,
            Self::ExportPayrollDeductions => Action::ExportPayrollDeductions,
        }
    }

    pub fn from_operation_id(value: &str) -> Option<Self> {
        match value {
            "placeEmployeeOrder" => Some(Self::PlaceEmployeeOrder),
            "manageVendorMenu" => Some(Self::ManageVendorMenu),
            "approveVendorEnrollment" => Some(Self::ApproveVendorEnrollment),
            "exportPayrollDeductions" => Some(Self::ExportPayrollDeductions),
            _ => None,
        }
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub struct RuntimeMcpTool {
    tool_name: &'static str,
    operation: McpOperation,
}

impl RuntimeMcpTool {
    pub const fn new(tool_name: &'static str, operation: McpOperation) -> Self {
        Self {
            tool_name,
            operation,
        }
    }

    pub const fn tool_name(self) -> &'static str {
        self.tool_name
    }

    pub const fn operation(self) -> McpOperation {
        self.operation
    }

    pub const fn operation_id(self) -> &'static str {
        self.operation.operation_id()
    }

    pub const fn action(self) -> Action {
        self.operation.action()
    }
}

const RUNTIME_MCP_TOOLS: [RuntimeMcpTool; 0] = [];

pub fn runtime_mcp_tools() -> &'static [RuntimeMcpTool] {
    &RUNTIME_MCP_TOOLS
}

pub fn mcp_contract_checks_enabled() -> bool {
    !runtime_mcp_tools().is_empty()
}

pub fn runtime_mcp_tool_contract_issues() -> Vec<String> {
    if !mcp_contract_checks_enabled() {
        return Vec::new();
    }

    let mut issues = Vec::new();
    let mut tool_names = HashSet::new();
    let mut operation_ids = HashSet::new();

    for tool in runtime_mcp_tools() {
        if !tool_names.insert(tool.tool_name()) {
            issues.push(format!(
                "duplicate MCP tool name `{}` in runtime catalog",
                tool.tool_name()
            ));
        }

        if !operation_ids.insert(tool.operation_id()) {
            issues.push(format!(
                "duplicate MCP operation id `{}` in runtime catalog",
                tool.operation_id()
            ));
        }

        if McpOperation::from_operation_id(tool.operation_id()).is_none() {
            issues.push(format!(
                "MCP tool `{}` references undefined operation id `{}`",
                tool.tool_name(),
                tool.operation_id()
            ));
        }
    }

    issues
}

fn resolve_runtime_mcp_operation(operation_id: &str) -> Option<McpOperation> {
    runtime_mcp_tools()
        .iter()
        .find(|tool| tool.operation_id() == operation_id)
        .map(|tool| tool.operation())
}

#[derive(Clone)]
pub struct McpAuthorizationGateway {
    access_controller: AccessController,
}

impl McpAuthorizationGateway {
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
        if mcp_contract_checks_enabled() {
            let operation = resolve_runtime_mcp_operation(&operation_id).ok_or(
                AuthorizationError::UnknownMcpOperationId {
                    operation_id: operation_id.clone(),
                },
            )?;

            let expected_action = operation.action();
            if expected_action != action {
                return Err(AuthorizationError::McpOperationActionMismatch {
                    operation_id,
                    expected_action,
                    provided_action: action,
                });
            }
        }

        self.access_controller.authorize_write(
            actor,
            action,
            target_plant,
            TransportLayer::Mcp,
            operation_id,
        )
    }
}
