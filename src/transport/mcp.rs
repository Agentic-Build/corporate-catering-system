use std::collections::{BTreeSet, HashSet};

use crate::access::{
    AccessController, Action, AuthorizationError, AuthorizedWriteOperation, TransportLayer,
};
use crate::anomaly_alert::{
    AnomalyAlertError, AnomalyAlertEvaluationResult, AnomalyAlertId, AnomalyAlertQuery,
    AnomalyAlertRecord, AnomalyAlertTransition, AnomalyAlertWorkflow, AnomalyRule,
    AnomalySignalSnapshot,
};
use crate::audit::AuditTimestamp;
use crate::identity::{ActorId, AuthenticatedActorContext, PlantId};
use crate::menu_supply_window::{
    EmployeeMenuDiscoveryEntry, MenuSupplyPolicy, MenuSupplyWindowError, OrderId,
    OrderLifecycleState, OrderLineItemRequest, OrderMutation,
};
use crate::observability::{TelemetryOutcome, TelemetryService};
use crate::payroll::{
    OrderPayrollView, PayrollExchangeBatch, PayrollExchangeBatchId, PayrollExportPage,
    PayrollHrApiSyncOutcome, PayrollLedgerError, PayrollLedgerService,
    PayrollSettlementLockReceipt, PayrollSortField, SortOrder,
};
use crate::pickup_totp::{PickupTotpVerificationError, PickupTotpVerifier, VerifiedTotp};
use crate::transport::http::{
    HttpEmployeeDiscoveryError, HttpEmployeeDiscoveryExecutionGateway, HttpOrderExecutionError,
    HttpOrderingExecutionGateway,
};
use crate::vendor_compliance::{
    ComplianceDate, LifecycleRunResult, VendorComplianceError, VendorComplianceLifecycle,
    VendorComplianceStatus, VendorId, VendorReviewDecision,
};
use crate::vendor_delivery_mapping::{TaipeiBusinessMoment, VendorPlantDeliveryPolicy};

const MAX_BRIDGE_KEY_TTL_SECONDS: i64 = 15 * 60;
const MAX_BRIDGE_ROTATION_STALENESS_SECONDS: i64 = 5 * 60;
const MAX_BRIDGE_AUDIT_REASON_LENGTH: usize = 280;

#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum McpCapabilityDomain {
    Ordering,
    Verification,
    ComplianceReview,
    Settlement,
    Anomaly,
}

impl McpCapabilityDomain {
    pub const ALL: [Self; 5] = [
        Self::Ordering,
        Self::Verification,
        Self::ComplianceReview,
        Self::Settlement,
        Self::Anomaly,
    ];

    pub const fn as_str(self) -> &'static str {
        match self {
            Self::Ordering => "ordering",
            Self::Verification => "verification",
            Self::ComplianceReview => "compliance-review",
            Self::Settlement => "settlement",
            Self::Anomaly => "anomaly",
        }
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum McpToolRisk {
    ReadOnly,
    Write,
    HighRiskWrite,
}

impl McpToolRisk {
    pub const fn is_write(self) -> bool {
        matches!(self, Self::Write | Self::HighRiskWrite)
    }

    pub const fn is_high_risk_write(self) -> bool {
        matches!(self, Self::HighRiskWrite)
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum McpOperation {
    PlaceEmployeeOrder,
    ManageVendorMenu,
    ManageVendorComplianceLifecycle,
    ExportPayrollDeductions,
}

impl McpOperation {
    pub const ALL: [Self; 4] = [
        Self::PlaceEmployeeOrder,
        Self::ManageVendorMenu,
        Self::ManageVendorComplianceLifecycle,
        Self::ExportPayrollDeductions,
    ];

    pub const fn operation_id(self) -> &'static str {
        match self {
            Self::PlaceEmployeeOrder => "placeEmployeeOrder",
            Self::ManageVendorMenu => "manageVendorMenu",
            Self::ManageVendorComplianceLifecycle => "manageVendorComplianceLifecycle",
            Self::ExportPayrollDeductions => "exportPayrollDeductions",
        }
    }

    pub const fn action(self) -> Action {
        match self {
            Self::PlaceEmployeeOrder => Action::PlaceEmployeeOrder,
            Self::ManageVendorMenu => Action::ManageVendorMenu,
            Self::ManageVendorComplianceLifecycle => Action::ManageVendorComplianceLifecycle,
            Self::ExportPayrollDeductions => Action::ExportPayrollDeductions,
        }
    }

    pub fn from_operation_id(value: &str) -> Option<Self> {
        match value {
            "placeEmployeeOrder" => Some(Self::PlaceEmployeeOrder),
            "manageVendorMenu" => Some(Self::ManageVendorMenu),
            "manageVendorComplianceLifecycle" => Some(Self::ManageVendorComplianceLifecycle),
            "exportPayrollDeductions" => Some(Self::ExportPayrollDeductions),
            _ => None,
        }
    }

    pub const fn from_action(action: Action) -> Option<Self> {
        match action {
            Action::PlaceEmployeeOrder => Some(Self::PlaceEmployeeOrder),
            Action::ManageVendorMenu => Some(Self::ManageVendorMenu),
            Action::ManageVendorComplianceLifecycle => Some(Self::ManageVendorComplianceLifecycle),
            Action::ExportPayrollDeductions => Some(Self::ExportPayrollDeductions),
        }
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub struct RuntimeMcpTool {
    tool_name: &'static str,
    operation: McpOperation,
    capability_domain: McpCapabilityDomain,
    risk: McpToolRisk,
}

impl RuntimeMcpTool {
    pub const fn new(
        tool_name: &'static str,
        operation: McpOperation,
        capability_domain: McpCapabilityDomain,
        risk: McpToolRisk,
    ) -> Self {
        Self {
            tool_name,
            operation,
            capability_domain,
            risk,
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

    pub const fn capability_domain(self) -> McpCapabilityDomain {
        self.capability_domain
    }

    pub const fn risk(self) -> McpToolRisk {
        self.risk
    }

    pub const fn is_high_risk_write(self) -> bool {
        self.risk.is_high_risk_write()
    }
}

const RUNTIME_MCP_TOOLS: [RuntimeMcpTool; 15] = [
    RuntimeMcpTool::new(
        "ordering.list_menu_discovery",
        McpOperation::PlaceEmployeeOrder,
        McpCapabilityDomain::Ordering,
        McpToolRisk::ReadOnly,
    ),
    RuntimeMcpTool::new(
        "ordering.create_employee_order",
        McpOperation::PlaceEmployeeOrder,
        McpCapabilityDomain::Ordering,
        McpToolRisk::HighRiskWrite,
    ),
    RuntimeMcpTool::new(
        "ordering.update_employee_order",
        McpOperation::PlaceEmployeeOrder,
        McpCapabilityDomain::Ordering,
        McpToolRisk::Write,
    ),
    RuntimeMcpTool::new(
        "verification.verify_pickup_totp",
        McpOperation::PlaceEmployeeOrder,
        McpCapabilityDomain::Verification,
        McpToolRisk::HighRiskWrite,
    ),
    RuntimeMcpTool::new(
        "compliance.review_vendor_application",
        McpOperation::ManageVendorComplianceLifecycle,
        McpCapabilityDomain::ComplianceReview,
        McpToolRisk::HighRiskWrite,
    ),
    RuntimeMcpTool::new(
        "compliance.run_vendor_lifecycle",
        McpOperation::ManageVendorComplianceLifecycle,
        McpCapabilityDomain::ComplianceReview,
        McpToolRisk::Write,
    ),
    RuntimeMcpTool::new(
        "settlement.query_order_ledger",
        McpOperation::ExportPayrollDeductions,
        McpCapabilityDomain::Settlement,
        McpToolRisk::ReadOnly,
    ),
    RuntimeMcpTool::new(
        "settlement.export_payroll_deductions",
        McpOperation::ExportPayrollDeductions,
        McpCapabilityDomain::Settlement,
        McpToolRisk::HighRiskWrite,
    ),
    RuntimeMcpTool::new(
        "settlement.close_monthly_settlement",
        McpOperation::ExportPayrollDeductions,
        McpCapabilityDomain::Settlement,
        McpToolRisk::HighRiskWrite,
    ),
    RuntimeMcpTool::new(
        "settlement.lock_cycle",
        McpOperation::ManageVendorComplianceLifecycle,
        McpCapabilityDomain::Settlement,
        McpToolRisk::HighRiskWrite,
    ),
    RuntimeMcpTool::new(
        "settlement.unlock_cycle",
        McpOperation::ManageVendorComplianceLifecycle,
        McpCapabilityDomain::Settlement,
        McpToolRisk::HighRiskWrite,
    ),
    RuntimeMcpTool::new(
        "anomaly.list_alerts",
        McpOperation::ManageVendorComplianceLifecycle,
        McpCapabilityDomain::Anomaly,
        McpToolRisk::ReadOnly,
    ),
    RuntimeMcpTool::new(
        "anomaly.evaluate_alerts",
        McpOperation::ManageVendorComplianceLifecycle,
        McpCapabilityDomain::Anomaly,
        McpToolRisk::Write,
    ),
    RuntimeMcpTool::new(
        "anomaly.update_alert_status",
        McpOperation::ManageVendorComplianceLifecycle,
        McpCapabilityDomain::Anomaly,
        McpToolRisk::HighRiskWrite,
    ),
    RuntimeMcpTool::new(
        "anomaly.upsert_rule",
        McpOperation::ManageVendorComplianceLifecycle,
        McpCapabilityDomain::Anomaly,
        McpToolRisk::HighRiskWrite,
    ),
];

#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub struct RuntimeMcpResource {
    resource_uri: &'static str,
    capability_domain: McpCapabilityDomain,
}

impl RuntimeMcpResource {
    pub const fn new(resource_uri: &'static str, capability_domain: McpCapabilityDomain) -> Self {
        Self {
            resource_uri,
            capability_domain,
        }
    }

    pub const fn resource_uri(self) -> &'static str {
        self.resource_uri
    }

    pub const fn capability_domain(self) -> McpCapabilityDomain {
        self.capability_domain
    }
}

const RUNTIME_MCP_RESOURCES: [RuntimeMcpResource; 6] = [
    RuntimeMcpResource::new(
        "resource://ordering/menu-discovery",
        McpCapabilityDomain::Ordering,
    ),
    RuntimeMcpResource::new(
        "resource://verification/pickup-verifications",
        McpCapabilityDomain::Verification,
    ),
    RuntimeMcpResource::new(
        "resource://compliance/vendor-reviews",
        McpCapabilityDomain::ComplianceReview,
    ),
    RuntimeMcpResource::new(
        "resource://settlement/payroll-ledger",
        McpCapabilityDomain::Settlement,
    ),
    RuntimeMcpResource::new(
        "resource://settlement/payroll-exports",
        McpCapabilityDomain::Settlement,
    ),
    RuntimeMcpResource::new("resource://anomaly/alerts", McpCapabilityDomain::Anomaly),
];

pub fn runtime_mcp_tools() -> &'static [RuntimeMcpTool] {
    &RUNTIME_MCP_TOOLS
}

pub fn runtime_mcp_resources() -> &'static [RuntimeMcpResource] {
    &RUNTIME_MCP_RESOURCES
}

pub fn runtime_mcp_tool_contract_issues() -> Vec<String> {
    let mut issues = Vec::new();
    let mut tool_names = HashSet::new();
    let mut covered_domains = HashSet::new();

    for tool in runtime_mcp_tools() {
        if !tool_names.insert(tool.tool_name()) {
            issues.push(format!(
                "duplicate MCP tool name `{}` in runtime catalog",
                tool.tool_name()
            ));
        }

        if McpOperation::from_operation_id(tool.operation_id()).is_none() {
            issues.push(format!(
                "MCP tool `{}` references undefined operation id `{}`",
                tool.tool_name(),
                tool.operation_id()
            ));
        }

        covered_domains.insert(tool.capability_domain());
    }

    for domain in McpCapabilityDomain::ALL {
        if !covered_domains.contains(&domain) {
            issues.push(format!(
                "MCP tool catalog is missing required capability domain `{}`",
                domain.as_str()
            ));
        }
    }

    issues
}

pub fn runtime_mcp_resource_contract_issues() -> Vec<String> {
    let mut issues = Vec::new();
    let mut resource_uris = HashSet::new();
    let mut covered_domains = HashSet::new();

    for resource in runtime_mcp_resources() {
        if !resource_uris.insert(resource.resource_uri()) {
            issues.push(format!(
                "duplicate MCP resource uri `{}` in runtime catalog",
                resource.resource_uri()
            ));
        }
        covered_domains.insert(resource.capability_domain());
    }

    for domain in McpCapabilityDomain::ALL {
        if !covered_domains.contains(&domain) {
            issues.push(format!(
                "MCP resource catalog is missing required capability domain `{}`",
                domain.as_str()
            ));
        }
    }

    issues
}

fn runtime_mcp_tool_by_name(tool_name: &str) -> Option<RuntimeMcpTool> {
    runtime_mcp_tools()
        .iter()
        .copied()
        .find(|tool| tool.tool_name() == tool_name)
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum McpAuthenticationModel {
    OAuthServiceAccount,
    OAuthServiceAccountWithBridgeKey,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct McpServiceAccountGrant {
    service_account_id: ActorId,
    actor: AuthenticatedActorContext,
    allowed_tool_names: BTreeSet<String>,
}

impl McpServiceAccountGrant {
    pub fn new<I, S>(
        service_account_id: ActorId,
        actor: AuthenticatedActorContext,
        allowed_tool_names: I,
    ) -> Result<Self, McpAuthorizationError>
    where
        I: IntoIterator<Item = S>,
        S: Into<String>,
    {
        if actor.actor_id() != &service_account_id {
            return Err(McpAuthorizationError::ServiceAccountActorMismatch {
                service_account_id,
                actor_id: actor.actor_id().clone(),
            });
        }

        let mut normalized_tool_names = BTreeSet::new();
        for tool_name in allowed_tool_names {
            let tool_name = tool_name.into();
            let normalized = tool_name.trim();
            if normalized.is_empty() {
                return Err(McpAuthorizationError::InvalidToolGrantName {
                    service_account_id: service_account_id.clone(),
                    tool_name,
                });
            }
            normalized_tool_names.insert(normalized.to_owned());
        }

        if normalized_tool_names.is_empty() {
            return Err(McpAuthorizationError::EmptyServiceAccountToolGrant { service_account_id });
        }

        Ok(Self {
            service_account_id,
            actor,
            allowed_tool_names: normalized_tool_names,
        })
    }

    pub fn service_account_id(&self) -> &ActorId {
        &self.service_account_id
    }

    pub fn actor(&self) -> &AuthenticatedActorContext {
        &self.actor
    }

    pub fn allowed_tool_names(&self) -> &BTreeSet<String> {
        &self.allowed_tool_names
    }

    fn allows_tool(&self, tool_name: &str) -> bool {
        self.allowed_tool_names.contains(tool_name)
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct McpShortLivedKeyBridge {
    key_id: String,
    issued_at_epoch_seconds: i64,
    expires_at_epoch_seconds: i64,
    rotated_at_epoch_seconds: i64,
    audit_reason: String,
}

impl McpShortLivedKeyBridge {
    pub fn new(
        key_id: impl Into<String>,
        issued_at_epoch_seconds: i64,
        expires_at_epoch_seconds: i64,
        rotated_at_epoch_seconds: i64,
        audit_reason: impl Into<String>,
    ) -> Result<Self, McpAuthorizationError> {
        let key_id = key_id.into();
        let audit_reason = audit_reason.into();
        if key_id.trim().is_empty() {
            return Err(McpAuthorizationError::InvalidBridgeKeyId);
        }
        if audit_reason.trim().is_empty() || audit_reason.len() > MAX_BRIDGE_AUDIT_REASON_LENGTH {
            return Err(McpAuthorizationError::InvalidBridgeAuditReason);
        }
        if expires_at_epoch_seconds <= issued_at_epoch_seconds {
            return Err(McpAuthorizationError::BridgeKeyWindowInvalid {
                issued_at_epoch_seconds,
                expires_at_epoch_seconds,
            });
        }
        if rotated_at_epoch_seconds < issued_at_epoch_seconds
            || rotated_at_epoch_seconds > expires_at_epoch_seconds
        {
            return Err(McpAuthorizationError::BridgeKeyWindowInvalid {
                issued_at_epoch_seconds,
                expires_at_epoch_seconds,
            });
        }

        Ok(Self {
            key_id: key_id.trim().to_owned(),
            issued_at_epoch_seconds,
            expires_at_epoch_seconds,
            rotated_at_epoch_seconds,
            audit_reason: audit_reason.trim().to_owned(),
        })
    }

    pub fn key_id(&self) -> &str {
        &self.key_id
    }

    pub fn audit_reason(&self) -> &str {
        &self.audit_reason
    }

    fn validate_for_use(&self, now_epoch_seconds: i64) -> Result<(), McpAuthorizationError> {
        if now_epoch_seconds < self.issued_at_epoch_seconds {
            return Err(McpAuthorizationError::BridgeKeyIssuedInFuture {
                key_id: self.key_id.clone(),
                issued_at_epoch_seconds: self.issued_at_epoch_seconds,
                now_epoch_seconds,
            });
        }

        if now_epoch_seconds > self.expires_at_epoch_seconds {
            return Err(McpAuthorizationError::BridgeKeyExpired {
                key_id: self.key_id.clone(),
                expires_at_epoch_seconds: self.expires_at_epoch_seconds,
                now_epoch_seconds,
            });
        }

        let ttl_seconds = self.expires_at_epoch_seconds - self.issued_at_epoch_seconds;
        if ttl_seconds > MAX_BRIDGE_KEY_TTL_SECONDS {
            return Err(McpAuthorizationError::BridgeKeyTtlTooLong {
                key_id: self.key_id.clone(),
                ttl_seconds,
                max_allowed_seconds: MAX_BRIDGE_KEY_TTL_SECONDS,
            });
        }

        if now_epoch_seconds - self.rotated_at_epoch_seconds > MAX_BRIDGE_ROTATION_STALENESS_SECONDS
        {
            return Err(McpAuthorizationError::BridgeKeyRotationStale {
                key_id: self.key_id.clone(),
                rotated_at_epoch_seconds: self.rotated_at_epoch_seconds,
                now_epoch_seconds,
                max_staleness_seconds: MAX_BRIDGE_ROTATION_STALENESS_SECONDS,
            });
        }

        Ok(())
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum McpAuthorizationError {
    Authorization(AuthorizationError),
    UnknownMcpToolName {
        tool_name: String,
    },
    ServiceAccountActorMismatch {
        service_account_id: ActorId,
        actor_id: ActorId,
    },
    EmptyServiceAccountToolGrant {
        service_account_id: ActorId,
    },
    InvalidToolGrantName {
        service_account_id: ActorId,
        tool_name: String,
    },
    ToolNotGrantedForServiceAccount {
        service_account_id: ActorId,
        tool_name: String,
    },
    InvalidBridgeKeyId,
    InvalidBridgeAuditReason,
    BridgeKeyWindowInvalid {
        issued_at_epoch_seconds: i64,
        expires_at_epoch_seconds: i64,
    },
    BridgeKeyIssuedInFuture {
        key_id: String,
        issued_at_epoch_seconds: i64,
        now_epoch_seconds: i64,
    },
    BridgeKeyExpired {
        key_id: String,
        expires_at_epoch_seconds: i64,
        now_epoch_seconds: i64,
    },
    BridgeKeyTtlTooLong {
        key_id: String,
        ttl_seconds: i64,
        max_allowed_seconds: i64,
    },
    BridgeKeyRotationStale {
        key_id: String,
        rotated_at_epoch_seconds: i64,
        now_epoch_seconds: i64,
        max_staleness_seconds: i64,
    },
}

impl std::fmt::Display for McpAuthorizationError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::Authorization(error) => error.fmt(f),
            Self::UnknownMcpToolName { tool_name } => {
                write!(f, "mcp tool `{tool_name}` is not defined in runtime catalog")
            }
            Self::ServiceAccountActorMismatch {
                service_account_id,
                actor_id,
            } => write!(
                f,
                "service account id {service_account_id} must match actor id {actor_id}",
            ),
            Self::EmptyServiceAccountToolGrant { service_account_id } => write!(
                f,
                "service account {service_account_id} must grant at least one MCP tool",
            ),
            Self::InvalidToolGrantName {
                service_account_id,
                tool_name,
            } => write!(
                f,
                "service account {service_account_id} has invalid tool grant `{tool_name}`",
            ),
            Self::ToolNotGrantedForServiceAccount {
                service_account_id,
                tool_name,
            } => write!(
                f,
                "service account {service_account_id} is not allowed to execute MCP tool `{tool_name}`",
            ),
            Self::InvalidBridgeKeyId => {
                f.write_str("short-lived MCP bridge key id must be non-empty")
            }
            Self::InvalidBridgeAuditReason => write!(
                f,
                "short-lived MCP bridge key audit reason must be non-empty and at most {MAX_BRIDGE_AUDIT_REASON_LENGTH} characters",
            ),
            Self::BridgeKeyWindowInvalid {
                issued_at_epoch_seconds,
                expires_at_epoch_seconds,
            } => write!(
                f,
                "short-lived MCP bridge key window is invalid: issuedAt={issued_at_epoch_seconds}, expiresAt={expires_at_epoch_seconds}",
            ),
            Self::BridgeKeyIssuedInFuture {
                key_id,
                issued_at_epoch_seconds,
                now_epoch_seconds,
            } => write!(
                f,
                "short-lived MCP bridge key {key_id} is not active yet: issuedAt={issued_at_epoch_seconds}, now={now_epoch_seconds}",
            ),
            Self::BridgeKeyExpired {
                key_id,
                expires_at_epoch_seconds,
                now_epoch_seconds,
            } => write!(
                f,
                "short-lived MCP bridge key {key_id} is expired: expiresAt={expires_at_epoch_seconds}, now={now_epoch_seconds}",
            ),
            Self::BridgeKeyTtlTooLong {
                key_id,
                ttl_seconds,
                max_allowed_seconds,
            } => write!(
                f,
                "short-lived MCP bridge key {key_id} TTL {ttl_seconds}s exceeds max {max_allowed_seconds}s",
            ),
            Self::BridgeKeyRotationStale {
                key_id,
                rotated_at_epoch_seconds,
                now_epoch_seconds,
                max_staleness_seconds,
            } => write!(
                f,
                "short-lived MCP bridge key {key_id} rotation is stale: rotatedAt={rotated_at_epoch_seconds}, now={now_epoch_seconds}, maxStaleness={max_staleness_seconds}s",
            ),
        }
    }
}

impl std::error::Error for McpAuthorizationError {}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AuthorizedMcpToolWrite {
    authorized_write: AuthorizedWriteOperation,
    tool_name: String,
    capability_domain: McpCapabilityDomain,
    risk: McpToolRisk,
    service_account_id: ActorId,
    bridge_key_id: Option<String>,
    bridge_audit_reason: Option<String>,
    authentication_model: McpAuthenticationModel,
}

impl AuthorizedMcpToolWrite {
    pub fn authorized_write(&self) -> &AuthorizedWriteOperation {
        &self.authorized_write
    }

    pub fn tool_name(&self) -> &str {
        &self.tool_name
    }

    pub fn capability_domain(&self) -> McpCapabilityDomain {
        self.capability_domain
    }

    pub fn risk(&self) -> McpToolRisk {
        self.risk
    }

    pub fn service_account_id(&self) -> &ActorId {
        &self.service_account_id
    }

    pub fn bridge_key_id(&self) -> Option<&str> {
        self.bridge_key_id.as_deref()
    }

    pub fn bridge_audit_reason(&self) -> Option<&str> {
        self.bridge_audit_reason.as_deref()
    }

    pub fn authentication_model(&self) -> McpAuthenticationModel {
        self.authentication_model
    }
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
        let telemetry = TelemetryService::McpGateway.begin_operation(
            operation_id.clone(),
            actor.map(|value| value.actor_id().as_str()),
            target_plant.map(PlantId::as_str),
        );

        let result = (|| {
            let operation = McpOperation::from_operation_id(&operation_id).ok_or(
                AuthorizationError::UnknownMcpOperationId {
                    operation_id: operation_id.clone(),
                },
            )?;
            let expected_action = operation.action();
            if expected_action != action {
                return Err(AuthorizationError::McpOperationActionMismatch {
                    operation_id: operation_id.clone(),
                    expected_action,
                    provided_action: action,
                });
            }

            self.access_controller.authorize_write(
                actor,
                action,
                target_plant,
                TransportLayer::Mcp,
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

    pub fn authorize_tool_write(
        &self,
        grant: &McpServiceAccountGrant,
        tool_name: impl Into<String>,
        target_plant: Option<&PlantId>,
        now_epoch_seconds: i64,
        bridge: Option<&McpShortLivedKeyBridge>,
    ) -> Result<AuthorizedMcpToolWrite, McpAuthorizationError> {
        let tool_name = tool_name.into();
        let normalized_tool_name = tool_name.trim();
        let tool = runtime_mcp_tool_by_name(normalized_tool_name).ok_or(
            McpAuthorizationError::UnknownMcpToolName {
                tool_name: tool_name.clone(),
            },
        )?;

        if !grant.allows_tool(tool.tool_name()) {
            return Err(McpAuthorizationError::ToolNotGrantedForServiceAccount {
                service_account_id: grant.service_account_id().clone(),
                tool_name,
            });
        }

        let (bridge_key_id, bridge_audit_reason, authentication_model) =
            if let Some(bridge) = bridge {
                bridge.validate_for_use(now_epoch_seconds)?;
                (
                    Some(bridge.key_id().to_owned()),
                    Some(bridge.audit_reason().to_owned()),
                    McpAuthenticationModel::OAuthServiceAccountWithBridgeKey,
                )
            } else {
                (None, None, McpAuthenticationModel::OAuthServiceAccount)
            };

        let authorized_write = self
            .authorize_write(
                Some(grant.actor()),
                tool.action(),
                target_plant,
                tool.operation_id(),
            )
            .map_err(McpAuthorizationError::Authorization)?;

        Ok(AuthorizedMcpToolWrite {
            authorized_write,
            tool_name: tool.tool_name().to_owned(),
            capability_domain: tool.capability_domain(),
            risk: tool.risk(),
            service_account_id: grant.service_account_id().clone(),
            bridge_key_id,
            bridge_audit_reason,
            authentication_model,
        })
    }
}

pub struct McpOrderingExecutionGateway<'a> {
    compliance_lifecycle: &'a VendorComplianceLifecycle,
    delivery_policy: &'a VendorPlantDeliveryPolicy,
    menu_supply_policy: &'a MenuSupplyPolicy,
}

impl<'a> McpOrderingExecutionGateway<'a> {
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
        let telemetry = TelemetryService::McpGateway.begin_operation(
            "ordering.list_menu_discovery",
            None::<&str>,
            Some(plant_id.as_str()),
        );
        let gateway = HttpEmployeeDiscoveryExecutionGateway::new(
            self.compliance_lifecycle,
            self.delivery_policy,
            self.menu_supply_policy,
        );
        let result = gateway.execute_discovery_snapshot(plant_id, at, for_search);
        telemetry.finish(if result.is_ok() {
            TelemetryOutcome::Success
        } else {
            TelemetryOutcome::Error
        });
        result
    }

    #[allow(clippy::too_many_arguments)]
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
        let telemetry = TelemetryService::McpGateway.begin_operation(
            "ordering.create_employee_order",
            Some(actor.actor_id().as_str()),
            Some(plant_id.as_str()),
        );
        let gateway = HttpOrderingExecutionGateway::new(
            self.compliance_lifecycle,
            self.delivery_policy,
            self.menu_supply_policy,
        );
        let result = gateway.execute_create_employee_order(
            actor,
            order_id,
            vendor_id,
            plant_id,
            delivery_epoch_day,
            line_items,
            at,
        );
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
        let telemetry = TelemetryService::McpGateway.begin_operation(
            "ordering.update_employee_order",
            Some(actor.actor_id().as_str()),
            Some(plant_id.as_str()),
        );
        let gateway = HttpOrderingExecutionGateway::new(
            self.compliance_lifecycle,
            self.delivery_policy,
            self.menu_supply_policy,
        );
        let result = gateway
            .execute_update_employee_order(actor, order_id, vendor_id, plant_id, mutation, at);
        telemetry.finish(if result.is_ok() {
            TelemetryOutcome::Success
        } else {
            TelemetryOutcome::Error
        });
        result
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum McpPickupVerificationError {
    InvalidVerificationCodeFormat,
    OrderNotFound(OrderId),
    OrderAlreadyClaimed(OrderId),
    OrderStateConflict { order_id: OrderId, state: String },
    TimeResolutionFailed(String),
    Verification(PickupTotpVerificationError),
    MenuSupply(MenuSupplyWindowError),
}

impl std::fmt::Display for McpPickupVerificationError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::InvalidVerificationCodeFormat => {
                f.write_str("verification code must be non-empty")
            }
            Self::OrderNotFound(order_id) => {
                write!(f, "order {} not found", order_id.as_str())
            }
            Self::OrderAlreadyClaimed(order_id) => write!(
                f,
                "order {} has already been claimed via pickup verification",
                order_id.as_str()
            ),
            Self::OrderStateConflict { order_id, state } => write!(
                f,
                "order {} is in state {} and cannot be pickup verified",
                order_id.as_str(),
                state,
            ),
            Self::TimeResolutionFailed(message) => {
                write!(f, "failed to resolve pickup TOTP step: {message}")
            }
            Self::Verification(error) => write!(f, "{error:?}"),
            Self::MenuSupply(error) => error.fmt(f),
        }
    }
}

impl std::error::Error for McpPickupVerificationError {}

pub struct McpPickupVerificationExecutionGateway<'a> {
    menu_supply_policy: &'a MenuSupplyPolicy,
    pickup_totp_verifier: &'a PickupTotpVerifier,
}

impl<'a> McpPickupVerificationExecutionGateway<'a> {
    pub fn new(
        menu_supply_policy: &'a MenuSupplyPolicy,
        pickup_totp_verifier: &'a PickupTotpVerifier,
    ) -> Self {
        Self {
            menu_supply_policy,
            pickup_totp_verifier,
        }
    }

    pub fn execute_verify_pickup(
        &self,
        actor: &AuthenticatedActorContext,
        order_id: &OrderId,
        verification_code: &str,
        at: TaipeiBusinessMoment,
    ) -> Result<VerifiedTotp, McpPickupVerificationError> {
        let telemetry = TelemetryService::McpGateway.begin_operation(
            "verification.verify_pickup_totp",
            Some(actor.actor_id().as_str()),
            None::<&str>,
        );
        let result = (|| {
            let verification_code = verification_code.trim();
            if verification_code.is_empty() {
                return Err(McpPickupVerificationError::InvalidVerificationCodeFormat);
            }

            let snapshot = self
                .menu_supply_policy
                .order_snapshot(order_id)
                .map_err(McpPickupVerificationError::MenuSupply)?
                .ok_or_else(|| McpPickupVerificationError::OrderNotFound(order_id.clone()))?;
            if snapshot.state() == OrderLifecycleState::Fulfilled {
                return Err(McpPickupVerificationError::OrderAlreadyClaimed(
                    order_id.clone(),
                ));
            }
            if !matches!(
                snapshot.state(),
                OrderLifecycleState::Pending | OrderLifecycleState::Modified
            ) {
                return Err(McpPickupVerificationError::OrderStateConflict {
                    order_id: order_id.clone(),
                    state: snapshot.state().as_str().to_owned(),
                });
            }

            let current_step = PickupTotpVerifier::current_taipei_step()
                .map_err(McpPickupVerificationError::TimeResolutionFailed)?;
            let verified = self
                .pickup_totp_verifier
                .verify(order_id, verification_code, current_step)
                .map_err(McpPickupVerificationError::Verification)?;

            self.menu_supply_policy
                .update_order(actor, order_id, OrderMutation::MarkFulfilled, at)
                .map_err(McpPickupVerificationError::MenuSupply)?;

            Ok(verified)
        })();
        telemetry.finish(if result.is_ok() {
            TelemetryOutcome::Success
        } else {
            TelemetryOutcome::Error
        });
        result
    }
}

pub struct McpComplianceReviewExecutionGateway<'a> {
    compliance_lifecycle: &'a mut VendorComplianceLifecycle,
}

impl<'a> McpComplianceReviewExecutionGateway<'a> {
    pub fn new(compliance_lifecycle: &'a mut VendorComplianceLifecycle) -> Self {
        Self {
            compliance_lifecycle,
        }
    }

    pub fn execute_review_vendor_application(
        &mut self,
        actor: &AuthenticatedActorContext,
        vendor_id: &VendorId,
        decision: VendorReviewDecision,
        comment: impl Into<String>,
        decided_on: ComplianceDate,
    ) -> Result<VendorComplianceStatus, VendorComplianceError> {
        let telemetry = TelemetryService::McpGateway.begin_operation(
            "compliance.review_vendor_application",
            Some(actor.actor_id().as_str()),
            None::<&str>,
        );
        let result = self
            .compliance_lifecycle
            .review_application(actor, vendor_id, decision, comment, decided_on);
        telemetry.finish(if result.is_ok() {
            TelemetryOutcome::Success
        } else {
            TelemetryOutcome::Error
        });
        result
    }

    pub fn execute_run_vendor_lifecycle(
        &mut self,
        actor: &AuthenticatedActorContext,
        run_on: ComplianceDate,
    ) -> Result<LifecycleRunResult, VendorComplianceError> {
        let telemetry = TelemetryService::McpGateway.begin_operation(
            "compliance.run_vendor_lifecycle",
            Some(actor.actor_id().as_str()),
            None::<&str>,
        );
        let result = self.compliance_lifecycle.run_lifecycle(actor, run_on);
        telemetry.finish(if result.is_ok() {
            TelemetryOutcome::Success
        } else {
            TelemetryOutcome::Error
        });
        result
    }
}

pub struct McpSettlementExecutionGateway<'a> {
    payroll_ledger_service: &'a PayrollLedgerService,
}

impl<'a> McpSettlementExecutionGateway<'a> {
    pub fn new(payroll_ledger_service: &'a PayrollLedgerService) -> Self {
        Self {
            payroll_ledger_service,
        }
    }

    pub fn execute_query_employee_order_view(
        &self,
        actor: &AuthenticatedActorContext,
        order_id: &OrderId,
    ) -> Result<OrderPayrollView, PayrollLedgerError> {
        let telemetry = TelemetryService::McpGateway.begin_operation(
            "settlement.query_order_ledger",
            Some(actor.actor_id().as_str()),
            None::<&str>,
        );
        let result = self
            .payroll_ledger_service
            .employee_order_view(actor, order_id);
        telemetry.finish(if result.is_ok() {
            TelemetryOutcome::Success
        } else {
            TelemetryOutcome::Error
        });
        result
    }

    #[allow(clippy::too_many_arguments)]
    pub fn execute_export_payroll_deductions(
        &self,
        actor: &AuthenticatedActorContext,
        pay_period: &str,
        cycle_key: &str,
        page: usize,
        page_size: usize,
        sort_by: PayrollSortField,
        sort_order: SortOrder,
        occurred_at: AuditTimestamp,
    ) -> Result<PayrollExportPage, PayrollLedgerError> {
        let telemetry = TelemetryService::McpGateway.begin_operation(
            "settlement.export_payroll_deductions",
            Some(actor.actor_id().as_str()),
            None::<&str>,
        );
        let result = self.payroll_ledger_service.export_sftp_batch(
            actor,
            pay_period,
            cycle_key,
            page,
            page_size,
            sort_by,
            sort_order,
            occurred_at,
        );
        telemetry.finish(if result.is_ok() {
            TelemetryOutcome::Success
        } else {
            TelemetryOutcome::Error
        });
        result
    }

    pub fn execute_close_monthly_settlement(
        &self,
        actor: &AuthenticatedActorContext,
        cycle_key: Option<&str>,
        page: usize,
        page_size: usize,
        sort_by: PayrollSortField,
        sort_order: SortOrder,
        occurred_at: AuditTimestamp,
    ) -> Result<PayrollExportPage, PayrollLedgerError> {
        let telemetry = TelemetryService::McpGateway.begin_operation(
            "settlement.close_monthly_settlement",
            Some(actor.actor_id().as_str()),
            None::<&str>,
        );
        let result = self.payroll_ledger_service.close_monthly_settlement(
            actor,
            cycle_key,
            page,
            page_size,
            sort_by,
            sort_order,
            occurred_at,
        );
        telemetry.finish(if result.is_ok() {
            TelemetryOutcome::Success
        } else {
            TelemetryOutcome::Error
        });
        result
    }

    pub fn execute_lock_cycle(
        &self,
        actor: &AuthenticatedActorContext,
        cycle_key: &str,
        reason: impl Into<String>,
        occurred_at: AuditTimestamp,
    ) -> Result<PayrollSettlementLockReceipt, PayrollLedgerError> {
        let telemetry = TelemetryService::McpGateway.begin_operation(
            "settlement.lock_cycle",
            Some(actor.actor_id().as_str()),
            None::<&str>,
        );
        let result = self
            .payroll_ledger_service
            .lock_cycle(actor, cycle_key, reason, occurred_at);
        telemetry.finish(if result.is_ok() {
            TelemetryOutcome::Success
        } else {
            TelemetryOutcome::Error
        });
        result
    }

    pub fn execute_unlock_cycle(
        &self,
        actor: &AuthenticatedActorContext,
        cycle_key: &str,
        reason: impl Into<String>,
        occurred_at: AuditTimestamp,
    ) -> Result<PayrollSettlementLockReceipt, PayrollLedgerError> {
        let telemetry = TelemetryService::McpGateway.begin_operation(
            "settlement.unlock_cycle",
            Some(actor.actor_id().as_str()),
            None::<&str>,
        );
        let result = self.payroll_ledger_service.unlock_cycle_for_recompute(
            actor,
            cycle_key,
            reason,
            occurred_at,
        );
        telemetry.finish(if result.is_ok() {
            TelemetryOutcome::Success
        } else {
            TelemetryOutcome::Error
        });
        result
    }

    pub fn execute_sync_hr_api_adjunct(
        &self,
        actor: &AuthenticatedActorContext,
        batch_id: &PayrollExchangeBatchId,
        outcome: PayrollHrApiSyncOutcome,
        note: Option<String>,
        occurred_at: AuditTimestamp,
    ) -> Result<PayrollExchangeBatch, PayrollLedgerError> {
        let telemetry = TelemetryService::McpGateway.begin_operation(
            "settlement.sync_hr_api_adjunct",
            Some(actor.actor_id().as_str()),
            None::<&str>,
        );
        let result = self.payroll_ledger_service.sync_hr_api_adjunct(
            actor,
            batch_id,
            outcome,
            note,
            occurred_at,
        );
        telemetry.finish(if result.is_ok() {
            TelemetryOutcome::Success
        } else {
            TelemetryOutcome::Error
        });
        result
    }
}

pub struct McpAnomalyExecutionGateway<'a> {
    anomaly_alert_workflow: &'a AnomalyAlertWorkflow,
}

impl<'a> McpAnomalyExecutionGateway<'a> {
    pub fn new(anomaly_alert_workflow: &'a AnomalyAlertWorkflow) -> Self {
        Self {
            anomaly_alert_workflow,
        }
    }

    pub fn execute_list_rules(&self) -> Result<Vec<AnomalyRule>, AnomalyAlertError> {
        let telemetry = TelemetryService::McpGateway.begin_operation(
            "anomaly.list_rules",
            None::<&str>,
            None::<&str>,
        );
        let result = self.anomaly_alert_workflow.list_rules();
        telemetry.finish(if result.is_ok() {
            TelemetryOutcome::Success
        } else {
            TelemetryOutcome::Error
        });
        result
    }

    pub fn execute_upsert_rule(
        &self,
        actor: &AuthenticatedActorContext,
        rule: AnomalyRule,
        occurred_at: AuditTimestamp,
    ) -> Result<AnomalyRule, AnomalyAlertError> {
        let telemetry = TelemetryService::McpGateway.begin_operation(
            "anomaly.upsert_rule",
            Some(actor.actor_id().as_str()),
            None::<&str>,
        );
        let result = self
            .anomaly_alert_workflow
            .upsert_rule(actor, rule, occurred_at);
        telemetry.finish(if result.is_ok() {
            TelemetryOutcome::Success
        } else {
            TelemetryOutcome::Error
        });
        result
    }

    pub fn execute_evaluate_alerts(
        &self,
        actor: &AuthenticatedActorContext,
        snapshot: AnomalySignalSnapshot,
        default_owner_actor_id: &ActorId,
    ) -> Result<AnomalyAlertEvaluationResult, AnomalyAlertError> {
        let telemetry = TelemetryService::McpGateway.begin_operation(
            "anomaly.evaluate_alerts",
            Some(actor.actor_id().as_str()),
            None::<&str>,
        );
        let result =
            self.anomaly_alert_workflow
                .evaluate_rules(actor, snapshot, default_owner_actor_id);
        telemetry.finish(if result.is_ok() {
            TelemetryOutcome::Success
        } else {
            TelemetryOutcome::Error
        });
        result
    }

    pub fn execute_list_alerts(
        &self,
        query: &AnomalyAlertQuery,
        as_of: AuditTimestamp,
    ) -> Result<Vec<AnomalyAlertRecord>, AnomalyAlertError> {
        let telemetry = TelemetryService::McpGateway.begin_operation(
            "anomaly.list_alerts",
            None::<&str>,
            None::<&str>,
        );
        let result = self.anomaly_alert_workflow.query_alerts(query, as_of);
        telemetry.finish(if result.is_ok() {
            TelemetryOutcome::Success
        } else {
            TelemetryOutcome::Error
        });
        result
    }

    pub fn execute_assign_alert_owner(
        &self,
        actor: &AuthenticatedActorContext,
        alert_id: &AnomalyAlertId,
        owner_actor_id: &ActorId,
        occurred_at: AuditTimestamp,
        note: Option<String>,
    ) -> Result<AnomalyAlertRecord, AnomalyAlertError> {
        let telemetry = TelemetryService::McpGateway.begin_operation(
            "anomaly.assign_alert_owner",
            Some(actor.actor_id().as_str()),
            None::<&str>,
        );
        let result = self.anomaly_alert_workflow.assign_owner(
            actor,
            alert_id,
            owner_actor_id,
            occurred_at,
            note,
        );
        telemetry.finish(if result.is_ok() {
            TelemetryOutcome::Success
        } else {
            TelemetryOutcome::Error
        });
        result
    }

    #[allow(clippy::too_many_arguments)]
    pub fn execute_transition_alert(
        &self,
        actor: &AuthenticatedActorContext,
        alert_id: &AnomalyAlertId,
        transition: AnomalyAlertTransition,
        occurred_at: AuditTimestamp,
        note: Option<String>,
        closure_note: Option<String>,
        closure_evidence_refs: Vec<String>,
        ticket_reference: Option<String>,
    ) -> Result<AnomalyAlertRecord, AnomalyAlertError> {
        let telemetry = TelemetryService::McpGateway.begin_operation(
            "anomaly.update_alert_status",
            Some(actor.actor_id().as_str()),
            None::<&str>,
        );
        let result = self.anomaly_alert_workflow.transition_alert(
            actor,
            alert_id,
            transition,
            occurred_at,
            note,
            closure_note,
            closure_evidence_refs,
            ticket_reference,
        );
        telemetry.finish(if result.is_ok() {
            TelemetryOutcome::Success
        } else {
            TelemetryOutcome::Error
        });
        result
    }
}
