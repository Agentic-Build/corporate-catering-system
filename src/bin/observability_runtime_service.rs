use std::cmp::Ordering as CmpOrdering;
use std::collections::{BTreeMap, HashSet};
use std::net::SocketAddr;
use std::path::PathBuf;
use std::sync::atomic::{AtomicU64, Ordering as AtomicOrdering};
use std::sync::{Arc, Mutex};
use std::time::{SystemTime, UNIX_EPOCH};

#[cfg(test)]
use std::collections::BTreeSet;
#[cfg(test)]
use std::sync::{RwLock, RwLockReadGuard};

use aes_gcm::aead::{Aead, KeyInit};
use aes_gcm::{Aes256Gcm, Nonce};
use axum::extract::{Path, Query, State};
use axum::http::{header::AUTHORIZATION, HeaderMap, StatusCode};
use axum::routing::{get, patch, post, put};
use axum::{Json, Router};
use base64::engine::general_purpose::{
    STANDARD as BASE64_STANDARD, URL_SAFE_NO_PAD as BASE64_URL_SAFE_NO_PAD,
};
use base64::Engine as _;
use corporate_catering_system::access::AccessController;
use corporate_catering_system::anomaly_alert::{
    AnomalyAlertError, AnomalyAlertId, AnomalyAlertRecord, AnomalyAlertSeverity,
    AnomalyAlertStatus, AnomalyAlertTraceEvent, AnomalyAlertTransition, AnomalyAlertWorkflow,
    AnomalyAlertWorkflowSnapshot, AnomalyRule, AnomalyRuleId, AnomalyRuleKind,
    AnomalySignalSnapshot, AnomalySlaStatus, AnomalyThresholdComparator,
};
use corporate_catering_system::audit::{
    AuditAction, AuditCorrelationId, AuditEntityRef, AuditEntityType, AuditEvidenceWrite,
    AuditInvestigationFilter, AuditRetentionPolicy, AuditSnapshotEncryptionKey, AuditTimestamp,
    AuditTrailError, ImmutableAuditEvidence, ImmutableAuditTrail, ResponsibilityAttribution,
};
use corporate_catering_system::cache_backbone::{
    RuntimeStateCacheTtls, ValkeyRuntimeStateCache, ANOMALY_ALERT_STATE_KEY,
    DELIVERY_POLICY_STATE_KEY, MENU_SUPPLY_STATE_KEY, OPERATIONS_ANALYTICS_STATE_KEY,
    PAYROLL_LEDGER_STATE_KEY,
};
use corporate_catering_system::event_backbone::{
    EventBackboneConfig, OrderEventBackbone, OrderStateChangedEvent,
};
use corporate_catering_system::health::{evaluate_probe, HealthProbeKind, HealthState};
use corporate_catering_system::identity::{
    ActorId, AuthenticatedActorContext, AuthenticationSource, EmploymentStatus, PlantId,
    PlantScope, Role,
};
use corporate_catering_system::menu_supply_window::{
    EmployeeMenuDiscoveryEntry, MenuHealthTag, MenuImageUrl, MenuItemId, MenuSupplyPolicy,
    MenuSupplyPolicySnapshot, MenuSupplyWindowError, Money, OrderId, OrderLifecycleState,
    OrderLineItemRequest, OrderMutation, OrderRetentionPolicy, OrderSnapshot, SpecialRequest,
    VendorMenuItem, VendorMenuItemDraft,
};
use corporate_catering_system::object_storage::{
    ObjectStorageError, ObjectStorageReference, ObjectStorageUploadPipeline, ObjectUploadIntent,
    PresignedDownloadPlan, PresignedUploadPlan, PresignedUploadTarget, S3ObjectStorageConfig,
    StorageArtifactClass, StorageLocale,
};
use corporate_catering_system::observability::{
    initialize_telemetry_runtime_from_env, TelemetryService,
};
use corporate_catering_system::operations_analytics::{
    OperationsAnalyticsDashboardSnapshot, OperationsAnalyticsQuery, OperationsAnalyticsWarehouse,
    OperationsAnalyticsWarehouseSnapshot,
};
use corporate_catering_system::payroll::{
    HrApiSyncStatus, OrderPayrollView, PayrollDeductionRecord, PayrollDisputeId,
    PayrollDisputeRecord, PayrollDisputeTraceEvent, PayrollExchangeBatch, PayrollExchangeBatchId,
    PayrollExportPage, PayrollHrApiSyncOutcome, PayrollLedgerError, PayrollLedgerService,
    PayrollLedgerServiceSnapshot, PayrollLedgerSourceKind, PayrollLedgerSourceRef,
    PayrollReconciliationMetadata, PayrollRetentionPolicy, PayrollSettlementLockReceipt,
    PayrollSortField as PayrollSortFieldDomain, SortOrder as PayrollSortOrderDomain,
};
use corporate_catering_system::persistence::{
    allocate_order_id_hex_from_postgres, build_operational_pg_pool_from_env,
    JsonStatePersistenceError, OutboxEventRecord, SqlJsonStateRepository,
    VendorCompliancePersistenceError, VendorComplianceSqlRepository,
};
use corporate_catering_system::pickup_totp::{
    PickupTotpVerificationError, PickupTotpVerifier, VerifiedTotp,
};
use corporate_catering_system::rush_reminder::{
    NoopRushReminderDeliveryGateway, RushReminderDeliveryGateway, RushReminderPolicy,
    RushReminderPreferences, RushReminderWorkflow,
};
use corporate_catering_system::transport::http::{
    HttpAuditInvestigationExecutionGateway, HttpEmployeeDiscoveryExecutionGateway,
    HttpOrderExecutionError, HttpOrderingExecutionGateway, HttpVendorMenuExecutionGateway,
};
use corporate_catering_system::transport::mcp::{
    runtime_mcp_resources, runtime_mcp_tools, AuthorizedMcpToolWrite, McpAuthenticationModel,
    McpAuthorizationError, McpAuthorizationGateway, McpServiceAccountGrant, McpShortLivedKeyBridge,
    MCP_TOOL_ANOMALY_EVALUATE_ALERTS, MCP_TOOL_ANOMALY_LIST_ALERTS,
    MCP_TOOL_ANOMALY_UPDATE_ALERT_STATUS, MCP_TOOL_ANOMALY_UPSERT_RULE,
    MCP_TOOL_COMPLIANCE_REVIEW_VENDOR_APPLICATION, MCP_TOOL_COMPLIANCE_RUN_VENDOR_LIFECYCLE,
    MCP_TOOL_ORDERING_CREATE_EMPLOYEE_ORDER, MCP_TOOL_ORDERING_LIST_MENU_DISCOVERY,
    MCP_TOOL_ORDERING_UPDATE_EMPLOYEE_ORDER, MCP_TOOL_SETTLEMENT_CLOSE_MONTHLY_SETTLEMENT,
    MCP_TOOL_SETTLEMENT_EXPORT_PAYROLL_DEDUCTIONS, MCP_TOOL_SETTLEMENT_LOCK_CYCLE,
    MCP_TOOL_SETTLEMENT_QUERY_ORDER_LEDGER, MCP_TOOL_SETTLEMENT_UNLOCK_CYCLE,
    MCP_TOOL_VERIFICATION_VERIFY_PICKUP_TOTP,
};
use corporate_catering_system::vendor_compliance::{
    ComplianceDate, ComplianceDocumentTemplate, DocumentTemplateId, HistoryRetentionPolicy,
    VendorCategory, VendorComplianceError, VendorComplianceLifecycle, VendorComplianceStatus,
    VendorDocumentSubmission, VendorId, VendorReviewDecision,
};
use corporate_catering_system::vendor_delivery_mapping::{
    DeliveryMappingId, DeliveryRuleEffect, PersistedPolicySnapshot, ServiceWindow,
    TaipeiBusinessMoment, VendorPlantDeliveryMapping, VendorPlantDeliveryPolicy,
};
use hmac::{Hmac, Mac};
use serde::de::DeserializeOwned;
use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};
use tokio::runtime::Handle;
use tokio::time::{self, MissedTickBehavior};

const DEFAULT_VENDOR_ID: &str = "ven-load-gate-a";
const DEFAULT_PLANT_ID: &str = "fab-a";
const DEFAULT_MENU_VARIANT_COUNT: u16 = 64;
const DEFAULT_DELIVERY_DAY_OFFSET: i32 = 2;
const DEFAULT_AUDIT_RETENTION_DAYS: u16 = 2555;
const DEFAULT_AUDIT_PURGE_INTERVAL_SECONDS: u64 = 3600;
const DEFAULT_AUDIT_TRAIL_PATH: &str = "ops/state/audit-trail.json";
const DEFAULT_PAYROLL_LEDGER_RETENTION_DAYS: u16 = 365 * 2;
const DEFAULT_PAYROLL_DISPUTE_RETENTION_DAYS: u16 = 365;
const DEFAULT_PAYROLL_EXCHANGE_RETENTION_DAYS: u16 = 365;
const DEFAULT_PAYROLL_PURGE_INTERVAL_SECONDS: u64 = 3600;
const DEFAULT_ORDER_RETENTION_DAYS: u16 = 365;
const DEFAULT_ORDER_PURGE_INTERVAL_SECONDS: u64 = 3600;
const LOAD_GATE_EMPLOYEE_ACTOR_ID: &str = "emp-load-gate";
const LOAD_GATE_COMMITTEE_ACTOR_ID: &str = "committee-load-gate";
const LOAD_GATE_PAYROLL_ACTOR_ID: &str = "payroll-load-gate";
const LOAD_GATE_PAYROLL_DISPUTE_OWNER_ACTOR_ID: &str = "payroll-dispute-owner";
const LOAD_GATE_ANOMALY_ALERT_OWNER_ACTOR_ID: &str = "anomaly-alert-owner";
const PRELAUNCH_TERMINATED_EMPLOYEE_ACTOR_IDS_ENV: &str = "PRELAUNCH_TERMINATED_EMPLOYEE_ACTOR_IDS";
const PRELAUNCH_AUDIT_TRAIL_ENCRYPTION_KEY_ENV: &str = "PRELAUNCH_AUDIT_TRAIL_ENCRYPTION_KEY_HEX";
const PRELAUNCH_PAYROLL_EXPORT_ENCRYPTION_KEY_ENV: &str =
    "PRELAUNCH_PAYROLL_EXPORT_ENCRYPTION_KEY_HEX";
const PRELAUNCH_RECOMMENDATION_ENGINE_ENABLED_ENV: &str = "PRELAUNCH_RECOMMENDATION_ENGINE_ENABLED";
const PRELAUNCH_ADVANCED_ANALYTICS_DASHBOARD_ENABLED_ENV: &str =
    "PRELAUNCH_ADVANCED_ANALYTICS_DASHBOARD_ENABLED";
const PRELAUNCH_RUSH_REMINDER_ENABLED_ENV: &str = "PRELAUNCH_RUSH_REMINDER_ENABLED";
const PRELAUNCH_RUSH_PREORDER_MIN_LEAD_DAYS_ENV: &str = "PRELAUNCH_RUSH_PREORDER_MIN_LEAD_DAYS";
const PRELAUNCH_RUSH_PREORDER_MAX_LEAD_DAYS_ENV: &str = "PRELAUNCH_RUSH_PREORDER_MAX_LEAD_DAYS";
const PRELAUNCH_RUSH_PREORDER_THROTTLE_MINUTES_ENV: &str =
    "PRELAUNCH_RUSH_PREORDER_THROTTLE_MINUTES";
const PRELAUNCH_RUSH_DEMAND_SPIKE_REMAINING_THRESHOLD_ENV: &str =
    "PRELAUNCH_RUSH_DEMAND_SPIKE_REMAINING_THRESHOLD";
const PRELAUNCH_RUSH_DEMAND_SPIKE_THROTTLE_MINUTES_ENV: &str =
    "PRELAUNCH_RUSH_DEMAND_SPIKE_THROTTLE_MINUTES";
const DEFAULT_RUSH_PREORDER_OPEN_MIN_LEAD_DAYS: u16 = 1;
const DEFAULT_RUSH_PREORDER_OPEN_MAX_LEAD_DAYS: u16 = 7;
const DEFAULT_RUSH_PREORDER_OPEN_THROTTLE_MINUTES: u16 = 180;
const DEFAULT_RUSH_DEMAND_SPIKE_REMAINING_THRESHOLD: u16 = 5;
const DEFAULT_RUSH_DEMAND_SPIKE_THROTTLE_MINUTES: u16 = 30;
const MINIO_ENDPOINT_ENV: &str = "MINIO_ENDPOINT";
const MINIO_ROOT_USER_ENV: &str = "MINIO_ROOT_USER";
const MINIO_ROOT_PASSWORD_ENV: &str = "MINIO_ROOT_PASSWORD";
const MINIO_BUCKET_MENU_IMAGES_ENV: &str = "MINIO_BUCKET_MENU_IMAGES";
const MINIO_BUCKET_COMPLIANCE_EVIDENCE_ENV: &str = "MINIO_BUCKET_COMPLIANCE_EVIDENCE";
const MINIO_BUCKET_FULFILLMENT_EXPORTS_ENV: &str = "MINIO_BUCKET_FULFILLMENT_EXPORTS";
const OBJECT_STORAGE_REGION_ENV: &str = "OBJECT_STORAGE_REGION";
const OBJECT_STORAGE_KEY_NAMESPACE_ENV: &str = "OBJECT_STORAGE_KEY_NAMESPACE";
const OBJECT_STORAGE_UPLOAD_TTL_SECONDS_ENV: &str = "OBJECT_STORAGE_UPLOAD_TTL_SECONDS";
const OBJECT_STORAGE_DOWNLOAD_TTL_SECONDS_ENV: &str = "OBJECT_STORAGE_DOWNLOAD_TTL_SECONDS";
const VALKEY_URL_ENV: &str = "VALKEY_URL";
const VALKEY_CACHE_KEY_PREFIX_ENV: &str = "PRELAUNCH_CACHE_KEY_PREFIX";
const DEFAULT_VALKEY_CACHE_KEY_PREFIX: &str = "ccs:runtime-state";
#[cfg(test)]
const DEFAULT_OBJECT_STORAGE_KEY_NAMESPACE: &str = "corporate-catering";
#[cfg(test)]
const DEFAULT_MENU_IMAGE_BUCKET: &str = "menu-assets";
#[cfg(test)]
const DEFAULT_COMPLIANCE_BUCKET: &str = "compliance-evidence";
const DEFAULT_OBJECT_STORAGE_UPLOAD_TTL_SECONDS: u16 = 900;
const DEFAULT_OBJECT_STORAGE_DOWNLOAD_TTL_SECONDS: u16 = 600;
const AUTHORIZATION_BEARER_PREFIX: &str = "Bearer ";
const MCP_OAUTH_SERVICE_ACCOUNT_TOKEN_PREFIX: &str = "mcp-oauth-sa:";
const MCP_OAUTH_SERVICE_ACCOUNT_ISSUER_ENV: &str = "MCP_OAUTH_SERVICE_ACCOUNT_ISSUER";
const MCP_OAUTH_SERVICE_ACCOUNT_AUDIENCE_ENV: &str = "MCP_OAUTH_SERVICE_ACCOUNT_AUDIENCE";
const MCP_OAUTH_SERVICE_ACCOUNT_HS256_SECRET_BASE64_ENV: &str =
    "MCP_OAUTH_SERVICE_ACCOUNT_HS256_SECRET_BASE64";
const MCP_BRIDGE_KEY_REGISTRY_JSON_ENV: &str = "MCP_BRIDGE_KEY_REGISTRY_JSON";
const MCP_BRIDGE_KEY_ID_HEADER: &str = "x-mcp-bridge-key-id";
const MCP_BRIDGE_ISSUED_AT_HEADER: &str = "x-mcp-bridge-issued-at";
const MCP_BRIDGE_EXPIRES_AT_HEADER: &str = "x-mcp-bridge-expires-at";
const MCP_BRIDGE_ROTATED_AT_HEADER: &str = "x-mcp-bridge-rotated-at";
const MCP_BRIDGE_AUDIT_REASON_HEADER: &str = "x-mcp-bridge-audit-reason";
const MAX_AUDIT_REASON_CHARS: usize = 280;
const PAYROLL_FIELD_ENVELOPE_VERSION: &str = "v1";
const PAYROLL_FIELD_NONCE_BYTES: usize = 12;
const DEFAULT_ADVANCED_ANALYTICS_LOOKBACK_DAYS: i32 = 30;
const MAX_ADVANCED_ANALYTICS_RANGE_DAYS: i32 = 366;
const DEFAULT_SEED_TEMPLATE_ID: &str = "tmpl-load-gate-license";
const DEFAULT_SEED_LIFECYCLE_TEMPLATE_ID: &str = "tmpl-load-gate-health-cert";
const DEFAULT_SEED_LIFECYCLE_VENDOR_ID: &str = "ven-load-gate-lifecycle";
const DEFAULT_SEED_LIFECYCLE_ALLOW_MAPPING_ID: &str = "map-load-gate-lifecycle-allow";
const DEFAULT_SEED_DENY_PLANT_ID: &str = "fab-b";
const DEFAULT_SEED_DENY_MAPPING_ID: &str = "map-load-gate-deny-fab-b";
const DEFAULT_SEED_DISPUTE_EMPLOYEE_ACTOR_ID: &str = "emp-seed-dispute";
const DEFAULT_SEED_DISPUTE_ORDER_ID: &str = "ord-seeddispute0001";

static ORDER_EVENT_SEQUENCE: AtomicU64 = AtomicU64::new(1);

const ALL_AUDIT_ACTIONS: [AuditAction; 35] = [
    AuditAction::CreateEmployeeOrder,
    AuditAction::UpdateEmployeeOrder,
    AuditAction::VerifyPickupOrder,
    AuditAction::MarkOrderSoldOut,
    AuditAction::MarkOrderRefundPending,
    AuditAction::MarkOrderRefunded,
    AuditAction::UpsertVendorMenuItem,
    AuditAction::UpsertVendorOrderingPolicy,
    AuditAction::AdvanceVendorFulfillmentDeliveryStatus,
    AuditAction::CreateVendorFulfillmentExportBatch,
    AuditAction::UpsertVendorPlantDeliveryMapping,
    AuditAction::DeleteVendorPlantDeliveryMapping,
    AuditAction::UpsertComplianceDocumentTemplate,
    AuditAction::RegisterVendorApplication,
    AuditAction::SubmitVendorComplianceDocument,
    AuditAction::ReviewVendorApplication,
    AuditAction::RunVendorComplianceLifecycle,
    AuditAction::PurgeAuditEvidence,
    AuditAction::PruneVendorComplianceHistory,
    AuditAction::ExportPayrollDeductions,
    AuditAction::AppendPayrollLedgerEntry,
    AuditAction::OpenPayrollDispute,
    AuditAction::AssignPayrollDisputeOwner,
    AuditAction::ResolvePayrollDispute,
    AuditAction::ExportPayrollSftpBatch,
    AuditAction::LockPayrollSettlementCycle,
    AuditAction::UnlockPayrollSettlementCycle,
    AuditAction::SyncPayrollHrApiAdjunct,
    AuditAction::PurgePayrollData,
    AuditAction::PurgeOrderData,
    AuditAction::UpsertAnomalyDetectionRule,
    AuditAction::TriggerAnomalyAlert,
    AuditAction::AssignAnomalyAlertOwner,
    AuditAction::AdvanceAnomalyAlertStatus,
    AuditAction::CloseAnomalyAlert,
];

const ALL_AUDIT_ENTITY_TYPES: [AuditEntityType; 15] = [
    AuditEntityType::Order,
    AuditEntityType::MenuItem,
    AuditEntityType::Vendor,
    AuditEntityType::DeliveryMapping,
    AuditEntityType::ComplianceDocumentTemplate,
    AuditEntityType::FulfillmentBatch,
    AuditEntityType::Settlement,
    AuditEntityType::VendorOrderingPolicy,
    AuditEntityType::AuditTrail,
    AuditEntityType::PayrollLedgerEntry,
    AuditEntityType::PayrollDispute,
    AuditEntityType::PayrollExchangeBatch,
    AuditEntityType::PayrollDataRetention,
    AuditEntityType::AnomalyRule,
    AuditEntityType::AnomalyAlert,
];

type MenuRecommendationRanker = fn(
    entries: &mut Vec<EmployeeMenuDiscoveryEntry>,
    at: TaipeiBusinessMoment,
    sort_by: MenuSortFieldQuery,
    sort_order: SortOrderQuery,
) -> Result<(), String>;

type ReminderDeliveryGateway = Arc<dyn RushReminderDeliveryGateway + Send + Sync>;

#[derive(Debug, Clone)]
enum CompliancePersistence {
    Sql(Arc<VendorComplianceSqlRepository>),
    #[cfg(test)]
    InMemoryOnly,
}

#[derive(Debug, Clone)]
struct SqlRuntimeStateRepositories {
    menu_supply: SqlJsonStateRepository,
    payroll_ledger: SqlJsonStateRepository,
    anomaly_alert: SqlJsonStateRepository,
    delivery_policy: SqlJsonStateRepository,
    operations_analytics: SqlJsonStateRepository,
}

#[derive(Debug, Clone)]
enum RuntimeStatePersistence {
    Sql(SqlRuntimeStateRepositories),
    #[cfg(test)]
    InMemoryOnly,
}

#[derive(Debug, Clone)]
struct AppState {
    #[cfg(test)]
    next_order_sequence: Arc<AtomicU64>,
    vendor_id: VendorId,
    plant_id: PlantId,
    recommendation_engine_runtime_enabled: bool,
    advanced_analytics_dashboard_runtime_enabled: bool,
    rush_reminder_runtime_enabled: bool,
    menu_recommendation_ranker: MenuRecommendationRanker,
    rush_reminder_workflow: RushReminderWorkflow,
    rush_reminder_delivery_gateway: ReminderDeliveryGateway,
    object_storage_upload_pipeline: Arc<ObjectStorageUploadPipeline>,
    terminated_employee_actor_ids: Arc<HashSet<ActorId>>,
    audit_trail: ImmutableAuditTrail,
    payroll_export_field_encryptor: PayrollExportFieldEncryptor,
    #[cfg(test)]
    compliance_lifecycle: Arc<RwLock<VendorComplianceLifecycle>>,
    compliance_persistence: CompliancePersistence,
    runtime_state_persistence: RuntimeStatePersistence,
    runtime_state_cache: Option<Arc<ValkeyRuntimeStateCache>>,
    runtime_state_cache_bypass_keys: Arc<Mutex<HashSet<&'static str>>>,
    order_event_backbone: Option<Arc<OrderEventBackbone>>,
    pickup_totp_verifier: Arc<PickupTotpVerifier>,
    #[cfg(test)]
    operations_analytics_warehouse: Arc<RwLock<OperationsAnalyticsWarehouse>>,
    #[cfg(test)]
    payroll_ledger_service: PayrollLedgerService,
    #[cfg(test)]
    anomaly_alert_workflow: AnomalyAlertWorkflow,
    #[cfg(test)]
    delivery_policy: Arc<VendorPlantDeliveryPolicy>,
    #[cfg(test)]
    menu_supply_policy: MenuSupplyPolicy,
}

#[derive(Debug, Clone)]
struct PayrollExportFieldEncryptor([u8; 32]);

impl PayrollExportFieldEncryptor {
    fn parse_hex(value: impl AsRef<str>) -> Result<Self, String> {
        let raw = value.as_ref().trim();
        if raw.len() != 64 {
            return Err("encryption key must be exactly 64 hex characters".to_owned());
        }
        if !raw.chars().all(|character| character.is_ascii_hexdigit()) {
            return Err("encryption key must be hexadecimal".to_owned());
        }
        let mut bytes = [0u8; 32];
        for index in 0..32 {
            let hex_slice = &raw[index * 2..index * 2 + 2];
            bytes[index] = u8::from_str_radix(hex_slice, 16)
                .map_err(|error| format!("failed to decode key byte {index}: {error}"))?;
        }
        Ok(Self(bytes))
    }

    fn encrypt_field(&self, context: &str, plaintext: &str) -> Result<String, String> {
        let nonce = self.derive_nonce(context);
        let cipher = Aes256Gcm::new_from_slice(&self.0)
            .map_err(|error| format!("failed to initialize cipher: {error}"))?;
        let ciphertext = cipher
            .encrypt(Nonce::from_slice(&nonce), plaintext.as_bytes())
            .map_err(|error| error.to_string())?;
        Ok(format!(
            "{PAYROLL_FIELD_ENVELOPE_VERSION}:{}:{}",
            BASE64_STANDARD.encode(nonce),
            BASE64_STANDARD.encode(ciphertext)
        ))
    }

    #[cfg(test)]
    fn decrypt_field(&self, envelope: &str) -> Result<String, String> {
        let mut parts = envelope.splitn(3, ':');
        let version = parts.next().ok_or("missing envelope version")?;
        if version != PAYROLL_FIELD_ENVELOPE_VERSION {
            return Err(format!("unsupported envelope version `{version}`"));
        }
        let nonce_b64 = parts.next().ok_or("missing envelope nonce")?;
        let ciphertext_b64 = parts.next().ok_or("missing envelope ciphertext")?;
        let nonce = BASE64_STANDARD
            .decode(nonce_b64.as_bytes())
            .map_err(|error| error.to_string())?;
        if nonce.len() != PAYROLL_FIELD_NONCE_BYTES {
            return Err(format!(
                "envelope nonce must be {PAYROLL_FIELD_NONCE_BYTES} bytes"
            ));
        }
        let ciphertext = BASE64_STANDARD
            .decode(ciphertext_b64.as_bytes())
            .map_err(|error| error.to_string())?;
        let cipher = Aes256Gcm::new_from_slice(&self.0)
            .map_err(|error| format!("failed to initialize cipher: {error}"))?;
        let plaintext = cipher
            .decrypt(Nonce::from_slice(&nonce), ciphertext.as_ref())
            .map_err(|error| error.to_string())?;
        String::from_utf8(plaintext).map_err(|error| error.to_string())
    }

    fn derive_nonce(&self, context: &str) -> [u8; PAYROLL_FIELD_NONCE_BYTES] {
        let mut digest = Sha256::new();
        digest.update(self.0);
        digest.update(context.as_bytes());
        let digest = digest.finalize();
        let mut nonce = [0u8; PAYROLL_FIELD_NONCE_BYTES];
        nonce.copy_from_slice(&digest[..PAYROLL_FIELD_NONCE_BYTES]);
        nonce
    }
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
struct EmployeeOrderCreateRequestPayload {
    plant_id: String,
    delivery_date: String,
    line_items: Vec<OrderLineItemRequestPayload>,
    employee_note: Option<String>,
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
struct OrderLineItemRequestPayload {
    menu_item_id: String,
    quantity: u16,
    #[serde(default)]
    special_requests: Vec<SpecialRequestOption>,
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "SCREAMING_SNAKE_CASE")]
enum SpecialRequestOption {
    LessRice,
    NoGreenOnion,
    SauceOnSide,
    NoUtensils,
    ExtraSpicy,
}

impl SpecialRequestOption {
    const fn into_domain(self) -> SpecialRequest {
        match self {
            Self::LessRice => SpecialRequest::LessRice,
            Self::NoGreenOnion => SpecialRequest::NoGreenOnion,
            Self::SauceOnSide => SpecialRequest::SauceOnSide,
            Self::NoUtensils => SpecialRequest::NoUtensils,
            Self::ExtraSpicy => SpecialRequest::ExtraSpicy,
        }
    }
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
struct EmployeeOrderLineItemPayload {
    menu_item_id: String,
    quantity: u16,
    price_per_unit: MenuPricePayload,
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
struct UpdateOrderRequest {
    operation: String,
    line_items: Option<Vec<OrderLineItemRequestPayload>>,
    cancel_reason: Option<String>,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
struct OrderTimelineEventPayload {
    occurred_at: String,
    event_type: String,
    status: String,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
struct EmployeeOrderPayload {
    order_id: String,
    employee_actor_id: String,
    plant_id: String,
    delivery_date: String,
    status: String,
    line_items: Vec<EmployeeOrderLineItemPayload>,
    total: MenuPricePayload,
    timeline: Vec<OrderTimelineEventPayload>,
    created_at: String,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
struct EmployeeOrderPagePayload {
    items: Vec<EmployeeOrderPayload>,
    page: PageMetaPayload,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
struct VendorOrderBoardEntryPayload {
    order_id: String,
    plant_id: String,
    delivery_date: String,
    status: String,
    line_items: Vec<EmployeeOrderLineItemPayload>,
    timeline: Vec<OrderTimelineEventPayload>,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
struct VendorOrderPagePayload {
    items: Vec<VendorOrderBoardEntryPayload>,
    page: PageMetaPayload,
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
struct EmployeeRushReminderPreferencesUpsertRequest {
    plant_id: String,
    preorder_open_enabled: bool,
    demand_spike_enabled: bool,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
struct EmployeeRushReminderPreferencesPayload {
    employee_actor_id: String,
    plant_id: String,
    preorder_open_enabled: bool,
    demand_spike_enabled: bool,
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
struct PickupVerificationRequest {
    verification_code: String,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
struct PickupVerificationResponse {
    order_id: String,
    verified: bool,
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
struct AuditInvestigationQuery {
    actor_id: Option<String>,
    action: Option<String>,
    entity_type: Option<String>,
    entity_id: Option<String>,
    occurred_from_epoch_day: Option<i32>,
    occurred_to_epoch_day: Option<i32>,
    correlation_id: Option<String>,
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
struct AuditRetentionPurgeRequest {
    as_of_epoch_day: Option<i32>,
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
struct OrderRetentionPurgeRequest {
    as_of_epoch_day: Option<i32>,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
struct AuditInvestigationResponse {
    items: Vec<AuditEvidencePayload>,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
struct AuditEvidencePayload {
    evidence_id: u64,
    occurred_at: String,
    actor_id: String,
    actor_role: String,
    authentication_source: String,
    operation_id: String,
    action: String,
    entity_type: String,
    entity_id: String,
    reason: String,
    correlation_id: String,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
struct AuditResponsibilityResponse {
    items: Vec<AuditResponsibilityPayload>,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
struct AuditResponsibilityPayload {
    actor_id: String,
    role: String,
    authentication_source: String,
    event_count: usize,
    actions: Vec<String>,
    entities: Vec<AuditEntityRefPayload>,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
struct AuditEntityRefPayload {
    entity_type: String,
    entity_id: String,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
struct AuditRetentionPurgeResponse {
    purged_events: usize,
    as_of_epoch_day: i32,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
struct OrderRetentionPurgeResponse {
    purged_orders: usize,
    as_of_epoch_day: i32,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
struct EmployeeOrderPayrollLedgerResponse {
    order_id: String,
    employee_actor_id: String,
    delivery_date: String,
    currency: String,
    net_amount_minor: i64,
    ledger_entries: Vec<PayrollLedgerEntryPayload>,
    disputes: Vec<PayrollDisputePayload>,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
struct PayrollLedgerEntryPayload {
    ledger_entry_id: u64,
    kind: String,
    amount: MenuPricePayload,
    occurred_at: String,
    source_event_kind: String,
    source_event_reference: String,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
struct PayrollDisputePayload {
    dispute_id: String,
    order_id: String,
    employee_actor_id: String,
    owner_actor_id: String,
    status: String,
    opened_at: String,
    updated_at: String,
    trace: Vec<PayrollDisputeTracePayload>,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
struct PayrollDisputeTracePayload {
    occurred_at: String,
    actor_id: String,
    event_type: String,
    status: String,
    owner_actor_id: String,
    note: Option<String>,
    source_event_kind: String,
    source_event_reference: String,
    refund_ledger_entry_id: Option<u64>,
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
struct EmployeePayrollDisputeCreateRequest {
    reason: String,
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
struct VendorApplicationReviewRequest {
    decision: String,
    comment: String,
    decided_on_epoch_day: i32,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
struct VendorApplicationReviewResponse {
    vendor_id: String,
    status: String,
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
struct VendorLifecycleRunRequest {
    run_on_epoch_day: i32,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
struct VendorLifecycleRunResponse {
    reminder_count: usize,
    suspension_count: usize,
    reinstatement_count: usize,
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
struct AdminPayrollDisputePatchRequest {
    operation: String,
    owner_actor_id: Option<String>,
    note: Option<String>,
    refund_amount_minor: Option<u32>,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
struct AnomalyRulePayload {
    rule_id: String,
    kind: String,
    display_name: String,
    description: String,
    governance_issue_id: String,
    enabled: bool,
    threshold_value: f64,
    threshold_comparator: String,
    evaluation_window_days: u16,
    sla_minutes: u32,
    severity: String,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
struct AnomalyRuleListResponse {
    items: Vec<AnomalyRulePayload>,
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
struct AnomalyRuleUpsertRequest {
    kind: String,
    display_name: String,
    description: String,
    governance_issue_id: String,
    enabled: bool,
    threshold_value: f64,
    threshold_comparator: String,
    evaluation_window_days: u16,
    sla_minutes: u32,
    severity: String,
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
struct AnomalyAlertEvaluationRequest {
    vendor_id: String,
    observed_at_epoch_day: Option<i32>,
    observed_at_minute_of_day: Option<u16>,
    days_until_expiry: Option<f64>,
    on_time_rate: Option<f64>,
    satisfaction_score: Option<f64>,
    complaint_count: Option<f64>,
    default_owner_actor_id: Option<String>,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
struct AnomalyAlertTracePayload {
    occurred_at: String,
    actor_id: String,
    event_type: String,
    status: String,
    note: Option<String>,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
struct AnomalyAlertPayload {
    alert_id: String,
    vendor_id: String,
    rule_id: String,
    rule_kind: String,
    rule_display_name: String,
    governance_issue_id: String,
    status: String,
    owner_actor_id: String,
    severity: String,
    observed_value: f64,
    threshold_value: f64,
    threshold_comparator: String,
    observed_at: String,
    opened_at: String,
    updated_at: String,
    sla_due_at: String,
    sla_status: String,
    escalated_at: Option<String>,
    closed_at: Option<String>,
    closure_note: Option<String>,
    closure_evidence_refs: Vec<String>,
    ticket_reference: Option<String>,
    trace: Vec<AnomalyAlertTracePayload>,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
struct AnomalyAlertEvaluationResponse {
    triggered_alerts: Vec<AnomalyAlertPayload>,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
struct AnomalyAlertListResponse {
    items: Vec<AnomalyAlertPayload>,
}

#[derive(Debug, Deserialize, Default)]
#[serde(rename_all = "camelCase")]
struct OperationsAnalyticsDashboardQueryRequest {
    from_epoch_day: Option<i32>,
    to_epoch_day: Option<i32>,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
struct OperationsAnalyticsMetricDefinitionPayload {
    key: String,
    display_name: String,
    unit: String,
    formula: String,
    source: String,
    version: String,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
struct OperationsAnalyticsMetricValuePayload {
    metric_key: String,
    value: f64,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
struct OperationsAnalyticsVendorBreakdownPayload {
    vendor_id: String,
    metrics: Vec<OperationsAnalyticsMetricValuePayload>,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
struct OperationsAnalyticsPlantBreakdownPayload {
    plant_id: String,
    metrics: Vec<OperationsAnalyticsMetricValuePayload>,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
struct OperationsAnalyticsTimeBreakdownPayload {
    epoch_day: i32,
    date: String,
    metrics: Vec<OperationsAnalyticsMetricValuePayload>,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
struct OperationsAnalyticsDashboardPayload {
    metric_schema_version: String,
    generated_at: String,
    from_epoch_day: i32,
    to_epoch_day: i32,
    metric_definitions: Vec<OperationsAnalyticsMetricDefinitionPayload>,
    vendor_breakdown: Vec<OperationsAnalyticsVendorBreakdownPayload>,
    plant_breakdown: Vec<OperationsAnalyticsPlantBreakdownPayload>,
    time_breakdown: Vec<OperationsAnalyticsTimeBreakdownPayload>,
}

#[derive(Debug, Deserialize, Default)]
#[serde(rename_all = "camelCase")]
struct AnomalyAlertQueryRequest {
    vendor_id: Option<String>,
    owner_actor_id: Option<String>,
    status: Option<String>,
    escalated_only: Option<bool>,
    sla_status: Option<String>,
    as_of_epoch_day: Option<i32>,
    as_of_minute_of_day: Option<u16>,
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
struct AdminAnomalyAlertPatchRequest {
    operation: String,
    owner_actor_id: Option<String>,
    note: Option<String>,
    closure_note: Option<String>,
    closure_evidence_refs: Option<Vec<String>>,
    ticket_reference: Option<String>,
}

#[derive(Debug, Deserialize, Clone, Copy)]
#[serde(rename_all = "camelCase")]
enum PayrollSortFieldQuery {
    EmployeeActorId,
    AmountMinor,
    DeliveryDate,
}

impl PayrollSortFieldQuery {
    const fn into_domain(self) -> PayrollSortFieldDomain {
        match self {
            Self::EmployeeActorId => PayrollSortFieldDomain::EmployeeActorId,
            Self::AmountMinor => PayrollSortFieldDomain::AmountMinor,
            Self::DeliveryDate => PayrollSortFieldDomain::DeliveryDate,
        }
    }
}

#[derive(Debug, Deserialize, Default)]
#[serde(rename_all = "camelCase")]
struct PayrollExportQuery {
    pay_period: Option<String>,
    cycle_key: Option<String>,
    page: Option<usize>,
    page_size: Option<usize>,
    sort_by: Option<PayrollSortFieldQuery>,
    sort_order: Option<SortOrderQuery>,
}

#[derive(Debug, Deserialize, Default)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
struct PayrollMonthlySettlementCloseRequest {
    cycle_key: Option<String>,
    page: Option<usize>,
    page_size: Option<usize>,
    sort_by: Option<PayrollSortFieldQuery>,
    sort_order: Option<SortOrderQuery>,
}

#[derive(Debug, Deserialize, Clone, Copy)]
#[serde(rename_all = "SCREAMING_SNAKE_CASE")]
enum PayrollHrApiSyncOutcomePayload {
    Succeeded,
    Failed,
}

impl PayrollHrApiSyncOutcomePayload {
    const fn into_domain(self) -> PayrollHrApiSyncOutcome {
        match self {
            Self::Succeeded => PayrollHrApiSyncOutcome::Succeeded,
            Self::Failed => PayrollHrApiSyncOutcome::Failed,
        }
    }
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
struct PayrollHrApiSyncRequest {
    outcome: PayrollHrApiSyncOutcomePayload,
    note: Option<String>,
}

#[derive(Debug, Deserialize, Default)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
struct PayrollSettlementCycleLockRequest {
    reason: Option<String>,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
struct PayrollDeductionRecordPayload {
    employee_actor_ciphertext: String,
    order_id_ciphertext: String,
    delivery_date: String,
    amount_ciphertext: String,
    pay_period: String,
    status: String,
    dispute_status: Option<String>,
    source_entry_ids: Vec<u64>,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
struct PageMetaPayload {
    page: usize,
    page_size: usize,
    total_items: usize,
    total_pages: usize,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
struct PayrollExchangeBatchPayload {
    batch_id: String,
    pay_period: String,
    cycle_key: String,
    generated_at: String,
    cycle_start_date: String,
    cycle_end_date: String,
    snapshot_checksum: String,
    reconciliation: PayrollReconciliationPayload,
    exchange_path: &'static str,
    hr_api_sync_status: String,
    hr_api_synced_at: Option<String>,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
struct PayrollReconciliationPayload {
    total_records: usize,
    total_amount_minor: u64,
    total_source_entries: usize,
    ready_records: usize,
    locked_records: usize,
    refunded_records: usize,
    disputed_records: usize,
    deduction_failed_records: usize,
    employee_terminated_records: usize,
    required_exception_classes: Vec<String>,
    present_exception_classes: Vec<String>,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
struct PayrollDeductionPagePayload {
    items: Vec<PayrollDeductionRecordPayload>,
    page: PageMetaPayload,
    exchange_batch: PayrollExchangeBatchPayload,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
struct PayrollHrApiSyncResponse {
    exchange_batch: PayrollExchangeBatchPayload,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
struct PayrollSettlementCycleLockResponse {
    settlement_cycle: PayrollSettlementCycleLockPayload,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
struct PayrollSettlementCycleLockPayload {
    cycle_key: String,
    pay_period: String,
    lock_state: String,
    batch_id: String,
    snapshot_checksum: String,
    reason: String,
    changed_at: String,
    actor_id: String,
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
struct PayrollRetentionPurgeRequest {
    as_of_epoch_day: Option<i32>,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
struct PayrollRetentionPurgeResponse {
    purged_ledger_entries: usize,
    purged_disputes: usize,
    purged_exchange_batches: usize,
    as_of_epoch_day: i32,
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
struct MenuPricePayload {
    currency: String,
    amount_minor: u32,
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
struct MenuDiscoveryItem {
    menu_item_id: String,
    vendor_id: String,
    name: String,
    description: String,
    image_url: Option<String>,
    menu_type: String,
    health_tags: Vec<String>,
    price: MenuPricePayload,
    remaining_quantity: u16,
    preorder_open: bool,
    preorder_open_days_ahead: u16,
    modify_cancel_cutoff_minute_of_day: u16,
    delivery_date: String,
    earliest_delivery_date: String,
    latest_delivery_date: String,
    cutoff_date: String,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
struct MenuDiscoveryDay {
    delivery_date: String,
    items: Vec<MenuDiscoveryItem>,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
struct MenuDiscoveryPageMeta {
    page: usize,
    page_size: usize,
    total_items: usize,
    total_pages: usize,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
struct MenuDiscoveryResponse {
    timezone: &'static str,
    view: &'static str,
    recommendation_requested: bool,
    recommendation_applied: bool,
    from_date: String,
    to_date: String,
    days: Vec<MenuDiscoveryDay>,
    items: Vec<MenuDiscoveryItem>,
    page: MenuDiscoveryPageMeta,
}

#[derive(Debug, Deserialize, Clone, Copy)]
#[serde(rename_all = "lowercase")]
enum MenuDiscoveryViewQuery {
    Calendar,
    Week,
}

impl MenuDiscoveryViewQuery {
    const fn as_str(self) -> &'static str {
        match self {
            Self::Calendar => "calendar",
            Self::Week => "week",
        }
    }
}

#[derive(Debug, Deserialize, Clone, Copy)]
#[serde(rename_all = "camelCase")]
enum MenuSortFieldQuery {
    Name,
    PriceMinor,
    RemainingQuantity,
    DeliveryDate,
}

#[derive(Debug, Deserialize, Clone, Copy)]
#[serde(rename_all = "camelCase")]
enum EmployeeOrderSortFieldQuery {
    DeliveryDate,
    Status,
    CreatedAt,
}

#[derive(Debug, Deserialize, Clone, Copy)]
#[serde(rename_all = "camelCase")]
enum VendorOrderSortFieldQuery {
    DeliveryDate,
    PlantId,
    Status,
    CreatedAt,
}

#[derive(Debug, Deserialize, Clone, Copy)]
#[serde(rename_all = "lowercase")]
enum SortOrderQuery {
    Asc,
    Desc,
}

impl SortOrderQuery {
    const fn into_payroll_domain(self) -> PayrollSortOrderDomain {
        match self {
            Self::Asc => PayrollSortOrderDomain::Asc,
            Self::Desc => PayrollSortOrderDomain::Desc,
        }
    }
}

#[derive(Debug, Deserialize, Default)]
#[serde(rename_all = "camelCase")]
struct EmployeeMenuDiscoveryQuery {
    plant_id: Option<String>,
    menu_date: Option<String>,
    from_date: Option<String>,
    to_date: Option<String>,
    view: Option<MenuDiscoveryViewQuery>,
    page: Option<usize>,
    page_size: Option<usize>,
    sort_by: Option<MenuSortFieldQuery>,
    sort_order: Option<SortOrderQuery>,
    search: Option<String>,
    menu_type: Option<String>,
    health_tag: Option<String>,
    price_min_minor: Option<u32>,
    price_max_minor: Option<u32>,
    remaining_quantity: Option<u16>,
}

#[derive(Debug, Deserialize, Default)]
#[serde(rename_all = "camelCase")]
struct EmployeeOrderListQuery {
    plant_id: Option<String>,
    from_date: Option<String>,
    to_date: Option<String>,
    page: Option<usize>,
    page_size: Option<usize>,
    sort_by: Option<EmployeeOrderSortFieldQuery>,
    sort_order: Option<SortOrderQuery>,
    status: Option<String>,
}

#[derive(Debug, Deserialize, Default)]
#[serde(rename_all = "camelCase")]
struct VendorOrderListQuery {
    plant_id: Option<String>,
    from_date: Option<String>,
    to_date: Option<String>,
    page: Option<usize>,
    page_size: Option<usize>,
    sort_by: Option<VendorOrderSortFieldQuery>,
    sort_order: Option<SortOrderQuery>,
    status: Option<String>,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
struct HealthPayload {
    status: &'static str,
    probe: &'static str,
    detail: String,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
struct ErrorPayload {
    code: &'static str,
    message: String,
    request_id: String,
}

impl ErrorPayload {
    fn with_request_id(mut self, request_id: &str) -> Self {
        self.request_id = request_id.to_owned();
        self
    }
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
struct ObjectStorageUploadRequestPayload {
    artifact_class: String,
    file_name: String,
    mime_type: String,
    size_bytes: u64,
    thumbnail_size_bytes: Option<u64>,
    locale: Option<String>,
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
struct ObjectStorageAccessLinkRequestPayload {
    object_ref: String,
    locale: Option<String>,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
struct ObjectStorageUploadTargetPayload {
    object_ref: String,
    upload_url: String,
    upload_expires_at_epoch_seconds: i64,
    required_headers: BTreeMap<String, String>,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
struct ObjectStorageUploadMetadataPayload {
    artifact_class: String,
    file_name: String,
    mime_type: String,
    size_bytes: u64,
    thumbnail_ref: Option<String>,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
struct ObjectStorageUploadPlanPayload {
    primary: ObjectStorageUploadTargetPayload,
    thumbnail: Option<ObjectStorageUploadTargetPayload>,
    metadata: ObjectStorageUploadMetadataPayload,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
struct ObjectStorageAccessLinkPayload {
    object_ref: String,
    download_url: String,
    download_expires_at_epoch_seconds: i64,
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase")]
struct McpServiceAccountJwtClaims {
    iss: String,
    aud: McpJwtAudienceClaim,
    sub: String,
    exp: i64,
    #[serde(default)]
    nbf: Option<i64>,
    #[serde(default)]
    iat: Option<i64>,
    role: String,
    all_plants: bool,
    #[serde(default)]
    plant_ids: Vec<String>,
    allowed_tools: Vec<String>,
}

#[derive(Debug, Deserialize)]
#[serde(untagged)]
enum McpJwtAudienceClaim {
    Single(String),
    Multiple(Vec<String>),
}

impl McpJwtAudienceClaim {
    fn contains(&self, expected_audience: &str) -> bool {
        match self {
            Self::Single(value) => value.trim() == expected_audience,
            Self::Multiple(values) => values.iter().any(|value| value.trim() == expected_audience),
        }
    }
}

#[derive(Debug, Deserialize)]
struct McpJwtHeader {
    alg: String,
    #[serde(default)]
    typ: Option<String>,
}

#[derive(Debug)]
struct McpOAuthServiceAccountVerifierConfig {
    issuer: String,
    audience: String,
    hs256_secret: Vec<u8>,
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
struct McpBridgeKeyRegistryEntry {
    key_id: String,
    issued_at_epoch_seconds: i64,
    expires_at_epoch_seconds: i64,
    rotated_at_epoch_seconds: i64,
    #[serde(default)]
    allowed_service_account_ids: Vec<String>,
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase")]
struct McpToolInvocationRequest {
    #[serde(default)]
    args: serde_json::Value,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
struct McpToolInvocationResponse {
    tool_name: String,
    capability_domain: String,
    risk: String,
    result: serde_json::Value,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
struct McpToolCatalogResponse {
    tools: Vec<McpToolCatalogItem>,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
struct McpToolCatalogItem {
    name: String,
    operation_id: String,
    capability_domain: String,
    risk: String,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
struct McpResourceCatalogResponse {
    resources: Vec<McpResourceCatalogItem>,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
struct McpResourceCatalogItem {
    uri: String,
    capability_domain: String,
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
struct McpOrderingUpdateArgs {
    order_id: String,
    request: UpdateOrderRequest,
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
struct McpVerifyPickupArgs {
    order_id: String,
    request: PickupVerificationRequest,
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
struct McpQueryOrderLedgerArgs {
    order_id: String,
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
struct McpReviewVendorApplicationArgs {
    vendor_id: String,
    decision: String,
    comment: String,
    decided_on_epoch_day: i32,
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
struct McpRunVendorLifecycleArgs {
    run_on_epoch_day: i32,
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
struct McpSettlementCycleArgs {
    cycle_key: String,
    request: PayrollSettlementCycleLockRequest,
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
struct McpUpsertAnomalyRuleArgs {
    rule_id: String,
    request: AnomalyRuleUpsertRequest,
}

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
struct McpUpdateAnomalyAlertArgs {
    alert_id: String,
    request: AdminAnomalyAlertPatchRequest,
}

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error + Send + Sync + 'static>> {
    initialize_telemetry_runtime_from_env(TelemetryService::HttpApi.service_name())?;

    let bind_addr =
        std::env::var("PRELAUNCH_BIND_ADDR").unwrap_or_else(|_| "127.0.0.1:18080".to_owned());
    let socket_addr: SocketAddr = bind_addr.parse()?;

    let vendor_id = VendorId::parse(
        std::env::var("PRELAUNCH_VENDOR_ID").unwrap_or_else(|_| DEFAULT_VENDOR_ID.to_owned()),
    )
    .map_err(|error| format!("PRELAUNCH_VENDOR_ID is invalid: {error}"))?;

    let plant_id = PlantId::parse(
        std::env::var("PRELAUNCH_PLANT_ID").unwrap_or_else(|_| DEFAULT_PLANT_ID.to_owned()),
    )
    .map_err(|error| format!("PRELAUNCH_PLANT_ID is invalid: {error}"))?;

    let menu_variant_count =
        parse_positive_u16_env("PRELAUNCH_MENU_VARIANT_COUNT", DEFAULT_MENU_VARIANT_COUNT)?;
    let recommendation_engine_runtime_enabled =
        parse_bool_env_default_false(PRELAUNCH_RECOMMENDATION_ENGINE_ENABLED_ENV)?;
    let advanced_analytics_dashboard_runtime_enabled =
        parse_bool_env_default_false(PRELAUNCH_ADVANCED_ANALYTICS_DASHBOARD_ENABLED_ENV)?;
    let rush_reminder_runtime_enabled =
        parse_bool_env_default_false(PRELAUNCH_RUSH_REMINDER_ENABLED_ENV)?;
    let rush_reminder_policy = resolve_rush_reminder_policy(rush_reminder_runtime_enabled)?;
    let object_storage_upload_pipeline = Arc::new(
        parse_object_storage_upload_pipeline_from_env()
            .map_err(|error| format!("object storage configuration is invalid: {error}"))?,
    );

    let delivery_epoch_day = resolve_delivery_epoch_day()?;
    let audit_retention_days = parse_positive_u16_env(
        "PRELAUNCH_AUDIT_RETENTION_DAYS",
        DEFAULT_AUDIT_RETENTION_DAYS,
    )?;
    let audit_retention_policy = AuditRetentionPolicy::new(audit_retention_days)
        .map_err(|error| format!("PRELAUNCH_AUDIT_RETENTION_DAYS is invalid: {error}"))?;
    let audit_trail_path = PathBuf::from(
        std::env::var("PRELAUNCH_AUDIT_TRAIL_PATH")
            .unwrap_or_else(|_| DEFAULT_AUDIT_TRAIL_PATH.to_owned()),
    );
    let audit_trail_encryption_key = parse_audit_trail_encryption_key_from_env()?;
    let payroll_export_field_encryptor = parse_payroll_export_encryption_key_from_env()?;
    let audit_trail = ImmutableAuditTrail::with_json_storage(
        audit_trail_path.clone(),
        audit_retention_policy,
        audit_trail_encryption_key,
    )
    .map_err(|error| format!("failed to initialize audit trail storage: {error}"))?;
    let audit_purge_interval_seconds = parse_positive_u64_env(
        "PRELAUNCH_AUDIT_PURGE_INTERVAL_SECONDS",
        DEFAULT_AUDIT_PURGE_INTERVAL_SECONDS,
    )?;
    let payroll_retention_policy = PayrollRetentionPolicy::new(
        parse_positive_u16_env(
            "PRELAUNCH_PAYROLL_LEDGER_RETENTION_DAYS",
            DEFAULT_PAYROLL_LEDGER_RETENTION_DAYS,
        )?,
        parse_positive_u16_env(
            "PRELAUNCH_PAYROLL_DISPUTE_RETENTION_DAYS",
            DEFAULT_PAYROLL_DISPUTE_RETENTION_DAYS,
        )?,
        parse_positive_u16_env(
            "PRELAUNCH_PAYROLL_EXCHANGE_RETENTION_DAYS",
            DEFAULT_PAYROLL_EXCHANGE_RETENTION_DAYS,
        )?,
    )
    .map_err(|error| format!("payroll retention policy is invalid: {error}"))?;
    let payroll_purge_interval_seconds = parse_positive_u64_env(
        "PRELAUNCH_PAYROLL_PURGE_INTERVAL_SECONDS",
        DEFAULT_PAYROLL_PURGE_INTERVAL_SECONDS,
    )?;
    let order_retention_policy = OrderRetentionPolicy::new(parse_positive_u16_env(
        "PRELAUNCH_ORDER_RETENTION_DAYS",
        DEFAULT_ORDER_RETENTION_DAYS,
    )?)
    .map_err(|error| format!("order retention policy is invalid: {error}"))?;
    let order_purge_interval_seconds = parse_positive_u64_env(
        "PRELAUNCH_ORDER_PURGE_INTERVAL_SECONDS",
        DEFAULT_ORDER_PURGE_INTERVAL_SECONDS,
    )?;
    let pickup_totp_verifier = PickupTotpVerifier::from_env("PRELAUNCH_PICKUP_TOTP_SECRET")
        .map(Arc::new)
        .map_err(|error| format!("pickup TOTP verifier initialization failed: {error}"))?;
    let operational_pool = build_operational_pg_pool_from_env()
        .await
        .map_err(|error| format!("PostgreSQL pool configuration is invalid: {error}"))?;
    let runtime_state_cache = Arc::new(
        parse_valkey_runtime_state_cache_from_env()
            .await
            .map_err(|error| format!("Valkey cache backbone configuration is invalid: {error}"))?,
    );
    let order_event_backbone = OrderEventBackbone::connect(
        operational_pool.clone(),
        EventBackboneConfig::from_env().map_err(|error| {
            format!("JetStream event backbone configuration is invalid: {error}")
        })?,
    )
    .await
    .map_err(|error| format!("failed to initialize JetStream event backbone: {error}"))?;
    let compliance_repository =
        Arc::new(VendorComplianceSqlRepository::new(operational_pool.clone()));
    let runtime_state_repositories = SqlRuntimeStateRepositories {
        menu_supply: SqlJsonStateRepository::for_menu_supply(operational_pool.clone()),
        payroll_ledger: SqlJsonStateRepository::for_payroll_ledger(operational_pool.clone()),
        anomaly_alert: SqlJsonStateRepository::for_anomaly_alert(operational_pool.clone()),
        delivery_policy: SqlJsonStateRepository::for_delivery_policy(operational_pool.clone()),
        operations_analytics: SqlJsonStateRepository::for_operations_analytics(
            operational_pool.clone(),
        ),
    };
    let (compliance_lifecycle, include_lifecycle_seed_baseline) =
        load_or_seed_compliance_lifecycle(
            compliance_repository.as_ref(),
            audit_trail.clone(),
            object_storage_upload_pipeline.as_ref(),
            vendor_id.clone(),
            plant_id.clone(),
            delivery_epoch_day,
        )
        .await
        .map_err(|error| format!("failed to initialize compliance persistence: {error}"))?;

    let state = bootstrap_runtime_state(
        audit_trail.clone(),
        vendor_id,
        plant_id,
        delivery_epoch_day,
        menu_variant_count,
        recommendation_engine_runtime_enabled,
        advanced_analytics_dashboard_runtime_enabled,
        rush_reminder_runtime_enabled,
        rush_reminder_policy,
        object_storage_upload_pipeline,
        payroll_retention_policy,
        order_retention_policy,
        payroll_export_field_encryptor,
        pickup_totp_verifier,
        compliance_lifecycle,
        CompliancePersistence::Sql(compliance_repository),
        RuntimeStatePersistence::Sql(runtime_state_repositories),
        Some(runtime_state_cache),
        Some(order_event_backbone.clone()),
        include_lifecycle_seed_baseline,
    )
    .map_err(|error| format!("failed to bootstrap runtime state: {error}"))?;
    order_event_backbone.spawn_background_workers();
    let committee_actor = load_gate_committee_admin_actor().map_err(|(_, error)| {
        format!(
            "failed to load committee actor for purge job: {}",
            error.message
        )
    })?;
    spawn_audit_retention_purge_job(
        audit_trail,
        committee_actor.clone(),
        audit_purge_interval_seconds,
    );
    spawn_payroll_retention_purge_job(
        state.clone(),
        committee_actor.clone(),
        payroll_purge_interval_seconds,
    );
    spawn_order_retention_purge_job(state.clone(), committee_actor, order_purge_interval_seconds);

    let app = Router::new()
        .route("/health/ready", get(ready_probe))
        .route("/health/live", get(live_probe))
        .route("/health/startup", get(startup_probe))
        .route("/api/v1/employee/menus", get(list_employee_menus))
        .route(
            "/api/v1/employee/orders",
            get(list_employee_orders).post(create_employee_order),
        )
        .route(
            "/api/v1/employee/orders/:orderId",
            patch(update_employee_order),
        )
        .route(
            "/api/v1/employee/orders/:orderId/pickup-verifications",
            post(verify_order_pickup),
        )
        .route(
            "/api/v1/employee/orders/:orderId/payroll-ledger",
            get(get_employee_order_payroll_ledger),
        )
        .route(
            "/api/v1/employee/orders/:orderId/disputes",
            post(create_employee_order_dispute),
        )
        .route("/api/v1/vendor/orders", get(list_vendor_orders))
        .route(
            "/api/v1/vendor/object-storage/upload-plans",
            post(create_vendor_object_storage_upload_plan),
        )
        .route(
            "/api/v1/vendor/object-storage/access-links",
            post(create_vendor_object_storage_access_link),
        )
        .route(
            "/api/v1/admin/vendors/:vendorId/reviews",
            post(review_vendor_application),
        )
        .route(
            "/api/v1/admin/object-storage/access-links",
            post(create_admin_object_storage_access_link),
        )
        .route(
            "/api/v1/admin/compliance/lifecycle/executions",
            post(run_vendor_compliance_lifecycle),
        )
        .route(
            "/api/v1/admin/audit/investigations",
            get(query_audit_investigations),
        )
        .route(
            "/api/v1/admin/audit/responsibilities",
            get(query_audit_responsibilities),
        )
        .route(
            "/api/v1/admin/audit/retention-purge",
            post(purge_audit_evidence),
        )
        .route(
            "/api/v1/admin/orders/retention-purge",
            post(purge_order_data),
        )
        .route("/api/v1/admin/anomaly/rules", get(list_anomaly_rules))
        .route(
            "/api/v1/admin/anomaly/rules/:ruleId",
            put(upsert_anomaly_rule),
        )
        .route(
            "/api/v1/admin/anomaly/alerts/evaluations",
            post(evaluate_anomaly_alerts),
        )
        .route("/api/v1/admin/anomaly/alerts", get(list_anomaly_alerts))
        .route(
            "/api/v1/admin/anomaly/alerts/:alertId",
            patch(update_admin_anomaly_alert),
        )
        .route(
            "/api/v1/admin/payroll/disputes/:disputeId",
            patch(update_admin_payroll_dispute),
        )
        .route(
            "/api/v1/admin/payroll/retention-purge",
            post(purge_payroll_data),
        )
        .route(
            "/api/v1/admin/payroll/monthly-settlements/close",
            post(close_payroll_monthly_settlement),
        )
        .route(
            "/api/v1/admin/payroll/monthly-settlements/:cycleKey/lock",
            post(lock_payroll_settlement_cycle),
        )
        .route(
            "/api/v1/admin/payroll/monthly-settlements/:cycleKey/unlock",
            post(unlock_payroll_settlement_cycle),
        )
        .route(
            "/api/v1/integrations/payroll/deductions",
            get(export_payroll_deductions),
        )
        .route(
            "/api/v1/integrations/payroll/sftp-batches/:batchId/hr-api-sync",
            post(sync_payroll_hr_api_adjunct),
        )
        .route("/mcp/v1/tools", get(list_mcp_tools))
        .route("/mcp/v1/resources", get(list_mcp_resources))
        .route("/mcp/v1/tools/:toolName/invoke", post(invoke_mcp_tool));
    let app = if rush_reminder_runtime_enabled {
        app.route(
            "/api/v1/employee/rush-reminder-preferences",
            put(upsert_employee_rush_reminder_preferences),
        )
    } else {
        app
    };
    let app = if advanced_analytics_dashboard_runtime_enabled {
        app.route(
            "/api/v1/admin/analytics/operations-dashboard",
            get(get_admin_operations_analytics_dashboard),
        )
        .route(
            "/api/v1/vendor/analytics/operations-dashboard",
            get(get_vendor_operations_analytics_dashboard),
        )
    } else {
        app
    }
    .with_state(state);

    let listener = tokio::net::TcpListener::bind(socket_addr).await?;
    tracing::info!(bind_addr = %socket_addr, "observability runtime service listening");
    axum::serve(listener, app).await?;
    Ok(())
}

async fn list_mcp_tools(
    State(_state): State<AppState>,
    headers: HeaderMap,
) -> (StatusCode, Json<serde_json::Value>) {
    let telemetry =
        TelemetryService::McpGateway.begin_operation("mcp.list_tools", None::<&str>, None::<&str>);
    let request_id = telemetry.correlation_context().request_id().to_owned();
    let grant = match require_mcp_service_account_grant(&headers) {
        Ok(grant) => grant,
        Err((status, error)) => {
            telemetry.finish_with_http_status(status.as_u16());
            return (
                status,
                Json(
                    serde_json::to_value(error.with_request_id(request_id.as_str()))
                        .expect("mcp tool-list auth error payload serialization should succeed"),
                ),
            );
        }
    };

    let tools = runtime_mcp_tools()
        .iter()
        .filter(|tool| grant.allowed_tool_names().contains(tool.tool_name()))
        .map(|tool| McpToolCatalogItem {
            name: tool.tool_name().to_owned(),
            operation_id: tool.operation_id().to_owned(),
            capability_domain: tool.capability_domain().as_str().to_owned(),
            risk: mcp_tool_risk_label(tool.risk().is_write(), tool.risk().is_high_risk_write())
                .to_owned(),
        })
        .collect::<Vec<_>>();

    telemetry.finish_with_http_status(StatusCode::OK.as_u16());
    (
        StatusCode::OK,
        Json(
            serde_json::to_value(McpToolCatalogResponse { tools })
                .expect("mcp tool-list payload serialization should succeed"),
        ),
    )
}

async fn list_mcp_resources(
    State(_state): State<AppState>,
    headers: HeaderMap,
) -> (StatusCode, Json<serde_json::Value>) {
    let telemetry = TelemetryService::McpGateway.begin_operation(
        "mcp.list_resources",
        None::<&str>,
        None::<&str>,
    );
    let request_id = telemetry.correlation_context().request_id().to_owned();
    let grant = match require_mcp_service_account_grant(&headers) {
        Ok(grant) => grant,
        Err((status, error)) => {
            telemetry.finish_with_http_status(status.as_u16());
            return (
                status,
                Json(
                    serde_json::to_value(error.with_request_id(request_id.as_str())).expect(
                        "mcp resource-list auth error payload serialization should succeed",
                    ),
                ),
            );
        }
    };

    let granted_domains = runtime_mcp_tools()
        .iter()
        .filter(|tool| grant.allowed_tool_names().contains(tool.tool_name()))
        .map(|tool| tool.capability_domain())
        .collect::<HashSet<_>>();
    let resources = runtime_mcp_resources()
        .iter()
        .filter(|resource| granted_domains.contains(&resource.capability_domain()))
        .map(|resource| McpResourceCatalogItem {
            uri: resource.resource_uri().to_owned(),
            capability_domain: resource.capability_domain().as_str().to_owned(),
        })
        .collect::<Vec<_>>();

    telemetry.finish_with_http_status(StatusCode::OK.as_u16());
    (
        StatusCode::OK,
        Json(
            serde_json::to_value(McpResourceCatalogResponse { resources })
                .expect("mcp resource-list payload serialization should succeed"),
        ),
    )
}

async fn invoke_mcp_tool(
    State(state): State<AppState>,
    Path(tool_name): Path<String>,
    headers: HeaderMap,
    Json(request): Json<McpToolInvocationRequest>,
) -> (StatusCode, Json<serde_json::Value>) {
    let telemetry = TelemetryService::McpGateway.begin_operation(
        tool_name.as_str(),
        None::<&str>,
        None::<&str>,
    );
    let request_id = telemetry.correlation_context().request_id().to_owned();
    let normalized_tool_name = tool_name.trim();
    if normalized_tool_name.is_empty() {
        let (status, error) = domain_error(
            StatusCode::BAD_REQUEST,
            "INVALID_MCP_TOOL_NAME",
            "tool name must be non-empty".to_owned(),
        );
        telemetry.finish_with_http_status(status.as_u16());
        return (
            status,
            Json(
                serde_json::to_value(error.with_request_id(request_id.as_str()))
                    .expect("mcp tool error payload serialization should succeed"),
            ),
        );
    }

    let tool = match runtime_mcp_tools()
        .iter()
        .copied()
        .find(|tool| tool.tool_name() == normalized_tool_name)
    {
        Some(tool) => tool,
        None => {
            let (status, error) = domain_error(
                StatusCode::NOT_FOUND,
                "MCP_TOOL_NOT_FOUND",
                format!("MCP tool `{normalized_tool_name}` is not defined"),
            );
            telemetry.finish_with_http_status(status.as_u16());
            return (
                status,
                Json(
                    serde_json::to_value(error.with_request_id(request_id.as_str()))
                        .expect("mcp tool error payload serialization should succeed"),
                ),
            );
        }
    };

    let grant = match require_mcp_service_account_grant(&headers) {
        Ok(grant) => grant,
        Err((status, error)) => {
            telemetry.finish_with_http_status(status.as_u16());
            return (
                status,
                Json(
                    serde_json::to_value(error.with_request_id(request_id.as_str()))
                        .expect("mcp auth error payload serialization should succeed"),
                ),
            );
        }
    };
    let bridge = match parse_optional_mcp_short_lived_bridge(&headers, grant.service_account_id()) {
        Ok(bridge) => bridge,
        Err((status, error)) => {
            telemetry.finish_with_http_status(status.as_u16());
            return (
                status,
                Json(
                    serde_json::to_value(error.with_request_id(request_id.as_str()))
                        .expect("mcp bridge error payload serialization should succeed"),
                ),
            );
        }
    };
    let now_epoch_seconds = match current_epoch_seconds_i64() {
        Ok(value) => value,
        Err((status, error)) => {
            telemetry.finish_with_http_status(status.as_u16());
            return (
                status,
                Json(
                    serde_json::to_value(error.with_request_id(request_id.as_str()))
                        .expect("mcp time error payload serialization should succeed"),
                ),
            );
        }
    };

    let auth_gateway = McpAuthorizationGateway::new(AccessController::with_default_policy());
    let result = if tool.risk().is_write() {
        invoke_mcp_write_tool(
            &state,
            &auth_gateway,
            &grant,
            normalized_tool_name,
            request.args,
            bridge.as_ref(),
            now_epoch_seconds,
            request_id.as_str(),
        )
    } else {
        match auth_gateway.authorize_tool_read(&grant, normalized_tool_name) {
            Ok(_) => invoke_mcp_read_tool(
                &state,
                &grant,
                normalized_tool_name,
                request.args,
                request_id.as_str(),
            ),
            Err(error) => Err(map_mcp_authorization_error(error)),
        }
    };

    match result {
        Ok(result_payload) => {
            telemetry.finish_with_http_status(StatusCode::OK.as_u16());
            (
                StatusCode::OK,
                Json(
                    serde_json::to_value(McpToolInvocationResponse {
                        tool_name: normalized_tool_name.to_owned(),
                        capability_domain: tool.capability_domain().as_str().to_owned(),
                        risk: mcp_tool_risk_label(
                            tool.risk().is_write(),
                            tool.risk().is_high_risk_write(),
                        )
                        .to_owned(),
                        result: result_payload,
                    })
                    .expect("mcp invoke payload serialization should succeed"),
                ),
            )
        }
        Err((status, error)) => {
            telemetry.finish_with_http_status(status.as_u16());
            (
                status,
                Json(
                    serde_json::to_value(error.with_request_id(request_id.as_str()))
                        .expect("mcp invoke error payload serialization should succeed"),
                ),
            )
        }
    }
}

#[allow(clippy::too_many_arguments)]
fn authorize_mcp_write_and_audit(
    state: &AppState,
    auth_gateway: &McpAuthorizationGateway,
    grant: &McpServiceAccountGrant,
    tool_name: &str,
    target_plant: Option<&PlantId>,
    now_epoch_seconds: i64,
    bridge: Option<&McpShortLivedKeyBridge>,
    request_id: &str,
) -> Result<AuthorizedMcpToolWrite, (StatusCode, ErrorPayload)> {
    let authorized = auth_gateway
        .authorize_tool_write(grant, tool_name, target_plant, now_epoch_seconds, bridge)
        .map_err(map_mcp_authorization_error)?;
    append_mcp_write_authorization_audit(state, &authorized, request_id)?;
    Ok(authorized)
}

fn append_mcp_write_authorization_audit(
    state: &AppState,
    authorized: &AuthorizedMcpToolWrite,
    request_id: &str,
) -> Result<(), (StatusCode, ErrorPayload)> {
    let moment = current_taipei_business_moment().map_err(|error| {
        domain_error(
            StatusCode::INTERNAL_SERVER_ERROR,
            "TIME_RESOLUTION_FAILED",
            error,
        )
    })?;
    let occurred_at =
        AuditTimestamp::from_taipei_business_moment(moment.epoch_day(), moment.minute_of_day())
            .map_err(|error| {
                domain_error(
                    StatusCode::INTERNAL_SERVER_ERROR,
                    "MCP_AUTHORIZATION_AUDIT_INTERNAL_ERROR",
                    error.to_string(),
                )
            })?;
    let action = mcp_tool_authorization_audit_action(authorized.tool_name()).ok_or_else(|| {
        domain_error(
            StatusCode::INTERNAL_SERVER_ERROR,
            "MCP_AUTHORIZATION_AUDIT_INTERNAL_ERROR",
            format!(
                "missing MCP authorization audit action mapping for tool `{}`",
                authorized.tool_name()
            ),
        )
    })?;
    let entity = AuditEntityRef::new(
        AuditEntityType::AuditTrail,
        format!("mcp-write-authz:{}", authorized.tool_name()),
    )
    .map_err(|error| map_audit_trail_error(error, "MCP_AUTHORIZATION_AUDIT_INTERNAL_ERROR"))?;
    let correlation_id =
        AuditCorrelationId::parse(format!("mcp-authz:{}:{request_id}", authorized.tool_name()))
            .map_err(|error| {
                map_audit_trail_error(error, "MCP_AUTHORIZATION_AUDIT_INTERNAL_ERROR")
            })?;
    let write = AuditEvidenceWrite::new_with_reason(
        occurred_at,
        authorized.authorized_write().audit_identity().clone(),
        action,
        entity,
        format_mcp_write_authorization_reason(authorized, request_id),
        correlation_id,
    )
    .map_err(|error| map_audit_trail_error(error, "MCP_AUTHORIZATION_AUDIT_INTERNAL_ERROR"))?;
    state
        .audit_trail
        .append(write)
        .map_err(|error| map_audit_trail_error(error, "MCP_AUTHORIZATION_AUDIT_INTERNAL_ERROR"))?;
    Ok(())
}

fn mcp_tool_authorization_audit_action(tool_name: &str) -> Option<AuditAction> {
    match tool_name {
        MCP_TOOL_ORDERING_CREATE_EMPLOYEE_ORDER => Some(AuditAction::CreateEmployeeOrder),
        MCP_TOOL_ORDERING_UPDATE_EMPLOYEE_ORDER => Some(AuditAction::UpdateEmployeeOrder),
        MCP_TOOL_VERIFICATION_VERIFY_PICKUP_TOTP => Some(AuditAction::VerifyPickupOrder),
        MCP_TOOL_COMPLIANCE_REVIEW_VENDOR_APPLICATION => Some(AuditAction::ReviewVendorApplication),
        MCP_TOOL_COMPLIANCE_RUN_VENDOR_LIFECYCLE => Some(AuditAction::RunVendorComplianceLifecycle),
        MCP_TOOL_SETTLEMENT_EXPORT_PAYROLL_DEDUCTIONS => Some(AuditAction::ExportPayrollDeductions),
        MCP_TOOL_SETTLEMENT_CLOSE_MONTHLY_SETTLEMENT => Some(AuditAction::ExportPayrollSftpBatch),
        MCP_TOOL_SETTLEMENT_LOCK_CYCLE => Some(AuditAction::LockPayrollSettlementCycle),
        MCP_TOOL_SETTLEMENT_UNLOCK_CYCLE => Some(AuditAction::UnlockPayrollSettlementCycle),
        MCP_TOOL_ANOMALY_EVALUATE_ALERTS => Some(AuditAction::TriggerAnomalyAlert),
        MCP_TOOL_ANOMALY_UPDATE_ALERT_STATUS => Some(AuditAction::AdvanceAnomalyAlertStatus),
        MCP_TOOL_ANOMALY_UPSERT_RULE => Some(AuditAction::UpsertAnomalyDetectionRule),
        _ => None,
    }
}

fn format_mcp_write_authorization_reason(
    authorized: &AuthorizedMcpToolWrite,
    request_id: &str,
) -> String {
    let mut reason = format!(
        "mcp-write-authz tool={} model={} bridgeKeyId={} bridgeReason={} requestId={}",
        authorized.tool_name(),
        mcp_authentication_model_label(authorized.authentication_model()),
        authorized.bridge_key_id().unwrap_or("none"),
        authorized.bridge_audit_reason().unwrap_or("none"),
        request_id,
    );
    if reason.chars().count() > MAX_AUDIT_REASON_CHARS {
        reason = reason.chars().take(MAX_AUDIT_REASON_CHARS).collect();
    }
    reason
}

fn mcp_authentication_model_label(model: McpAuthenticationModel) -> &'static str {
    match model {
        McpAuthenticationModel::OAuthServiceAccount => "OAUTH_SERVICE_ACCOUNT",
        McpAuthenticationModel::OAuthServiceAccountWithBridgeKey => {
            "OAUTH_SERVICE_ACCOUNT_WITH_BRIDGE_KEY"
        }
    }
}

#[allow(clippy::too_many_arguments)]
fn invoke_mcp_write_tool(
    state: &AppState,
    auth_gateway: &McpAuthorizationGateway,
    grant: &McpServiceAccountGrant,
    tool_name: &str,
    args: serde_json::Value,
    bridge: Option<&McpShortLivedKeyBridge>,
    now_epoch_seconds: i64,
    request_id: &str,
) -> Result<serde_json::Value, (StatusCode, ErrorPayload)> {
    match tool_name {
        MCP_TOOL_ORDERING_CREATE_EMPLOYEE_ORDER => {
            let request = decode_mcp_args::<EmployeeOrderCreateRequestPayload>(args, tool_name)?;
            let target_plant = PlantId::parse(request.plant_id.clone()).map_err(|error| {
                domain_error(
                    StatusCode::BAD_REQUEST,
                    "INVALID_MCP_TOOL_ARGUMENTS",
                    format!("arguments.plantId is invalid: {error}"),
                )
            })?;
            let authorized = authorize_mcp_write_and_audit(
                state,
                auth_gateway,
                grant,
                tool_name,
                Some(&target_plant),
                now_epoch_seconds,
                bridge,
                request_id,
            )?;
            let payload = handle_create_employee_order_for_actor(
                state,
                authorized.actor(),
                authorized
                    .authorized_write()
                    .audit_identity()
                    .operation_id(),
                request,
            )?;
            Ok(serde_json::to_value(payload)
                .expect("mcp ordering create payload serialization should succeed"))
        }
        MCP_TOOL_ORDERING_UPDATE_EMPLOYEE_ORDER => {
            let args = decode_mcp_args::<McpOrderingUpdateArgs>(args, tool_name)?;
            let parsed_order_id = parse_contract_order_id(&args.order_id).map_err(|error| {
                domain_error(
                    StatusCode::BAD_REQUEST,
                    "INVALID_MCP_TOOL_ARGUMENTS",
                    format!("arguments.orderId is invalid: {error}"),
                )
            })?;
            let snapshot = load_order_snapshot_or_not_found(state, &parsed_order_id)?;
            let target_plant = snapshot.plant_id().clone();
            let authorized = authorize_mcp_write_and_audit(
                state,
                auth_gateway,
                grant,
                tool_name,
                Some(&target_plant),
                now_epoch_seconds,
                bridge,
                request_id,
            )?;
            let payload = handle_update_employee_order_for_actor(
                state,
                authorized.actor(),
                authorized
                    .authorized_write()
                    .audit_identity()
                    .operation_id(),
                args.order_id,
                args.request,
            )?;
            Ok(serde_json::to_value(payload)
                .expect("mcp ordering update payload serialization should succeed"))
        }
        MCP_TOOL_VERIFICATION_VERIFY_PICKUP_TOTP => {
            let args = decode_mcp_args::<McpVerifyPickupArgs>(args, tool_name)?;
            let parsed_order_id = parse_contract_order_id(&args.order_id).map_err(|error| {
                domain_error(
                    StatusCode::BAD_REQUEST,
                    "INVALID_MCP_TOOL_ARGUMENTS",
                    format!("arguments.orderId is invalid: {error}"),
                )
            })?;
            let snapshot = load_order_snapshot_or_not_found(state, &parsed_order_id)?;
            let target_plant = snapshot.plant_id().clone();
            let authorized = authorize_mcp_write_and_audit(
                state,
                auth_gateway,
                grant,
                tool_name,
                Some(&target_plant),
                now_epoch_seconds,
                bridge,
                request_id,
            )?;
            let payload = handle_verify_order_pickup_for_actor(
                state,
                authorized.actor(),
                args.order_id,
                args.request,
                request_id,
            )?;
            Ok(serde_json::to_value(payload)
                .expect("mcp pickup verification payload serialization should succeed"))
        }
        MCP_TOOL_COMPLIANCE_REVIEW_VENDOR_APPLICATION => {
            let args = decode_mcp_args::<McpReviewVendorApplicationArgs>(args, tool_name)?;
            let authorized = authorize_mcp_write_and_audit(
                state,
                auth_gateway,
                grant,
                tool_name,
                None,
                now_epoch_seconds,
                bridge,
                request_id,
            )?;
            let payload = handle_review_vendor_application(
                state,
                authorized.actor(),
                args.vendor_id,
                VendorApplicationReviewRequest {
                    decision: args.decision,
                    comment: args.comment,
                    decided_on_epoch_day: args.decided_on_epoch_day,
                },
            )?;
            Ok(serde_json::to_value(payload)
                .expect("mcp compliance review payload serialization should succeed"))
        }
        MCP_TOOL_COMPLIANCE_RUN_VENDOR_LIFECYCLE => {
            let args = decode_mcp_args::<McpRunVendorLifecycleArgs>(args, tool_name)?;
            let authorized = authorize_mcp_write_and_audit(
                state,
                auth_gateway,
                grant,
                tool_name,
                None,
                now_epoch_seconds,
                bridge,
                request_id,
            )?;
            let payload = handle_run_vendor_compliance_lifecycle(
                state,
                authorized.actor(),
                VendorLifecycleRunRequest {
                    run_on_epoch_day: args.run_on_epoch_day,
                },
            )?;
            Ok(serde_json::to_value(payload)
                .expect("mcp compliance lifecycle payload serialization should succeed"))
        }
        MCP_TOOL_SETTLEMENT_EXPORT_PAYROLL_DEDUCTIONS => {
            let query = decode_mcp_args::<PayrollExportQuery>(args, tool_name)?;
            let authorized = authorize_mcp_write_and_audit(
                state,
                auth_gateway,
                grant,
                tool_name,
                None,
                now_epoch_seconds,
                bridge,
                request_id,
            )?;
            let payload = handle_export_payroll_deductions(state, authorized.actor(), query)?;
            Ok(serde_json::to_value(payload)
                .expect("mcp settlement export payload serialization should succeed"))
        }
        MCP_TOOL_SETTLEMENT_CLOSE_MONTHLY_SETTLEMENT => {
            let request =
                decode_optional_mcp_args::<PayrollMonthlySettlementCloseRequest>(args, tool_name)?;
            let authorized = authorize_mcp_write_and_audit(
                state,
                auth_gateway,
                grant,
                tool_name,
                None,
                now_epoch_seconds,
                bridge,
                request_id,
            )?;
            let payload =
                handle_close_payroll_monthly_settlement(state, authorized.actor(), request)?;
            Ok(serde_json::to_value(payload)
                .expect("mcp settlement close payload serialization should succeed"))
        }
        MCP_TOOL_SETTLEMENT_LOCK_CYCLE => {
            let args = decode_mcp_args::<McpSettlementCycleArgs>(args, tool_name)?;
            let authorized = authorize_mcp_write_and_audit(
                state,
                auth_gateway,
                grant,
                tool_name,
                None,
                now_epoch_seconds,
                bridge,
                request_id,
            )?;
            let payload = handle_lock_payroll_settlement_cycle(
                state,
                authorized.actor(),
                args.cycle_key,
                args.request,
            )?;
            Ok(serde_json::to_value(payload)
                .expect("mcp settlement lock payload serialization should succeed"))
        }
        MCP_TOOL_SETTLEMENT_UNLOCK_CYCLE => {
            let args = decode_mcp_args::<McpSettlementCycleArgs>(args, tool_name)?;
            let authorized = authorize_mcp_write_and_audit(
                state,
                auth_gateway,
                grant,
                tool_name,
                None,
                now_epoch_seconds,
                bridge,
                request_id,
            )?;
            let payload = handle_unlock_payroll_settlement_cycle(
                state,
                authorized.actor(),
                args.cycle_key,
                args.request,
            )?;
            Ok(serde_json::to_value(payload)
                .expect("mcp settlement unlock payload serialization should succeed"))
        }
        MCP_TOOL_ANOMALY_EVALUATE_ALERTS => {
            let request = decode_mcp_args::<AnomalyAlertEvaluationRequest>(args, tool_name)?;
            let authorized = authorize_mcp_write_and_audit(
                state,
                auth_gateway,
                grant,
                tool_name,
                None,
                now_epoch_seconds,
                bridge,
                request_id,
            )?;
            let payload = handle_evaluate_anomaly_alerts(state, authorized.actor(), request)?;
            Ok(serde_json::to_value(payload)
                .expect("mcp anomaly evaluation payload serialization should succeed"))
        }
        MCP_TOOL_ANOMALY_UPDATE_ALERT_STATUS => {
            let args = decode_mcp_args::<McpUpdateAnomalyAlertArgs>(args, tool_name)?;
            let authorized = authorize_mcp_write_and_audit(
                state,
                auth_gateway,
                grant,
                tool_name,
                None,
                now_epoch_seconds,
                bridge,
                request_id,
            )?;
            let payload = handle_update_admin_anomaly_alert(
                state,
                authorized.actor(),
                args.alert_id,
                args.request,
            )?;
            Ok(serde_json::to_value(payload)
                .expect("mcp anomaly update payload serialization should succeed"))
        }
        MCP_TOOL_ANOMALY_UPSERT_RULE => {
            let args = decode_mcp_args::<McpUpsertAnomalyRuleArgs>(args, tool_name)?;
            let authorized = authorize_mcp_write_and_audit(
                state,
                auth_gateway,
                grant,
                tool_name,
                None,
                now_epoch_seconds,
                bridge,
                request_id,
            )?;
            let payload =
                handle_upsert_anomaly_rule(state, authorized.actor(), args.rule_id, args.request)?;
            Ok(serde_json::to_value(payload)
                .expect("mcp anomaly upsert payload serialization should succeed"))
        }
        _ => Err(domain_error(
            StatusCode::BAD_REQUEST,
            "INVALID_MCP_TOOL_NAME",
            format!("tool `{tool_name}` is not a writable MCP tool"),
        )),
    }
}

fn invoke_mcp_read_tool(
    state: &AppState,
    grant: &McpServiceAccountGrant,
    tool_name: &str,
    args: serde_json::Value,
    _request_id: &str,
) -> Result<serde_json::Value, (StatusCode, ErrorPayload)> {
    match tool_name {
        MCP_TOOL_ORDERING_LIST_MENU_DISCOVERY => {
            let query = decode_mcp_args::<EmployeeMenuDiscoveryQuery>(args, tool_name)?;
            let payload = handle_list_employee_menus(state, query)?;
            Ok(serde_json::to_value(payload)
                .expect("mcp ordering discovery payload serialization should succeed"))
        }
        MCP_TOOL_SETTLEMENT_QUERY_ORDER_LEDGER => {
            let args = decode_mcp_args::<McpQueryOrderLedgerArgs>(args, tool_name)?;
            let payload = handle_get_employee_order_payroll_ledger_for_actor(
                state,
                grant.actor(),
                args.order_id,
            )?;
            Ok(serde_json::to_value(payload)
                .expect("mcp settlement ledger payload serialization should succeed"))
        }
        MCP_TOOL_ANOMALY_LIST_ALERTS => {
            let query = decode_mcp_args::<AnomalyAlertQueryRequest>(args, tool_name)?;
            let payload = handle_list_anomaly_alerts(state, grant.actor(), query)?;
            Ok(serde_json::to_value(payload)
                .expect("mcp anomaly list payload serialization should succeed"))
        }
        _ => Err(domain_error(
            StatusCode::BAD_REQUEST,
            "INVALID_MCP_TOOL_NAME",
            format!("tool `{tool_name}` is not a read-only MCP tool"),
        )),
    }
}

fn decode_mcp_args<T>(
    args: serde_json::Value,
    tool_name: &str,
) -> Result<T, (StatusCode, ErrorPayload)>
where
    T: serde::de::DeserializeOwned,
{
    serde_json::from_value(args).map_err(|error| {
        domain_error(
            StatusCode::BAD_REQUEST,
            "INVALID_MCP_TOOL_ARGUMENTS",
            format!("arguments for `{tool_name}` are invalid: {error}"),
        )
    })
}

fn decode_optional_mcp_args<T>(
    args: serde_json::Value,
    tool_name: &str,
) -> Result<T, (StatusCode, ErrorPayload)>
where
    T: serde::de::DeserializeOwned + Default,
{
    if args.is_null() {
        return Ok(T::default());
    }
    decode_mcp_args::<T>(args, tool_name)
}

fn require_mcp_service_account_grant(
    headers: &HeaderMap,
) -> Result<McpServiceAccountGrant, (StatusCode, ErrorPayload)> {
    let authorization = headers
        .get(AUTHORIZATION)
        .ok_or_else(|| {
            domain_error(
                StatusCode::UNAUTHORIZED,
                "UNAUTHORIZED",
                "authorization header is required".to_owned(),
            )
        })?
        .to_str()
        .map_err(|_| {
            domain_error(
                StatusCode::UNAUTHORIZED,
                "UNAUTHORIZED",
                "authorization header must be ASCII".to_owned(),
            )
        })?;
    let token = authorization
        .strip_prefix(AUTHORIZATION_BEARER_PREFIX)
        .ok_or_else(|| {
            domain_error(
                StatusCode::UNAUTHORIZED,
                "UNAUTHORIZED",
                "authorization header must use Bearer token".to_owned(),
            )
        })?;
    if token.starts_with(MCP_OAUTH_SERVICE_ACCOUNT_TOKEN_PREFIX) {
        return Err(domain_error(
            StatusCode::UNAUTHORIZED,
            "UNAUTHORIZED",
            "legacy MCP token format is unsupported; provide a signed OAuth JWT bearer token"
                .to_owned(),
        ));
    }
    let now_epoch_seconds = current_epoch_seconds_i64()?;
    let claims = verify_mcp_service_account_oauth_token(token, now_epoch_seconds)?;

    let role = parse_role_label(claims.role.as_str()).ok_or_else(|| {
        domain_error(
            StatusCode::UNAUTHORIZED,
            "UNAUTHORIZED",
            format!(
                "MCP service-account role `{}` is unsupported",
                claims.role.trim()
            ),
        )
    })?;
    let service_account_id = ActorId::parse(claims.sub.as_str()).map_err(|error| {
        domain_error(
            StatusCode::UNAUTHORIZED,
            "UNAUTHORIZED",
            format!("MCP service-account subject is invalid: {error}"),
        )
    })?;
    let plant_scope = if claims.all_plants {
        if !claims.plant_ids.is_empty() {
            return Err(domain_error(
                StatusCode::UNAUTHORIZED,
                "UNAUTHORIZED",
                "MCP service-account token cannot set both allPlants=true and plantIds".to_owned(),
            ));
        }
        PlantScope::all()
    } else {
        let plant_ids = claims
            .plant_ids
            .iter()
            .map(|value| {
                PlantId::parse(value).map_err(|error| {
                    domain_error(
                        StatusCode::UNAUTHORIZED,
                        "UNAUTHORIZED",
                        format!("MCP service-account plant id is invalid: {error}"),
                    )
                })
            })
            .collect::<Result<Vec<_>, _>>()?;
        PlantScope::restricted(plant_ids).map_err(|error| {
            domain_error(
                StatusCode::UNAUTHORIZED,
                "UNAUTHORIZED",
                format!("MCP service-account plant scope is invalid: {error}"),
            )
        })?
    };
    let actor = AuthenticatedActorContext::new(
        service_account_id.clone(),
        role,
        plant_scope,
        AuthenticationSource::OAuthServiceAccount,
    )
    .map_err(|error| {
        domain_error(
            StatusCode::UNAUTHORIZED,
            "UNAUTHORIZED",
            format!("MCP service-account actor context is invalid: {error}"),
        )
    })?;

    McpServiceAccountGrant::new(service_account_id, actor, claims.allowed_tools)
        .map_err(map_mcp_authorization_error)
}

fn verify_mcp_service_account_oauth_token(
    token: &str,
    now_epoch_seconds: i64,
) -> Result<McpServiceAccountJwtClaims, (StatusCode, ErrorPayload)> {
    let config = load_mcp_oauth_service_account_verifier_config()?;
    let mut segments = token.split('.');
    let header_segment = segments.next().ok_or_else(|| {
        domain_error(
            StatusCode::UNAUTHORIZED,
            "UNAUTHORIZED",
            "MCP OAuth bearer token must be a JWT".to_owned(),
        )
    })?;
    let payload_segment = segments.next().ok_or_else(|| {
        domain_error(
            StatusCode::UNAUTHORIZED,
            "UNAUTHORIZED",
            "MCP OAuth bearer token must be a JWT".to_owned(),
        )
    })?;
    let signature_segment = segments.next().ok_or_else(|| {
        domain_error(
            StatusCode::UNAUTHORIZED,
            "UNAUTHORIZED",
            "MCP OAuth bearer token must be a JWT".to_owned(),
        )
    })?;
    if segments.next().is_some() {
        return Err(domain_error(
            StatusCode::UNAUTHORIZED,
            "UNAUTHORIZED",
            "MCP OAuth bearer token must be a JWT".to_owned(),
        ));
    }

    let header_json = decode_url_safe_base64_json(header_segment, "header")?;
    let header: McpJwtHeader = serde_json::from_str(header_json.as_str()).map_err(|error| {
        domain_error(
            StatusCode::UNAUTHORIZED,
            "UNAUTHORIZED",
            format!("MCP OAuth JWT header is invalid: {error}"),
        )
    })?;
    if header.alg.trim() != "HS256" {
        return Err(domain_error(
            StatusCode::UNAUTHORIZED,
            "UNAUTHORIZED",
            format!(
                "MCP OAuth JWT alg `{}` is unsupported; expected HS256",
                header.alg.trim()
            ),
        ));
    }
    if let Some(token_type) = header.typ.as_deref() {
        if !token_type.eq_ignore_ascii_case("JWT") {
            return Err(domain_error(
                StatusCode::UNAUTHORIZED,
                "UNAUTHORIZED",
                format!(
                    "MCP OAuth JWT typ `{}` is unsupported; expected JWT",
                    token_type.trim()
                ),
            ));
        }
    }

    let signing_input = format!("{header_segment}.{payload_segment}");
    let provided_signature = BASE64_URL_SAFE_NO_PAD
        .decode(signature_segment.as_bytes())
        .map_err(|error| {
            domain_error(
                StatusCode::UNAUTHORIZED,
                "UNAUTHORIZED",
                format!("MCP OAuth JWT signature is invalid base64url: {error}"),
            )
        })?;
    type HmacSha256 = Hmac<Sha256>;
    let mut mac =
        <HmacSha256 as Mac>::new_from_slice(config.hs256_secret.as_slice()).map_err(|error| {
            domain_error(
                StatusCode::INTERNAL_SERVER_ERROR,
                "MCP_OAUTH_CONFIGURATION_ERROR",
                format!("MCP OAuth signing key configuration is invalid: {error}"),
            )
        })?;
    mac.update(signing_input.as_bytes());
    mac.verify_slice(provided_signature.as_slice())
        .map_err(|_| {
            domain_error(
                StatusCode::UNAUTHORIZED,
                "UNAUTHORIZED",
                "MCP OAuth JWT signature verification failed".to_owned(),
            )
        })?;

    let claims_json = decode_url_safe_base64_json(payload_segment, "payload")?;
    let claims: McpServiceAccountJwtClaims =
        serde_json::from_str(claims_json.as_str()).map_err(|error| {
            domain_error(
                StatusCode::UNAUTHORIZED,
                "UNAUTHORIZED",
                format!("MCP OAuth JWT payload is invalid: {error}"),
            )
        })?;

    if claims.iss.trim() != config.issuer {
        return Err(domain_error(
            StatusCode::UNAUTHORIZED,
            "UNAUTHORIZED",
            format!(
                "MCP OAuth JWT issuer `{}` does not match expected issuer",
                claims.iss.trim()
            ),
        ));
    }
    if !claims.aud.contains(config.audience.as_str()) {
        return Err(domain_error(
            StatusCode::UNAUTHORIZED,
            "UNAUTHORIZED",
            "MCP OAuth JWT audience does not include the required audience".to_owned(),
        ));
    }
    if claims.exp <= now_epoch_seconds {
        return Err(domain_error(
            StatusCode::UNAUTHORIZED,
            "UNAUTHORIZED",
            format!(
                "MCP OAuth JWT is expired: exp={}, now={}",
                claims.exp, now_epoch_seconds
            ),
        ));
    }
    if let Some(nbf) = claims.nbf {
        if now_epoch_seconds < nbf {
            return Err(domain_error(
                StatusCode::UNAUTHORIZED,
                "UNAUTHORIZED",
                format!("MCP OAuth JWT is not active yet: nbf={nbf}, now={now_epoch_seconds}"),
            ));
        }
    }
    if let Some(iat) = claims.iat {
        if iat > now_epoch_seconds {
            return Err(domain_error(
                StatusCode::UNAUTHORIZED,
                "UNAUTHORIZED",
                format!(
                    "MCP OAuth JWT issued-at is in the future: iat={iat}, now={now_epoch_seconds}"
                ),
            ));
        }
    }

    Ok(claims)
}

fn decode_url_safe_base64_json(
    encoded: &str,
    segment_label: &str,
) -> Result<String, (StatusCode, ErrorPayload)> {
    let raw = BASE64_URL_SAFE_NO_PAD
        .decode(encoded.as_bytes())
        .map_err(|error| {
            domain_error(
                StatusCode::UNAUTHORIZED,
                "UNAUTHORIZED",
                format!("MCP OAuth JWT {segment_label} is invalid base64url: {error}"),
            )
        })?;
    String::from_utf8(raw).map_err(|error| {
        domain_error(
            StatusCode::UNAUTHORIZED,
            "UNAUTHORIZED",
            format!("MCP OAuth JWT {segment_label} is not valid UTF-8: {error}"),
        )
    })
}

fn load_mcp_oauth_service_account_verifier_config(
) -> Result<McpOAuthServiceAccountVerifierConfig, (StatusCode, ErrorPayload)> {
    let issuer = load_non_empty_env(MCP_OAUTH_SERVICE_ACCOUNT_ISSUER_ENV)?;
    let audience = load_non_empty_env(MCP_OAUTH_SERVICE_ACCOUNT_AUDIENCE_ENV)?;
    let hs256_secret_base64 =
        load_non_empty_env(MCP_OAUTH_SERVICE_ACCOUNT_HS256_SECRET_BASE64_ENV)?;
    let hs256_secret = BASE64_STANDARD
        .decode(hs256_secret_base64.as_bytes())
        .map_err(|error| {
            domain_error(
                StatusCode::INTERNAL_SERVER_ERROR,
                "MCP_OAUTH_CONFIGURATION_ERROR",
                format!(
                    "{} must be valid base64: {error}",
                    MCP_OAUTH_SERVICE_ACCOUNT_HS256_SECRET_BASE64_ENV
                ),
            )
        })?;
    if hs256_secret.is_empty() {
        return Err(domain_error(
            StatusCode::INTERNAL_SERVER_ERROR,
            "MCP_OAUTH_CONFIGURATION_ERROR",
            format!(
                "{} must decode to a non-empty key",
                MCP_OAUTH_SERVICE_ACCOUNT_HS256_SECRET_BASE64_ENV
            ),
        ));
    }
    Ok(McpOAuthServiceAccountVerifierConfig {
        issuer,
        audience,
        hs256_secret,
    })
}

fn load_non_empty_env(name: &str) -> Result<String, (StatusCode, ErrorPayload)> {
    let value = std::env::var(name).map_err(|_| {
        domain_error(
            StatusCode::INTERNAL_SERVER_ERROR,
            "MCP_OAUTH_CONFIGURATION_ERROR",
            format!("{name} environment variable is required"),
        )
    })?;
    let trimmed = value.trim();
    if trimmed.is_empty() {
        return Err(domain_error(
            StatusCode::INTERNAL_SERVER_ERROR,
            "MCP_OAUTH_CONFIGURATION_ERROR",
            format!("{name} environment variable must be non-empty"),
        ));
    }
    Ok(trimmed.to_owned())
}

fn parse_optional_mcp_short_lived_bridge(
    headers: &HeaderMap,
    service_account_id: &ActorId,
) -> Result<Option<McpShortLivedKeyBridge>, (StatusCode, ErrorPayload)> {
    let key_id = match headers.get(MCP_BRIDGE_KEY_ID_HEADER) {
        Some(value) => value.to_str().map_err(|_| {
            domain_error(
                StatusCode::UNAUTHORIZED,
                "UNAUTHORIZED",
                format!("{MCP_BRIDGE_KEY_ID_HEADER} must be ASCII"),
            )
        })?,
        None => return Ok(None),
    };

    let issued_at_epoch_seconds =
        parse_required_bridge_i64_header(headers, MCP_BRIDGE_ISSUED_AT_HEADER)?;
    let expires_at_epoch_seconds =
        parse_required_bridge_i64_header(headers, MCP_BRIDGE_EXPIRES_AT_HEADER)?;
    let rotated_at_epoch_seconds =
        parse_required_bridge_i64_header(headers, MCP_BRIDGE_ROTATED_AT_HEADER)?;
    let audit_reason = headers
        .get(MCP_BRIDGE_AUDIT_REASON_HEADER)
        .ok_or_else(|| {
            domain_error(
                StatusCode::UNAUTHORIZED,
                "UNAUTHORIZED",
                format!(
                    "{MCP_BRIDGE_AUDIT_REASON_HEADER} header is required when bridge key is used"
                ),
            )
        })?
        .to_str()
        .map_err(|_| {
            domain_error(
                StatusCode::UNAUTHORIZED,
                "UNAUTHORIZED",
                format!("{MCP_BRIDGE_AUDIT_REASON_HEADER} must be ASCII"),
            )
        })?;

    let bridge = McpShortLivedKeyBridge::new(
        key_id,
        issued_at_epoch_seconds,
        expires_at_epoch_seconds,
        rotated_at_epoch_seconds,
        audit_reason,
    )
    .map_err(map_mcp_authorization_error)?;
    validate_bridge_key_against_registry(&bridge, service_account_id)?;
    Ok(Some(bridge))
}

fn validate_bridge_key_against_registry(
    bridge: &McpShortLivedKeyBridge,
    service_account_id: &ActorId,
) -> Result<(), (StatusCode, ErrorPayload)> {
    let registry_raw = std::env::var(MCP_BRIDGE_KEY_REGISTRY_JSON_ENV).map_err(|_| {
        domain_error(
            StatusCode::UNAUTHORIZED,
            "UNAUTHORIZED",
            format!(
                "{MCP_BRIDGE_KEY_REGISTRY_JSON_ENV} must be configured before MCP bridge keys are accepted"
            ),
        )
    })?;
    let registry: Vec<McpBridgeKeyRegistryEntry> = serde_json::from_str(registry_raw.as_str())
        .map_err(|error| {
            domain_error(
                StatusCode::UNAUTHORIZED,
                "UNAUTHORIZED",
                format!("{MCP_BRIDGE_KEY_REGISTRY_JSON_ENV} must be valid JSON: {error}"),
            )
        })?;
    let key_id = bridge.key_id();
    let entry = registry
        .iter()
        .find(|entry| entry.key_id.trim() == key_id)
        .ok_or_else(|| {
            domain_error(
                StatusCode::UNAUTHORIZED,
                "UNAUTHORIZED",
                format!("MCP bridge key `{key_id}` is not registered"),
            )
        })?;

    if entry.issued_at_epoch_seconds != bridge.issued_at_epoch_seconds()
        || entry.expires_at_epoch_seconds != bridge.expires_at_epoch_seconds()
        || entry.rotated_at_epoch_seconds != bridge.rotated_at_epoch_seconds()
    {
        return Err(domain_error(
            StatusCode::UNAUTHORIZED,
            "UNAUTHORIZED",
            format!("MCP bridge key `{key_id}` metadata does not match server rotation records"),
        ));
    }

    if !entry.allowed_service_account_ids.is_empty() {
        let allowed = entry
            .allowed_service_account_ids
            .iter()
            .map(|value| value.trim())
            .filter(|value| !value.is_empty())
            .collect::<HashSet<_>>();
        if !allowed.contains(service_account_id.as_str()) {
            return Err(domain_error(
                StatusCode::FORBIDDEN,
                "FORBIDDEN",
                format!(
                    "service account {} is not allowed to use MCP bridge key `{key_id}`",
                    service_account_id
                ),
            ));
        }
    }

    Ok(())
}

fn parse_required_bridge_i64_header(
    headers: &HeaderMap,
    header_name: &'static str,
) -> Result<i64, (StatusCode, ErrorPayload)> {
    let raw = headers.get(header_name).ok_or_else(|| {
        domain_error(
            StatusCode::UNAUTHORIZED,
            "UNAUTHORIZED",
            format!("{header_name} header is required when bridge key is used"),
        )
    })?;
    let raw = raw.to_str().map_err(|_| {
        domain_error(
            StatusCode::UNAUTHORIZED,
            "UNAUTHORIZED",
            format!("{header_name} must be ASCII"),
        )
    })?;
    raw.parse::<i64>().map_err(|error| {
        domain_error(
            StatusCode::UNAUTHORIZED,
            "UNAUTHORIZED",
            format!("{header_name} must be an integer timestamp: {error}"),
        )
    })
}

fn current_epoch_seconds_i64() -> Result<i64, (StatusCode, ErrorPayload)> {
    let duration = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map_err(|error| {
            domain_error(
                StatusCode::INTERNAL_SERVER_ERROR,
                "TIME_RESOLUTION_FAILED",
                format!("failed to resolve unix epoch seconds: {error}"),
            )
        })?;
    i64::try_from(duration.as_secs()).map_err(|error| {
        domain_error(
            StatusCode::INTERNAL_SERVER_ERROR,
            "TIME_RESOLUTION_FAILED",
            format!("unix epoch seconds overflow: {error}"),
        )
    })
}

fn map_mcp_authorization_error(error: McpAuthorizationError) -> (StatusCode, ErrorPayload) {
    match error {
        McpAuthorizationError::Authorization(inner) => match inner {
            corporate_catering_system::access::AuthorizationError::RoleNotPermitted { .. }
            | corporate_catering_system::access::AuthorizationError::TargetPlantOutOfScope { .. }
            | corporate_catering_system::access::AuthorizationError::McpOperationActionMismatch {
                ..
            } => domain_error(StatusCode::FORBIDDEN, "FORBIDDEN", inner.to_string()),
            corporate_catering_system::access::AuthorizationError::MissingAuthenticatedActorContext {
                ..
            } => domain_error(StatusCode::UNAUTHORIZED, "UNAUTHORIZED", inner.to_string()),
            _ => domain_error(StatusCode::BAD_REQUEST, "BAD_REQUEST", inner.to_string()),
        },
        McpAuthorizationError::UnknownMcpToolName { .. } => {
            domain_error(StatusCode::NOT_FOUND, "MCP_TOOL_NOT_FOUND", error.to_string())
        }
        McpAuthorizationError::ToolNotGrantedForServiceAccount { .. }
        | McpAuthorizationError::AuthorizedToolMismatch { .. } => {
            domain_error(StatusCode::FORBIDDEN, "FORBIDDEN", error.to_string())
        }
        McpAuthorizationError::ToolRequiresWriteAuthorization { .. } => {
            domain_error(StatusCode::BAD_REQUEST, "BAD_REQUEST", error.to_string())
        }
        McpAuthorizationError::ServiceAccountActorMismatch { .. }
        | McpAuthorizationError::UnsupportedServiceAccountAuthenticationSource { .. }
        | McpAuthorizationError::EmptyServiceAccountToolGrant { .. }
        | McpAuthorizationError::InvalidToolGrantName { .. }
        | McpAuthorizationError::InvalidBridgeKeyId
        | McpAuthorizationError::InvalidBridgeAuditReason
        | McpAuthorizationError::BridgeKeyWindowInvalid { .. }
        | McpAuthorizationError::BridgeKeyIssuedInFuture { .. }
        | McpAuthorizationError::BridgeKeyExpired { .. }
        | McpAuthorizationError::BridgeKeyTtlTooLong { .. }
        | McpAuthorizationError::BridgeKeyRotatedInFuture { .. }
        | McpAuthorizationError::BridgeKeyRotationStale { .. } => {
            domain_error(StatusCode::UNAUTHORIZED, "UNAUTHORIZED", error.to_string())
        }
    }
}

fn mcp_tool_risk_label(is_write: bool, is_high_risk_write: bool) -> &'static str {
    if is_high_risk_write {
        "HIGH_RISK_WRITE"
    } else if is_write {
        "WRITE"
    } else {
        "READ_ONLY"
    }
}

fn parse_vendor_review_decision(value: &str) -> Option<VendorReviewDecision> {
    match value.trim().to_ascii_uppercase().as_str() {
        "APPROVED" => Some(VendorReviewDecision::Approved),
        "REJECTED" => Some(VendorReviewDecision::Rejected),
        "REQUEST_FIX" => Some(VendorReviewDecision::RequestFix),
        _ => None,
    }
}

fn vendor_compliance_status_label(status: VendorComplianceStatus) -> &'static str {
    match status {
        VendorComplianceStatus::PendingReview => "PENDING_REVIEW",
        VendorComplianceStatus::FixRequested => "FIX_REQUESTED",
        VendorComplianceStatus::Active => "ACTIVE",
        VendorComplianceStatus::Rejected => "REJECTED",
        VendorComplianceStatus::Suspended => "SUSPENDED",
    }
}

fn map_vendor_compliance_error(error: VendorComplianceError) -> (StatusCode, ErrorPayload) {
    match error {
        VendorComplianceError::UnauthorizedRole { .. } => {
            domain_error(StatusCode::FORBIDDEN, "FORBIDDEN", error.to_string())
        }
        VendorComplianceError::VendorNotFound(_) => {
            domain_error(StatusCode::NOT_FOUND, "NOT_FOUND", error.to_string())
        }
        VendorComplianceError::VendorAlreadyExists(_)
        | VendorComplianceError::ApprovalBlockedByComplianceGap(_) => {
            domain_error(StatusCode::CONFLICT, "CONFLICT", error.to_string())
        }
        VendorComplianceError::AuditTrail(_)
        | VendorComplianceError::PersistenceDataCorrupted(_) => domain_error(
            StatusCode::INTERNAL_SERVER_ERROR,
            "VENDOR_COMPLIANCE_INTERNAL_ERROR",
            error.to_string(),
        ),
        _ => domain_error(StatusCode::BAD_REQUEST, "BAD_REQUEST", error.to_string()),
    }
}

fn map_vendor_compliance_persistence_error(
    error: VendorCompliancePersistenceError,
) -> (StatusCode, ErrorPayload) {
    match error {
        VendorCompliancePersistenceError::Domain(domain_error_value) => {
            map_vendor_compliance_error(domain_error_value)
        }
        VendorCompliancePersistenceError::Sqlx(_)
        | VendorCompliancePersistenceError::Serialize(_) => domain_error(
            StatusCode::INTERNAL_SERVER_ERROR,
            "VENDOR_COMPLIANCE_PERSISTENCE_ERROR",
            error.to_string(),
        ),
    }
}

#[cfg(test)]
fn read_compliance_lifecycle<'a>(
    state: &'a AppState,
) -> Result<RwLockReadGuard<'a, VendorComplianceLifecycle>, (StatusCode, ErrorPayload)> {
    state.compliance_lifecycle.read().map_err(|_| {
        domain_error(
            StatusCode::INTERNAL_SERVER_ERROR,
            "VENDOR_COMPLIANCE_INTERNAL_ERROR",
            "compliance lifecycle state lock is poisoned".to_owned(),
        )
    })
}

fn load_compliance_lifecycle_snapshot(
    state: &AppState,
) -> Result<VendorComplianceLifecycle, (StatusCode, ErrorPayload)> {
    match &state.compliance_persistence {
        CompliancePersistence::Sql(repository) => tokio::task::block_in_place(|| {
            Handle::current().block_on(
                repository
                    .load_lifecycle(HistoryRetentionPolicy::default(), state.audit_trail.clone()),
            )
        })
        .map_err(map_vendor_compliance_persistence_error)?
        .ok_or_else(|| {
            domain_error(
                StatusCode::INTERNAL_SERVER_ERROR,
                "VENDOR_COMPLIANCE_PERSISTENCE_ERROR",
                "compliance lifecycle state is uninitialized".to_owned(),
            )
        }),
        #[cfg(test)]
        CompliancePersistence::InMemoryOnly => {
            let lifecycle = read_compliance_lifecycle(state)?;
            Ok(lifecycle.clone())
        }
    }
}

fn mutate_compliance_lifecycle<T, F>(
    state: &AppState,
    mutator: F,
) -> Result<T, (StatusCode, ErrorPayload)>
where
    F: FnOnce(&mut VendorComplianceLifecycle) -> Result<T, VendorComplianceError>,
{
    let mut mutator = Some(mutator);
    match &state.compliance_persistence {
        CompliancePersistence::Sql(repository) => {
            let mutator = mutator
                .take()
                .expect("compliance lifecycle mutator should be present");
            let persistence_result = tokio::task::block_in_place(|| {
                tokio::runtime::Handle::current().block_on(repository.mutate_lifecycle(
                    HistoryRetentionPolicy::default(),
                    state.audit_trail.clone(),
                    mutator,
                ))
            });
            let (_latest_lifecycle, value) =
                persistence_result.map_err(map_vendor_compliance_persistence_error)?;
            Ok(value)
        }
        #[cfg(test)]
        CompliancePersistence::InMemoryOnly => {
            let mut lifecycle = state.compliance_lifecycle.write().map_err(|_| {
                domain_error(
                    StatusCode::INTERNAL_SERVER_ERROR,
                    "VENDOR_COMPLIANCE_INTERNAL_ERROR",
                    "compliance lifecycle state lock is poisoned".to_owned(),
                )
            })?;
            let mutator = mutator
                .take()
                .expect("compliance lifecycle mutator should be present");
            mutator(&mut lifecycle).map_err(map_vendor_compliance_error)
        }
    }
}

fn load_runtime_state_snapshot_from_cache<T>(state: &AppState, state_key: &'static str) -> Option<T>
where
    T: DeserializeOwned,
{
    let bypass_guard = state.runtime_state_cache_bypass_keys.lock();
    match bypass_guard {
        Ok(guard) => {
            if guard.contains(state_key) {
                tracing::warn!(
                    state_key = state_key,
                    "Valkey cache bypass is active for state key; loading authoritative SQL snapshot"
                );
                return None;
            }
        }
        Err(_poisoned) => {
            tracing::warn!(
                state_key = state_key,
                "cache bypass lock is poisoned; loading authoritative SQL snapshot"
            );
            return None;
        }
    }
    let cache = state.runtime_state_cache.as_ref()?;
    let load_result = tokio::task::block_in_place(|| {
        Handle::current().block_on(cache.load_snapshot::<T>(state_key))
    });
    match load_result {
        Ok(snapshot) => snapshot,
        Err(error) => {
            tracing::warn!(
                error = %error,
                state_key = state_key,
                "Valkey cache read failed; falling back to PostgreSQL snapshot"
            );
            None
        }
    }
}

fn write_runtime_state_snapshot_to_cache<T>(state: &AppState, state_key: &'static str, snapshot: &T)
where
    T: Serialize,
{
    let Some(cache) = state.runtime_state_cache.as_ref() else {
        return;
    };
    let write_result = tokio::task::block_in_place(|| {
        Handle::current().block_on(cache.write_through_snapshot(state_key, snapshot))
    });
    match write_result {
        Ok(()) => match state.runtime_state_cache_bypass_keys.lock() {
            Ok(mut bypass_keys) => {
                if bypass_keys.remove(state_key) {
                    tracing::info!(
                        state_key = state_key,
                        "Valkey cache bypass cleared after successful write-through"
                    );
                }
            }
            Err(_poisoned) => {
                tracing::warn!(
                    state_key = state_key,
                    "cache bypass lock is poisoned after successful write-through"
                );
            }
        },
        Err(error) => {
            match state.runtime_state_cache_bypass_keys.lock() {
                Ok(mut bypass_keys) => {
                    bypass_keys.insert(state_key);
                }
                Err(_poisoned) => {
                    tracing::warn!(
                        state_key = state_key,
                        "cache bypass lock is poisoned while recording write-through failure"
                    );
                }
            }
            tracing::warn!(
                error = %error,
                state_key = state_key,
                "Valkey cache write-through invalidation failed; enabling SQL-authoritative bypass for this key"
            );
        }
    }
}

fn with_delivery_policy<T, F>(state: &AppState, reader: F) -> Result<T, (StatusCode, ErrorPayload)>
where
    F: FnOnce(&VendorPlantDeliveryPolicy) -> Result<T, (StatusCode, ErrorPayload)>,
{
    match &state.runtime_state_persistence {
        RuntimeStatePersistence::Sql(repositories) => {
            let snapshot = tokio::task::block_in_place(|| {
                Handle::current().block_on(
                    repositories
                        .delivery_policy
                        .load_snapshot::<PersistedPolicySnapshot>(),
                )
            })
            .map_err(|error| {
                domain_error(
                    StatusCode::INTERNAL_SERVER_ERROR,
                    "ORDER_POLICY_VIOLATION",
                    format!("failed to load delivery policy state from SQL: {error}"),
                )
            })?
            .ok_or_else(|| {
                domain_error(
                    StatusCode::INTERNAL_SERVER_ERROR,
                    "ORDER_POLICY_VIOLATION",
                    "delivery policy state is uninitialized".to_owned(),
                )
            })?;
            write_runtime_state_snapshot_to_cache(state, DELIVERY_POLICY_STATE_KEY, &snapshot);
            let delivery_policy =
                VendorPlantDeliveryPolicy::from_snapshot(snapshot, state.audit_trail.clone())
                    .map_err(|error| {
                        domain_error(
                            StatusCode::INTERNAL_SERVER_ERROR,
                            "ORDER_POLICY_VIOLATION",
                            format!("failed to restore delivery policy state: {error}"),
                        )
                    })?;
            reader(&delivery_policy)
        }
        #[cfg(test)]
        RuntimeStatePersistence::InMemoryOnly => reader(state.delivery_policy.as_ref()),
    }
}

fn with_menu_supply_policy<T, F>(
    state: &AppState,
    reader: F,
) -> Result<T, (StatusCode, ErrorPayload)>
where
    F: FnOnce(&MenuSupplyPolicy) -> Result<T, (StatusCode, ErrorPayload)>,
{
    match &state.runtime_state_persistence {
        RuntimeStatePersistence::Sql(repositories) => {
            let snapshot = tokio::task::block_in_place(|| {
                Handle::current().block_on(
                    repositories
                        .menu_supply
                        .load_snapshot::<MenuSupplyPolicySnapshot>(),
                )
            })
            .map_err(|error| {
                domain_error(
                    StatusCode::INTERNAL_SERVER_ERROR,
                    "ORDER_POLICY_VIOLATION",
                    format!("failed to load menu supply state from SQL: {error}"),
                )
            })?
            .ok_or_else(|| {
                domain_error(
                    StatusCode::INTERNAL_SERVER_ERROR,
                    "ORDER_POLICY_VIOLATION",
                    "menu supply state is uninitialized".to_owned(),
                )
            })?;
            write_runtime_state_snapshot_to_cache(state, MENU_SUPPLY_STATE_KEY, &snapshot);
            let menu_supply_policy =
                MenuSupplyPolicy::from_snapshot(snapshot, state.audit_trail.clone()).map_err(
                    |error| {
                        domain_error(
                            StatusCode::INTERNAL_SERVER_ERROR,
                            "ORDER_POLICY_VIOLATION",
                            format!("failed to restore menu supply state: {error}"),
                        )
                    },
                )?;
            reader(&menu_supply_policy)
        }
        #[cfg(test)]
        RuntimeStatePersistence::InMemoryOnly => reader(&state.menu_supply_policy),
    }
}

fn mutate_menu_supply_policy<T, F, M>(
    state: &AppState,
    mutator: F,
    map_domain_error: M,
    persistence_error_code: &'static str,
) -> Result<T, (StatusCode, ErrorPayload)>
where
    F: FnOnce(&MenuSupplyPolicy) -> Result<T, MenuSupplyWindowError>,
    M: Fn(MenuSupplyWindowError) -> (StatusCode, ErrorPayload),
{
    match &state.runtime_state_persistence {
        RuntimeStatePersistence::Sql(repositories) => {
            let audit_trail = state.audit_trail.clone();
            let persistence_result =
                tokio::task::block_in_place(|| {
                    Handle::current().block_on(repositories.menu_supply.mutate_snapshot::<
                    MenuSupplyPolicySnapshot,
                    T,
                    MenuSupplyWindowError,
                    _,
                >(move |snapshot| {
                    let snapshot = snapshot.ok_or(MenuSupplyWindowError::StatePoisoned)?;
                    let menu_supply_policy =
                        MenuSupplyPolicy::from_snapshot(snapshot, audit_trail)?;
                    let value = mutator(&menu_supply_policy)?;
                    let snapshot = menu_supply_policy.snapshot()?;
                    Ok((snapshot, value))
                }))
                });
            match persistence_result {
                Ok((snapshot, value)) => {
                    write_runtime_state_snapshot_to_cache(state, MENU_SUPPLY_STATE_KEY, &snapshot);
                    Ok(value)
                }
                Err(JsonStatePersistenceError::Domain(error)) => Err(map_domain_error(error)),
                Err(JsonStatePersistenceError::Sqlx(error)) => Err(domain_error(
                    StatusCode::INTERNAL_SERVER_ERROR,
                    persistence_error_code,
                    format!("failed to persist menu supply state: {error}"),
                )),
                Err(JsonStatePersistenceError::Serialize(error)) => Err(domain_error(
                    StatusCode::INTERNAL_SERVER_ERROR,
                    persistence_error_code,
                    format!("failed to serialize menu supply state: {error}"),
                )),
            }
        }
        #[cfg(test)]
        RuntimeStatePersistence::InMemoryOnly => {
            mutator(&state.menu_supply_policy).map_err(map_domain_error)
        }
    }
}

fn mutate_menu_supply_policy_with_outbox<T, F, M>(
    state: &AppState,
    mutator: F,
    map_domain_error: M,
    persistence_error_code: &'static str,
) -> Result<T, (StatusCode, ErrorPayload)>
where
    F: FnOnce(&MenuSupplyPolicy) -> Result<(T, Vec<OutboxEventRecord>), MenuSupplyWindowError>,
    M: Fn(MenuSupplyWindowError) -> (StatusCode, ErrorPayload),
{
    match &state.runtime_state_persistence {
        RuntimeStatePersistence::Sql(repositories) => {
            let audit_trail = state.audit_trail.clone();
            let persistence_result = tokio::task::block_in_place(|| {
                Handle::current().block_on(repositories.menu_supply.mutate_snapshot_with_outbox::<
                    MenuSupplyPolicySnapshot,
                    T,
                    MenuSupplyWindowError,
                    _,
                >(move |snapshot| {
                    let snapshot = snapshot.ok_or(MenuSupplyWindowError::StatePoisoned)?;
                    let menu_supply_policy = MenuSupplyPolicy::from_snapshot(snapshot, audit_trail)?;
                    let (value, outbox_events) = mutator(&menu_supply_policy)?;
                    let snapshot = menu_supply_policy.snapshot()?;
                    Ok((snapshot, value, outbox_events))
                }))
            });
            match persistence_result {
                Ok((snapshot, value)) => {
                    write_runtime_state_snapshot_to_cache(state, MENU_SUPPLY_STATE_KEY, &snapshot);
                    Ok(value)
                }
                Err(JsonStatePersistenceError::Domain(error)) => Err(map_domain_error(error)),
                Err(JsonStatePersistenceError::Sqlx(error)) => Err(domain_error(
                    StatusCode::INTERNAL_SERVER_ERROR,
                    persistence_error_code,
                    format!("failed to persist menu supply state: {error}"),
                )),
                Err(JsonStatePersistenceError::Serialize(error)) => Err(domain_error(
                    StatusCode::INTERNAL_SERVER_ERROR,
                    persistence_error_code,
                    format!("failed to serialize menu supply state: {error}"),
                )),
            }
        }
        #[cfg(test)]
        RuntimeStatePersistence::InMemoryOnly => {
            let (value, _outbox_events) =
                mutator(&state.menu_supply_policy).map_err(map_domain_error)?;
            Ok(value)
        }
    }
}

fn mutate_ordering_menu_supply_policy<T, F>(
    state: &AppState,
    mutator: F,
) -> Result<T, (StatusCode, ErrorPayload)>
where
    F: FnOnce(&MenuSupplyPolicy) -> Result<(T, Vec<OutboxEventRecord>), HttpOrderExecutionError>,
{
    match &state.runtime_state_persistence {
        RuntimeStatePersistence::Sql(repositories) => {
            let audit_trail = state.audit_trail.clone();
            let persistence_result = tokio::task::block_in_place(|| {
                Handle::current().block_on(repositories.menu_supply.mutate_snapshot_with_outbox::<
                    MenuSupplyPolicySnapshot,
                    T,
                    HttpOrderExecutionError,
                    _,
                >(move |snapshot| {
                    let snapshot = snapshot.ok_or_else(|| {
                        HttpOrderExecutionError::MenuSupply(MenuSupplyWindowError::StatePoisoned)
                    })?;
                    let menu_supply_policy = MenuSupplyPolicy::from_snapshot(snapshot, audit_trail)
                        .map_err(HttpOrderExecutionError::MenuSupply)?;
                    let (value, outbox_events) = mutator(&menu_supply_policy)?;
                    let snapshot = menu_supply_policy
                        .snapshot()
                        .map_err(HttpOrderExecutionError::MenuSupply)?;
                    Ok((snapshot, value, outbox_events))
                }))
            });
            match persistence_result {
                Ok((snapshot, value)) => {
                    write_runtime_state_snapshot_to_cache(state, MENU_SUPPLY_STATE_KEY, &snapshot);
                    Ok(value)
                }
                Err(JsonStatePersistenceError::Domain(error)) => {
                    Err(map_http_order_execution_error(error))
                }
                Err(JsonStatePersistenceError::Sqlx(error)) => Err(domain_error(
                    StatusCode::INTERNAL_SERVER_ERROR,
                    "ORDER_POLICY_VIOLATION",
                    format!("failed to persist ordering state: {error}"),
                )),
                Err(JsonStatePersistenceError::Serialize(error)) => Err(domain_error(
                    StatusCode::INTERNAL_SERVER_ERROR,
                    "ORDER_POLICY_VIOLATION",
                    format!("failed to serialize ordering state: {error}"),
                )),
            }
        }
        #[cfg(test)]
        RuntimeStatePersistence::InMemoryOnly => {
            let (value, _outbox_events) =
                mutator(&state.menu_supply_policy).map_err(map_http_order_execution_error)?;
            Ok(value)
        }
    }
}

fn with_payroll_ledger_service<T, F>(
    state: &AppState,
    reader: F,
) -> Result<T, (StatusCode, ErrorPayload)>
where
    F: FnOnce(&PayrollLedgerService) -> Result<T, PayrollLedgerError>,
{
    match &state.runtime_state_persistence {
        RuntimeStatePersistence::Sql(repositories) => {
            let snapshot = match load_runtime_state_snapshot_from_cache::<
                PayrollLedgerServiceSnapshot,
            >(state, PAYROLL_LEDGER_STATE_KEY)
            {
                Some(snapshot) => snapshot,
                None => {
                    let snapshot = tokio::task::block_in_place(|| {
                        Handle::current().block_on(
                            repositories
                                .payroll_ledger
                                .load_snapshot::<PayrollLedgerServiceSnapshot>(),
                        )
                    })
                    .map_err(|error| {
                        domain_error(
                            StatusCode::INTERNAL_SERVER_ERROR,
                            "PAYROLL_LEDGER_INTERNAL_ERROR",
                            format!("failed to load payroll ledger state from SQL: {error}"),
                        )
                    })?
                    .ok_or_else(|| {
                        domain_error(
                            StatusCode::INTERNAL_SERVER_ERROR,
                            "PAYROLL_LEDGER_INTERNAL_ERROR",
                            "payroll ledger state is uninitialized".to_owned(),
                        )
                    })?;
                    write_runtime_state_snapshot_to_cache(
                        state,
                        PAYROLL_LEDGER_STATE_KEY,
                        &snapshot,
                    );
                    snapshot
                }
            };
            let payroll_ledger_service =
                PayrollLedgerService::from_snapshot(snapshot, state.audit_trail.clone());
            reader(&payroll_ledger_service).map_err(map_payroll_ledger_error)
        }
        #[cfg(test)]
        RuntimeStatePersistence::InMemoryOnly => {
            reader(&state.payroll_ledger_service).map_err(map_payroll_ledger_error)
        }
    }
}

fn mutate_payroll_ledger_service<T, F>(
    state: &AppState,
    mutator: F,
) -> Result<T, (StatusCode, ErrorPayload)>
where
    F: FnOnce(&PayrollLedgerService) -> Result<T, PayrollLedgerError>,
{
    match &state.runtime_state_persistence {
        RuntimeStatePersistence::Sql(repositories) => {
            let audit_trail = state.audit_trail.clone();
            let persistence_result = tokio::task::block_in_place(|| {
                Handle::current().block_on(
                    repositories
                        .payroll_ledger
                        .mutate_snapshot::<PayrollLedgerServiceSnapshot, T, PayrollLedgerError, _>(
                            move |snapshot| {
                                let snapshot = snapshot.ok_or(PayrollLedgerError::StatePoisoned)?;
                                let payroll_ledger_service =
                                    PayrollLedgerService::from_snapshot(snapshot, audit_trail);
                                let value = mutator(&payroll_ledger_service)?;
                                let snapshot = payroll_ledger_service.snapshot()?;
                                Ok((snapshot, value))
                            },
                        ),
                )
            });
            match persistence_result {
                Ok((snapshot, value)) => {
                    write_runtime_state_snapshot_to_cache(
                        state,
                        PAYROLL_LEDGER_STATE_KEY,
                        &snapshot,
                    );
                    Ok(value)
                }
                Err(JsonStatePersistenceError::Domain(error)) => {
                    Err(map_payroll_ledger_error(error))
                }
                Err(JsonStatePersistenceError::Sqlx(error)) => Err(domain_error(
                    StatusCode::INTERNAL_SERVER_ERROR,
                    "PAYROLL_LEDGER_INTERNAL_ERROR",
                    format!("failed to persist payroll ledger state: {error}"),
                )),
                Err(JsonStatePersistenceError::Serialize(error)) => Err(domain_error(
                    StatusCode::INTERNAL_SERVER_ERROR,
                    "PAYROLL_LEDGER_INTERNAL_ERROR",
                    format!("failed to serialize payroll ledger state: {error}"),
                )),
            }
        }
        #[cfg(test)]
        RuntimeStatePersistence::InMemoryOnly => {
            mutator(&state.payroll_ledger_service).map_err(map_payroll_ledger_error)
        }
    }
}

fn with_anomaly_alert_workflow<T, F>(
    state: &AppState,
    reader: F,
) -> Result<T, (StatusCode, ErrorPayload)>
where
    F: FnOnce(&AnomalyAlertWorkflow) -> Result<T, AnomalyAlertError>,
{
    match &state.runtime_state_persistence {
        RuntimeStatePersistence::Sql(repositories) => {
            let snapshot = match load_runtime_state_snapshot_from_cache::<
                AnomalyAlertWorkflowSnapshot,
            >(state, ANOMALY_ALERT_STATE_KEY)
            {
                Some(snapshot) => snapshot,
                None => {
                    let snapshot = tokio::task::block_in_place(|| {
                        Handle::current().block_on(
                            repositories
                                .anomaly_alert
                                .load_snapshot::<AnomalyAlertWorkflowSnapshot>(),
                        )
                    })
                    .map_err(|error| {
                        domain_error(
                            StatusCode::INTERNAL_SERVER_ERROR,
                            "ANOMALY_ALERT_INTERNAL_ERROR",
                            format!("failed to load anomaly alert state from SQL: {error}"),
                        )
                    })?
                    .ok_or_else(|| {
                        domain_error(
                            StatusCode::INTERNAL_SERVER_ERROR,
                            "ANOMALY_ALERT_INTERNAL_ERROR",
                            "anomaly alert state is uninitialized".to_owned(),
                        )
                    })?;
                    write_runtime_state_snapshot_to_cache(
                        state,
                        ANOMALY_ALERT_STATE_KEY,
                        &snapshot,
                    );
                    snapshot
                }
            };
            let anomaly_alert_workflow =
                AnomalyAlertWorkflow::from_snapshot(snapshot, state.audit_trail.clone());
            reader(&anomaly_alert_workflow).map_err(map_anomaly_alert_error)
        }
        #[cfg(test)]
        RuntimeStatePersistence::InMemoryOnly => {
            reader(&state.anomaly_alert_workflow).map_err(map_anomaly_alert_error)
        }
    }
}

fn mutate_anomaly_alert_workflow<T, F>(
    state: &AppState,
    mutator: F,
) -> Result<T, (StatusCode, ErrorPayload)>
where
    F: FnOnce(&AnomalyAlertWorkflow) -> Result<T, AnomalyAlertError>,
{
    match &state.runtime_state_persistence {
        RuntimeStatePersistence::Sql(repositories) => {
            let audit_trail = state.audit_trail.clone();
            let persistence_result = tokio::task::block_in_place(|| {
                Handle::current().block_on(
                    repositories
                        .anomaly_alert
                        .mutate_snapshot::<AnomalyAlertWorkflowSnapshot, T, AnomalyAlertError, _>(
                            move |snapshot| {
                                let snapshot = snapshot.ok_or(AnomalyAlertError::StatePoisoned)?;
                                let anomaly_alert_workflow =
                                    AnomalyAlertWorkflow::from_snapshot(snapshot, audit_trail);
                                let value = mutator(&anomaly_alert_workflow)?;
                                let snapshot = anomaly_alert_workflow.snapshot()?;
                                Ok((snapshot, value))
                            },
                        ),
                )
            });
            match persistence_result {
                Ok((snapshot, value)) => {
                    write_runtime_state_snapshot_to_cache(
                        state,
                        ANOMALY_ALERT_STATE_KEY,
                        &snapshot,
                    );
                    Ok(value)
                }
                Err(JsonStatePersistenceError::Domain(error)) => {
                    Err(map_anomaly_alert_error(error))
                }
                Err(JsonStatePersistenceError::Sqlx(error)) => Err(domain_error(
                    StatusCode::INTERNAL_SERVER_ERROR,
                    "ANOMALY_ALERT_INTERNAL_ERROR",
                    format!("failed to persist anomaly alert state: {error}"),
                )),
                Err(JsonStatePersistenceError::Serialize(error)) => Err(domain_error(
                    StatusCode::INTERNAL_SERVER_ERROR,
                    "ANOMALY_ALERT_INTERNAL_ERROR",
                    format!("failed to serialize anomaly alert state: {error}"),
                )),
            }
        }
        #[cfg(test)]
        RuntimeStatePersistence::InMemoryOnly => {
            mutator(&state.anomaly_alert_workflow).map_err(map_anomaly_alert_error)
        }
    }
}

fn with_operations_analytics_warehouse<T, F>(
    state: &AppState,
    reader: F,
) -> Result<T, (StatusCode, ErrorPayload)>
where
    F: FnOnce(&OperationsAnalyticsWarehouse) -> T,
{
    match &state.runtime_state_persistence {
        RuntimeStatePersistence::Sql(repositories) => {
            let snapshot = match load_runtime_state_snapshot_from_cache::<
                OperationsAnalyticsWarehouseSnapshot,
            >(state, OPERATIONS_ANALYTICS_STATE_KEY)
            {
                Some(snapshot) => snapshot,
                None => {
                    let snapshot = tokio::task::block_in_place(|| {
                        Handle::current().block_on(
                            repositories
                                .operations_analytics
                                .load_snapshot::<OperationsAnalyticsWarehouseSnapshot>(),
                        )
                    })
                    .map_err(|error| {
                        domain_error(
                            StatusCode::INTERNAL_SERVER_ERROR,
                            "ANALYTICS_WAREHOUSE_INTERNAL_ERROR",
                            format!("failed to load analytics warehouse state from SQL: {error}"),
                        )
                    })?
                    .ok_or_else(|| {
                        domain_error(
                            StatusCode::INTERNAL_SERVER_ERROR,
                            "ANALYTICS_WAREHOUSE_INTERNAL_ERROR",
                            "analytics warehouse state is uninitialized".to_owned(),
                        )
                    })?;
                    write_runtime_state_snapshot_to_cache(
                        state,
                        OPERATIONS_ANALYTICS_STATE_KEY,
                        &snapshot,
                    );
                    snapshot
                }
            };
            let warehouse =
                OperationsAnalyticsWarehouse::from_snapshot(snapshot).map_err(|error| {
                    domain_error(
                        StatusCode::INTERNAL_SERVER_ERROR,
                        "ANALYTICS_WAREHOUSE_INTERNAL_ERROR",
                        format!("failed to restore analytics warehouse state: {error}"),
                    )
                })?;
            Ok(reader(&warehouse))
        }
        #[cfg(test)]
        RuntimeStatePersistence::InMemoryOnly => {
            let warehouse = state.operations_analytics_warehouse.read().map_err(|_| {
                domain_error(
                    StatusCode::INTERNAL_SERVER_ERROR,
                    "ANALYTICS_WAREHOUSE_INTERNAL_ERROR",
                    "operations analytics warehouse lock is poisoned".to_owned(),
                )
            })?;
            Ok(reader(&warehouse))
        }
    }
}

fn mutate_operations_analytics_warehouse_best_effort<F>(
    state: &AppState,
    warning_context: &'static str,
    mutator: F,
) where
    F: FnOnce(&mut OperationsAnalyticsWarehouse),
{
    match &state.runtime_state_persistence {
        RuntimeStatePersistence::Sql(repositories) => {
            let persistence_result = tokio::task::block_in_place(|| {
                Handle::current().block_on(
                    repositories
                        .operations_analytics
                        .mutate_snapshot::<OperationsAnalyticsWarehouseSnapshot, (), String, _>(
                            move |snapshot| {
                                let mut warehouse = match snapshot {
                                    Some(snapshot) => {
                                        OperationsAnalyticsWarehouse::from_snapshot(snapshot)?
                                    }
                                    None => OperationsAnalyticsWarehouse::default(),
                                };
                                mutator(&mut warehouse);
                                Ok((warehouse.snapshot(), ()))
                            },
                        ),
                )
            });
            match persistence_result {
                Ok((snapshot, ())) => write_runtime_state_snapshot_to_cache(
                    state,
                    OPERATIONS_ANALYTICS_STATE_KEY,
                    &snapshot,
                ),
                Err(error) => {
                    tracing::warn!(
                        error = %error,
                        "{warning_context}"
                    );
                }
            }
        }
        #[cfg(test)]
        RuntimeStatePersistence::InMemoryOnly => {
            match state.operations_analytics_warehouse.write() {
                Ok(mut warehouse) => mutator(&mut warehouse),
                Err(_poisoned) => {
                    tracing::warn!(
                        "{warning_context}: operations analytics state lock is poisoned"
                    );
                }
            }
        }
    }
}

fn record_operations_analytics_anomaly_triggered_best_effort(
    state: &AppState,
    alerts: &[AnomalyAlertRecord],
) {
    if !state.advanced_analytics_dashboard_runtime_enabled || alerts.is_empty() {
        return;
    }
    mutate_operations_analytics_warehouse_best_effort(
        state,
        "advanced analytics update failed; anomaly evaluation command remains authoritative",
        |warehouse| {
            for alert in alerts {
                warehouse.record_anomaly_triggered(
                    alert.vendor_id().as_str(),
                    state.plant_id.as_str(),
                    alert.observed_at().epoch_day(),
                );
            }
        },
    );
}

fn record_operations_analytics_anomaly_closed_best_effort(
    state: &AppState,
    vendor_id: &VendorId,
    epoch_day: i32,
) {
    if !state.advanced_analytics_dashboard_runtime_enabled {
        return;
    }
    mutate_operations_analytics_warehouse_best_effort(
        state,
        "advanced analytics update failed; anomaly lifecycle command remains authoritative",
        |warehouse| {
            warehouse.record_anomaly_closed(vendor_id.as_str(), state.plant_id.as_str(), epoch_day);
        },
    );
}

fn record_operations_analytics_payroll_settlement_closed_best_effort(
    state: &AppState,
    epoch_day: i32,
    batch: &PayrollExchangeBatch,
) {
    if !state.advanced_analytics_dashboard_runtime_enabled {
        return;
    }
    mutate_operations_analytics_warehouse_best_effort(
        state,
        "advanced analytics update failed; payroll settlement command remains authoritative",
        |warehouse| {
            let reconciliation = batch.reconciliation();
            warehouse.record_payroll_settlement_closed(
                state.vendor_id.as_str(),
                state.plant_id.as_str(),
                epoch_day,
                batch.batch_id().as_str(),
                reconciliation.total_records(),
                reconciliation.disputed_records(),
                reconciliation.deduction_failed_records(),
            );
        },
    );
}

fn record_operations_analytics_payroll_hr_sync_best_effort(
    state: &AppState,
    epoch_day: i32,
    batch: &PayrollExchangeBatch,
) {
    if !state.advanced_analytics_dashboard_runtime_enabled {
        return;
    }
    let Some(sync_receipt) = batch.hr_api_sync_receipt() else {
        return;
    };
    let sync_succeeded = match sync_receipt.status() {
        HrApiSyncStatus::Succeeded => true,
        HrApiSyncStatus::Failed => false,
        HrApiSyncStatus::NotSynced => return,
    };
    mutate_operations_analytics_warehouse_best_effort(
        state,
        "advanced analytics update failed; payroll hr-sync command remains authoritative",
        |warehouse| {
            warehouse.record_payroll_hr_sync_outcome(
                state.vendor_id.as_str(),
                state.plant_id.as_str(),
                epoch_day,
                batch.batch_id().as_str(),
                sync_succeeded,
            );
        },
    );
}

fn parse_positive_u16_env(key: &str, default_value: u16) -> Result<u16, String> {
    let raw = match std::env::var(key) {
        Ok(value) => value,
        Err(_) => return Ok(default_value),
    };
    let parsed = raw
        .parse::<u16>()
        .map_err(|error| format!("{key} must be a positive integer: {error}"))?;
    if parsed == 0 {
        return Err(format!("{key} must be greater than zero"));
    }
    Ok(parsed)
}

fn parse_positive_u64_env(key: &str, default_value: u64) -> Result<u64, String> {
    let raw = match std::env::var(key) {
        Ok(value) => value,
        Err(_) => return Ok(default_value),
    };
    let parsed = raw
        .parse::<u64>()
        .map_err(|error| format!("{key} must be a positive integer: {error}"))?;
    if parsed == 0 {
        return Err(format!("{key} must be greater than zero"));
    }
    Ok(parsed)
}

fn parse_bool_env_default_false(key: &str) -> Result<bool, String> {
    let raw = match std::env::var(key) {
        Ok(value) => value,
        Err(_) => return Ok(false),
    };
    match raw.trim().to_ascii_lowercase().as_str() {
        "true" => Ok(true),
        "false" => Ok(false),
        _ => Err(format!("{key} must be either `true` or `false`")),
    }
}

fn load_required_non_empty_env(key: &str) -> Result<String, String> {
    let raw = std::env::var(key).map_err(|_| format!("{key} environment variable is required"))?;
    let trimmed = raw.trim();
    if trimmed.is_empty() {
        return Err(format!("{key} environment variable must be non-empty"));
    }
    Ok(trimmed.to_owned())
}

async fn parse_valkey_runtime_state_cache_from_env() -> Result<ValkeyRuntimeStateCache, String> {
    let valkey_url = load_required_non_empty_env(VALKEY_URL_ENV)?;
    let key_prefix = std::env::var(VALKEY_CACHE_KEY_PREFIX_ENV)
        .map(|value| value.trim().to_owned())
        .ok()
        .filter(|value| !value.is_empty())
        .unwrap_or_else(|| DEFAULT_VALKEY_CACHE_KEY_PREFIX.to_owned());
    let ttls = RuntimeStateCacheTtls::from_env()?;
    ValkeyRuntimeStateCache::connect(valkey_url, key_prefix, ttls).await
}

fn parse_object_storage_upload_pipeline_from_env() -> Result<ObjectStorageUploadPipeline, String> {
    let endpoint = load_required_non_empty_env(MINIO_ENDPOINT_ENV)?;
    let access_key_id = load_required_non_empty_env(MINIO_ROOT_USER_ENV)?;
    let secret_access_key = load_required_non_empty_env(MINIO_ROOT_PASSWORD_ENV)?;
    let menu_bucket = load_required_non_empty_env(MINIO_BUCKET_MENU_IMAGES_ENV)?;
    let compliance_bucket = load_required_non_empty_env(MINIO_BUCKET_COMPLIANCE_EVIDENCE_ENV)?;
    let fulfillment_bucket = load_required_non_empty_env(MINIO_BUCKET_FULFILLMENT_EXPORTS_ENV)?;
    let region = load_required_non_empty_env(OBJECT_STORAGE_REGION_ENV)?;
    let key_namespace = load_required_non_empty_env(OBJECT_STORAGE_KEY_NAMESPACE_ENV)?;
    let upload_ttl_seconds = parse_positive_u16_env(
        OBJECT_STORAGE_UPLOAD_TTL_SECONDS_ENV,
        DEFAULT_OBJECT_STORAGE_UPLOAD_TTL_SECONDS,
    )?;
    let download_ttl_seconds = parse_positive_u16_env(
        OBJECT_STORAGE_DOWNLOAD_TTL_SECONDS_ENV,
        DEFAULT_OBJECT_STORAGE_DOWNLOAD_TTL_SECONDS,
    )?;

    let config = S3ObjectStorageConfig::new(
        endpoint,
        region,
        access_key_id,
        secret_access_key,
        menu_bucket,
        compliance_bucket,
        fulfillment_bucket,
    )
    .map_err(|error| error.to_string())?
    .with_ttls(
        u32::from(upload_ttl_seconds),
        u32::from(download_ttl_seconds),
    )
    .map_err(|error| error.to_string())?
    .with_key_namespace(key_namespace);
    ObjectStorageUploadPipeline::new(config).map_err(|error| error.to_string())
}

fn resolve_rush_reminder_policy(runtime_enabled: bool) -> Result<RushReminderPolicy, String> {
    if runtime_enabled {
        parse_rush_reminder_policy_from_env()
    } else {
        Ok(RushReminderPolicy::default())
    }
}

fn parse_rush_reminder_policy_from_env() -> Result<RushReminderPolicy, String> {
    let preorder_open_min_lead_days = parse_positive_u16_env(
        PRELAUNCH_RUSH_PREORDER_MIN_LEAD_DAYS_ENV,
        DEFAULT_RUSH_PREORDER_OPEN_MIN_LEAD_DAYS,
    )?;
    let preorder_open_max_lead_days = parse_positive_u16_env(
        PRELAUNCH_RUSH_PREORDER_MAX_LEAD_DAYS_ENV,
        DEFAULT_RUSH_PREORDER_OPEN_MAX_LEAD_DAYS,
    )?;
    let preorder_open_throttle_minutes = parse_positive_u16_env(
        PRELAUNCH_RUSH_PREORDER_THROTTLE_MINUTES_ENV,
        DEFAULT_RUSH_PREORDER_OPEN_THROTTLE_MINUTES,
    )?;
    let demand_spike_remaining_quantity_threshold = parse_positive_u16_env(
        PRELAUNCH_RUSH_DEMAND_SPIKE_REMAINING_THRESHOLD_ENV,
        DEFAULT_RUSH_DEMAND_SPIKE_REMAINING_THRESHOLD,
    )?;
    let demand_spike_throttle_minutes = parse_positive_u16_env(
        PRELAUNCH_RUSH_DEMAND_SPIKE_THROTTLE_MINUTES_ENV,
        DEFAULT_RUSH_DEMAND_SPIKE_THROTTLE_MINUTES,
    )?;

    RushReminderPolicy::new(
        preorder_open_min_lead_days,
        preorder_open_max_lead_days,
        preorder_open_throttle_minutes,
        demand_spike_remaining_quantity_threshold,
        demand_spike_throttle_minutes,
    )
    .map_err(|error| format!("rush reminder policy is invalid: {error}"))
}

fn parse_audit_trail_encryption_key_from_env() -> Result<AuditSnapshotEncryptionKey, String> {
    let raw = std::env::var(PRELAUNCH_AUDIT_TRAIL_ENCRYPTION_KEY_ENV).map_err(|_| {
        format!("{PRELAUNCH_AUDIT_TRAIL_ENCRYPTION_KEY_ENV} must be set to a 64-char hex key")
    })?;
    AuditSnapshotEncryptionKey::parse_hex(raw)
        .map_err(|error| format!("{PRELAUNCH_AUDIT_TRAIL_ENCRYPTION_KEY_ENV} is invalid: {error}"))
}

fn parse_payroll_export_encryption_key_from_env() -> Result<PayrollExportFieldEncryptor, String> {
    let raw = std::env::var(PRELAUNCH_PAYROLL_EXPORT_ENCRYPTION_KEY_ENV).map_err(|_| {
        format!("{PRELAUNCH_PAYROLL_EXPORT_ENCRYPTION_KEY_ENV} must be set to a 64-char hex key")
    })?;
    PayrollExportFieldEncryptor::parse_hex(raw).map_err(|error| {
        format!("{PRELAUNCH_PAYROLL_EXPORT_ENCRYPTION_KEY_ENV} is invalid: {error}")
    })
}

fn parse_terminated_employee_actor_ids_from_env() -> Result<HashSet<ActorId>, String> {
    let raw = match std::env::var(PRELAUNCH_TERMINATED_EMPLOYEE_ACTOR_IDS_ENV) {
        Ok(value) => value,
        Err(_) => return Ok(HashSet::new()),
    };
    let mut actor_ids = HashSet::new();
    for candidate in raw
        .split(',')
        .map(str::trim)
        .filter(|value| !value.is_empty())
    {
        let actor_id = ActorId::parse(candidate.to_owned()).map_err(|error| error.to_string())?;
        actor_ids.insert(actor_id);
    }
    Ok(actor_ids)
}

fn resolve_delivery_epoch_day() -> Result<i32, String> {
    if let Ok(raw) = std::env::var("PRELAUNCH_DELIVERY_EPOCH_DAY") {
        let parsed = raw
            .parse::<i32>()
            .map_err(|error| format!("PRELAUNCH_DELIVERY_EPOCH_DAY must be integer: {error}"))?;
        if parsed <= 0 {
            return Err("PRELAUNCH_DELIVERY_EPOCH_DAY must be greater than zero".to_owned());
        }
        return Ok(parsed);
    }

    let now = current_taipei_business_moment()?;
    Ok(now.epoch_day().saturating_add(DEFAULT_DELIVERY_DAY_OFFSET))
}

fn current_taipei_business_moment() -> Result<TaipeiBusinessMoment, String> {
    let unix_seconds = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map_err(|error| format!("failed to read system clock: {error}"))?
        .as_secs();
    let unix_seconds_i64 = i64::try_from(unix_seconds)
        .map_err(|_| "system clock overflowed i64 seconds".to_owned())?;
    TaipeiBusinessMoment::from_utc_unix_seconds(unix_seconds_i64).map_err(|error| {
        format!("failed to convert system time to Taipei business moment: {error}")
    })
}

fn seeded_menu_type(index: u16) -> &'static str {
    const MENU_TYPES: [&str; 7] = [
        "BENTO", "NOODLE", "SALAD", "BOWL", "SOUP", "SNACK", "BEVERAGE",
    ];
    MENU_TYPES[usize::from((index - 1) % (MENU_TYPES.len() as u16))]
}

#[cfg(test)]
fn seeded_menu_image_ref(vendor_id: &VendorId, file_name: &str) -> String {
    let menu_bucket = configured_menu_object_bucket();
    let namespace = configured_object_storage_key_namespace();
    let owner_scope = normalize_owner_scope_segment(vendor_id.as_str());
    let object_file_name = seeded_object_file_name(262_144, file_name);
    let key_prefix = if namespace.is_empty() {
        format!("menu-images/{owner_scope}/seed")
    } else {
        format!("{namespace}/menu-images/{owner_scope}/seed")
    };
    format!("s3://{menu_bucket}/{key_prefix}/{object_file_name}")
}

#[cfg(test)]
fn seeded_compliance_document_ref(vendor_id: &VendorId, file_name: &str) -> String {
    let compliance_bucket = configured_compliance_object_bucket();
    let namespace = configured_object_storage_key_namespace();
    let owner_scope = normalize_owner_scope_segment(vendor_id.as_str());
    let object_file_name = seeded_object_file_name(524_288, file_name);
    let key_prefix = if namespace.is_empty() {
        format!("compliance-documents/{owner_scope}/seed")
    } else {
        format!("{namespace}/compliance-documents/{owner_scope}/seed")
    };
    format!("s3://{compliance_bucket}/{key_prefix}/{object_file_name}")
}

#[cfg(test)]
fn seeded_object_file_name(size_bytes: u64, file_name: &str) -> String {
    format!("{size_bytes}-deadbeef-{file_name}")
}

#[cfg(not(test))]
const SEEDED_MENU_IMAGE_PAYLOAD: &[u8] = &[
    0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52,
    0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, 0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53,
    0xDE, 0x00, 0x00, 0x00, 0x0C, 0x49, 0x44, 0x41, 0x54, 0x08, 0xD7, 0x63, 0xF8, 0xCF, 0xC0, 0x00,
    0x00, 0x03, 0x01, 0x01, 0x00, 0x18, 0xDD, 0x8D, 0xB1, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4E,
    0x44, 0xAE, 0x42, 0x60, 0x82,
];
#[cfg(not(test))]
const SEEDED_MENU_IMAGE_THUMBNAIL_PAYLOAD: &[u8] = b"RIFF\x1a\x00\x00\x00WEBPVP8 \x0e\x00\x00\x00\x30\x01\x00\x9d\x01*\x01\x00\x01\x00\x00\x02\x00\x34\x25\xa4";
#[cfg(not(test))]
const SEEDED_COMPLIANCE_DOCUMENT_PAYLOAD: &[u8] =
    b"%PDF-1.4\n1 0 obj << /Type /Catalog >> endobj\ntrailer << /Root 1 0 R >>\n%%EOF\n";

fn provision_seeded_menu_image_ref(
    object_storage_upload_pipeline: &ObjectStorageUploadPipeline,
    vendor_id: &VendorId,
    file_name: &str,
) -> Result<String, String> {
    #[cfg(test)]
    {
        let _ = object_storage_upload_pipeline;
        Ok(seeded_menu_image_ref(vendor_id, file_name))
    }
    #[cfg(not(test))]
    {
        upload_seeded_object_reference(
            object_storage_upload_pipeline,
            StorageArtifactClass::MenuImage,
            vendor_id,
            file_name,
            "image/png",
            SEEDED_MENU_IMAGE_PAYLOAD,
            Some(SEEDED_MENU_IMAGE_THUMBNAIL_PAYLOAD),
        )
    }
}

fn provision_seeded_compliance_document_ref(
    object_storage_upload_pipeline: &ObjectStorageUploadPipeline,
    vendor_id: &VendorId,
    file_name: &str,
) -> Result<String, String> {
    #[cfg(test)]
    {
        let _ = object_storage_upload_pipeline;
        Ok(seeded_compliance_document_ref(vendor_id, file_name))
    }
    #[cfg(not(test))]
    {
        upload_seeded_object_reference(
            object_storage_upload_pipeline,
            StorageArtifactClass::ComplianceDocument,
            vendor_id,
            file_name,
            "application/pdf",
            SEEDED_COMPLIANCE_DOCUMENT_PAYLOAD,
            None,
        )
    }
}

#[cfg(not(test))]
fn upload_seeded_object_reference(
    object_storage_upload_pipeline: &ObjectStorageUploadPipeline,
    artifact_class: StorageArtifactClass,
    vendor_id: &VendorId,
    file_name: &str,
    mime_type: &str,
    payload: &[u8],
    thumbnail_payload: Option<&[u8]>,
) -> Result<String, String> {
    let size_bytes = u64::try_from(payload.len())
        .map_err(|_| "seeded payload length overflowed u64".to_owned())?;
    let thumbnail_size_bytes = thumbnail_payload
        .map(|bytes| {
            u64::try_from(bytes.len())
                .map_err(|_| "seeded thumbnail payload length overflowed u64".to_owned())
        })
        .transpose()?;
    let upload_plan = object_storage_upload_pipeline
        .create_upload_plan(
            ObjectUploadIntent {
                artifact_class,
                owner_scope: Some(vendor_id.as_str().to_owned()),
                file_name: file_name.to_owned(),
                mime_type: mime_type.to_owned(),
                size_bytes,
                thumbnail_size_bytes,
            },
            SystemTime::now(),
        )
        .map_err(|error| format!("failed to create seeded upload plan: {error}"))?;
    upload_seeded_payload(&upload_plan.primary, payload)
        .map_err(|error| format!("failed to upload seeded primary object: {error}"))?;
    match (&upload_plan.thumbnail, thumbnail_payload) {
        (Some(target), Some(payload)) => upload_seeded_payload(target, payload)
            .map_err(|error| format!("failed to upload seeded thumbnail object: {error}"))?,
        (Some(_), None) => {
            return Err(
                "seeded upload plan required thumbnail payload but none provided".to_owned(),
            )
        }
        (None, Some(_)) => {
            return Err(
                "seeded upload plan omitted thumbnail target but payload was provided".to_owned(),
            )
        }
        (None, None) => {}
    }
    Ok(upload_plan.primary.object_ref.as_str().to_owned())
}

#[cfg(not(test))]
fn upload_seeded_payload(target: &PresignedUploadTarget, payload: &[u8]) -> Result<(), String> {
    let client = reqwest::blocking::Client::builder()
        .build()
        .map_err(|error| error.to_string())?;
    let mut request = client.put(target.upload_url.as_str());
    for (name, value) in &target.required_headers {
        request = request.header(name.as_str(), value.as_str());
    }
    let response = request
        .body(payload.to_vec())
        .send()
        .map_err(|error| error.to_string())?;
    if !response.status().is_success() {
        return Err(format!(
            "object storage upload failed with status {}",
            response.status()
        ));
    }
    Ok(())
}

fn seeded_menu_health_tags(index: u16) -> Vec<MenuHealthTag> {
    match index % 5 {
        0 => vec![MenuHealthTag::LowCalorie, MenuHealthTag::HighProtein],
        1 => vec![MenuHealthTag::HighProtein],
        2 => vec![MenuHealthTag::Vegetarian],
        3 => vec![MenuHealthTag::Vegan],
        _ => vec![MenuHealthTag::GlutenFree],
    }
}

async fn load_or_seed_compliance_lifecycle(
    repository: &VendorComplianceSqlRepository,
    audit_trail: ImmutableAuditTrail,
    object_storage_upload_pipeline: &ObjectStorageUploadPipeline,
    vendor_id: VendorId,
    plant_id: PlantId,
    delivery_epoch_day: i32,
) -> Result<(VendorComplianceLifecycle, bool), String> {
    let retention_policy = HistoryRetentionPolicy::default();
    if let Some(existing) = repository
        .load_lifecycle(retention_policy.clone(), audit_trail.clone())
        .await
        .map_err(|error| error.to_string())?
    {
        return Ok((existing, false));
    }

    let seeded = build_seeded_load_gate_compliance_lifecycle(
        audit_trail,
        retention_policy,
        object_storage_upload_pipeline,
        vendor_id,
        plant_id,
        delivery_epoch_day,
    )?;
    repository
        .save_lifecycle(&seeded)
        .await
        .map_err(|error| error.to_string())?;
    Ok((seeded, true))
}

fn build_seeded_load_gate_compliance_lifecycle(
    audit_trail: ImmutableAuditTrail,
    retention_policy: HistoryRetentionPolicy,
    object_storage_upload_pipeline: &ObjectStorageUploadPipeline,
    vendor_id: VendorId,
    plant_id: PlantId,
    delivery_epoch_day: i32,
) -> Result<VendorComplianceLifecycle, String> {
    let committee_actor = load_gate_committee_admin_actor().map_err(|(_, error)| error.message)?;
    let vendor_actor = AuthenticatedActorContext::new(
        ActorId::parse("vendor-load-gate").map_err(|error| error.to_string())?,
        Role::VendorOperator,
        PlantScope::restricted(vec![plant_id]).map_err(|error| error.to_string())?,
        AuthenticationSource::VendorAccountMfa,
    )
    .map_err(|error| error.to_string())?;
    let mut lifecycle = VendorComplianceLifecycle::with_audit_trail(retention_policy, audit_trail);
    let vendor_category = VendorCategory::parse("RESTAURANT").map_err(|error| error.to_string())?;
    let template_id =
        DocumentTemplateId::parse(DEFAULT_SEED_TEMPLATE_ID).map_err(|error| error.to_string())?;

    lifecycle
        .upsert_document_template(
            &committee_actor,
            ComplianceDocumentTemplate::new(
                template_id.clone(),
                vendor_category.clone(),
                "Business License",
                true,
                365,
                vec![30, 7],
                0,
            )
            .map_err(|error| error.to_string())?,
        )
        .map_err(|error| error.to_string())?;

    let submitted_on = ComplianceDate::from_epoch_day(delivery_epoch_day.saturating_sub(30));
    let approved_on = ComplianceDate::from_epoch_day(delivery_epoch_day.saturating_sub(29));

    lifecycle
        .register_vendor_application(
            &vendor_actor,
            vendor_id.clone(),
            "Load Gate Vendor",
            vendor_category,
            submitted_on,
        )
        .map_err(|error| error.to_string())?;

    lifecycle
        .submit_document(
            &vendor_actor,
            &vendor_id,
            &template_id,
            VendorDocumentSubmission::new(
                provision_seeded_compliance_document_ref(
                    object_storage_upload_pipeline,
                    &vendor_id,
                    "load-gate-license.pdf",
                )?,
                submitted_on,
                ComplianceDate::from_epoch_day(delivery_epoch_day.saturating_add(300)),
            )
            .map_err(|error| error.to_string())?,
        )
        .map_err(|error| error.to_string())?;

    lifecycle
        .review_application(
            &committee_actor,
            &vendor_id,
            VendorReviewDecision::Approved,
            "Prelaunch load-gate vendor is approved.",
            approved_on,
        )
        .map_err(|error| error.to_string())?;

    Ok(lifecycle)
}

fn bootstrap_runtime_state(
    audit_trail: ImmutableAuditTrail,
    vendor_id: VendorId,
    plant_id: PlantId,
    delivery_epoch_day: i32,
    menu_variant_count: u16,
    recommendation_engine_runtime_enabled: bool,
    advanced_analytics_dashboard_runtime_enabled: bool,
    rush_reminder_runtime_enabled: bool,
    rush_reminder_policy: RushReminderPolicy,
    object_storage_upload_pipeline: Arc<ObjectStorageUploadPipeline>,
    payroll_retention_policy: PayrollRetentionPolicy,
    order_retention_policy: OrderRetentionPolicy,
    payroll_export_field_encryptor: PayrollExportFieldEncryptor,
    pickup_totp_verifier: Arc<PickupTotpVerifier>,
    compliance_lifecycle: VendorComplianceLifecycle,
    compliance_persistence: CompliancePersistence,
    runtime_state_persistence: RuntimeStatePersistence,
    runtime_state_cache: Option<Arc<ValkeyRuntimeStateCache>>,
    order_event_backbone: Option<Arc<OrderEventBackbone>>,
    include_lifecycle_seed_baseline: bool,
) -> Result<AppState, String> {
    let committee_actor = load_gate_committee_admin_actor().map_err(|(_, error)| error.message)?;

    let vendor_actor = AuthenticatedActorContext::new(
        ActorId::parse("vendor-load-gate").map_err(|error| error.to_string())?,
        Role::VendorOperator,
        PlantScope::restricted(vec![plant_id.clone()]).map_err(|error| error.to_string())?,
        AuthenticationSource::VendorAccountMfa,
    )
    .map_err(|error| error.to_string())?;

    let mut compliance_lifecycle = compliance_lifecycle;
    let terminated_employee_actor_ids =
        parse_terminated_employee_actor_ids_from_env().map_err(|error| {
            format!("{PRELAUNCH_TERMINATED_EMPLOYEE_ACTOR_IDS_ENV} is invalid: {error}")
        })?;
    let rush_reminder_workflow = RushReminderWorkflow::new(rush_reminder_policy);
    let rush_reminder_delivery_gateway: ReminderDeliveryGateway =
        Arc::new(NoopRushReminderDeliveryGateway);

    let mut should_seed_runtime_baseline = true;
    let mut delivery_policy;
    let menu_supply_policy;
    let payroll_ledger_service;
    let anomaly_alert_workflow;
    let operations_analytics_warehouse;

    match &runtime_state_persistence {
        RuntimeStatePersistence::Sql(repositories) => {
            let repositories = repositories.clone();
            let (
                delivery_snapshot,
                menu_snapshot,
                payroll_snapshot,
                anomaly_snapshot,
                analytics_snapshot,
            ) = tokio::task::block_in_place(|| {
                Handle::current().block_on(async move {
                    let delivery_snapshot = repositories
                        .delivery_policy
                        .load_snapshot::<PersistedPolicySnapshot>()
                        .await?;
                    let menu_snapshot = repositories
                        .menu_supply
                        .load_snapshot::<MenuSupplyPolicySnapshot>()
                        .await?;
                    let payroll_snapshot = repositories
                        .payroll_ledger
                        .load_snapshot::<PayrollLedgerServiceSnapshot>()
                        .await?;
                    let anomaly_snapshot = repositories
                        .anomaly_alert
                        .load_snapshot::<AnomalyAlertWorkflowSnapshot>()
                        .await?;
                    let analytics_snapshot = repositories
                        .operations_analytics
                        .load_snapshot::<OperationsAnalyticsWarehouseSnapshot>()
                        .await?;
                    Ok::<_, JsonStatePersistenceError>((
                        delivery_snapshot,
                        menu_snapshot,
                        payroll_snapshot,
                        anomaly_snapshot,
                        analytics_snapshot,
                    ))
                })
            })
            .map_err(|error| format!("failed to load runtime SQL state snapshots: {error}"))?;

            match (
                delivery_snapshot,
                menu_snapshot,
                payroll_snapshot,
                anomaly_snapshot,
                analytics_snapshot,
            ) {
                (
                    Some(delivery_snapshot),
                    Some(menu_snapshot),
                    Some(payroll_snapshot),
                    Some(anomaly_snapshot),
                    Some(analytics_snapshot),
                ) => {
                    delivery_policy = VendorPlantDeliveryPolicy::from_snapshot(
                        delivery_snapshot,
                        audit_trail.clone(),
                    )
                    .map_err(|error| {
                        format!("failed to restore delivery policy from SQL snapshot: {error}")
                    })?;
                    menu_supply_policy = MenuSupplyPolicy::from_snapshot(
                        menu_snapshot,
                        audit_trail.clone(),
                    )
                    .map_err(|error| {
                        format!("failed to restore menu supply policy from SQL snapshot: {error}")
                    })?;
                    payroll_ledger_service =
                        PayrollLedgerService::from_snapshot(payroll_snapshot, audit_trail.clone());
                    anomaly_alert_workflow =
                        AnomalyAlertWorkflow::from_snapshot(anomaly_snapshot, audit_trail.clone());
                    operations_analytics_warehouse =
                        OperationsAnalyticsWarehouse::from_snapshot(analytics_snapshot)
                            .map_err(|error| {
                                format!(
                                    "failed to restore operations analytics state from SQL snapshot: {error}"
                                )
                            })?;
                    should_seed_runtime_baseline = false;
                }
                (None, None, None, None, None) => {
                    delivery_policy =
                        VendorPlantDeliveryPolicy::with_audit_trail(audit_trail.clone());
                    menu_supply_policy = MenuSupplyPolicy::with_audit_trail_and_retention(
                        Default::default(),
                        audit_trail.clone(),
                        order_retention_policy,
                    );
                    payroll_ledger_service =
                        PayrollLedgerService::new(payroll_retention_policy, audit_trail.clone());
                    anomaly_alert_workflow =
                        AnomalyAlertWorkflow::with_default_rules(audit_trail.clone());
                    operations_analytics_warehouse = OperationsAnalyticsWarehouse::default();
                }
                _ => {
                    return Err(
                        "runtime SQL state snapshots are partially initialized; expected all-or-none persisted snapshots"
                            .to_owned(),
                    );
                }
            }
        }
        #[cfg(test)]
        RuntimeStatePersistence::InMemoryOnly => {
            delivery_policy = VendorPlantDeliveryPolicy::with_audit_trail(audit_trail.clone());
            menu_supply_policy = MenuSupplyPolicy::with_audit_trail_and_retention(
                Default::default(),
                audit_trail.clone(),
                order_retention_policy,
            );
            payroll_ledger_service =
                PayrollLedgerService::new(payroll_retention_policy, audit_trail.clone());
            anomaly_alert_workflow = AnomalyAlertWorkflow::with_default_rules(audit_trail.clone());
            operations_analytics_warehouse = OperationsAnalyticsWarehouse::default();
        }
    }

    if should_seed_runtime_baseline {
        let mapping_window_start =
            TaipeiBusinessMoment::new(delivery_epoch_day.saturating_sub(30), 0)
                .map_err(|error| format!("failed to create delivery window start: {error}"))?;
        let mapping_window_end =
            TaipeiBusinessMoment::new(delivery_epoch_day.saturating_add(30), 23 * 60 + 59)
                .map_err(|error| format!("failed to create delivery window end: {error}"))?;

        delivery_policy
            .upsert_mapping(
                &committee_actor,
                TaipeiBusinessMoment::new(delivery_epoch_day.saturating_sub(30), 1)
                    .map_err(|error| error.to_string())?,
                VendorPlantDeliveryMapping::new(
                    DeliveryMappingId::parse("map-load-gate-allow")
                        .map_err(|error| error.to_string())?,
                    vendor_id.clone(),
                    plant_id.clone(),
                    ServiceWindow::new(mapping_window_start, mapping_window_end)
                        .map_err(|error| error.to_string())?,
                    DeliveryRuleEffect::Allow,
                    100,
                ),
            )
            .map_err(|error| error.to_string())?;

        let vendor_menu_gateway = HttpVendorMenuExecutionGateway::new(&menu_supply_policy);
        for index in 1..=menu_variant_count {
            let menu_item_id =
                MenuItemId::parse(format!("menu-{index}")).map_err(|error| error.to_string())?;
            let delivery_epoch_day = delivery_epoch_day.saturating_add(i32::from((index - 1) % 7));
            let image_ref = provision_seeded_menu_image_ref(
                object_storage_upload_pipeline.as_ref(),
                &vendor_id,
                format!("load-gate-{index}.png").as_str(),
            )?;
            let image_url = MenuImageUrl::parse(image_ref).map_err(|error| error.to_string())?;
            let menu_item = VendorMenuItem::new(
                menu_item_id.clone(),
                vendor_id.clone(),
                VendorMenuItemDraft::new(
                    format!("Load Gate Meal {index}"),
                    "Seeded menu item for hard-SLO prelaunch verification",
                    seeded_menu_type(index).to_owned(),
                    seeded_menu_health_tags(index),
                    Some(image_url),
                    Money::new("TWD", 12000).map_err(|error| error.to_string())?,
                    2000,
                    delivery_epoch_day,
                )
                .map_err(|error| error.to_string())?,
            );

            vendor_menu_gateway
                .execute_upsert_vendor_menu_item(&vendor_actor, menu_item)
                .map_err(|error| error.to_string())?;
        }

        seed_runtime_baseline_scenarios(
            &committee_actor,
            &vendor_actor,
            object_storage_upload_pipeline.as_ref(),
            &vendor_id,
            &plant_id,
            delivery_epoch_day,
            include_lifecycle_seed_baseline,
            &mut compliance_lifecycle,
            &mut delivery_policy,
            &menu_supply_policy,
            &payroll_ledger_service,
            &anomaly_alert_workflow,
        )?;

        #[cfg(not(test))]
        {
            let RuntimeStatePersistence::Sql(repositories) = &runtime_state_persistence;
            let repositories = repositories.clone();
            let delivery_snapshot = delivery_policy.snapshot();
            let menu_snapshot = menu_supply_policy
                .snapshot()
                .map_err(|error| format!("failed to snapshot menu supply state: {error}"))?;
            let payroll_snapshot = payroll_ledger_service
                .snapshot()
                .map_err(|error| format!("failed to snapshot payroll ledger state: {error}"))?;
            let anomaly_snapshot = anomaly_alert_workflow
                .snapshot()
                .map_err(|error| format!("failed to snapshot anomaly alert state: {error}"))?;
            let operations_analytics_snapshot = operations_analytics_warehouse.snapshot();
            tokio::task::block_in_place(|| {
                Handle::current().block_on(async move {
                    repositories
                        .delivery_policy
                        .save_snapshot(&delivery_snapshot)
                        .await?;
                    repositories
                        .menu_supply
                        .save_snapshot(&menu_snapshot)
                        .await?;
                    repositories
                        .payroll_ledger
                        .save_snapshot(&payroll_snapshot)
                        .await?;
                    repositories
                        .anomaly_alert
                        .save_snapshot(&anomaly_snapshot)
                        .await?;
                    repositories
                        .operations_analytics
                        .save_snapshot(&operations_analytics_snapshot)
                        .await?;
                    Ok::<(), JsonStatePersistenceError>(())
                })
            })
            .map_err(|error| format!("failed to persist seeded runtime SQL snapshots: {error}"))?;
        }
        #[cfg(test)]
        if let RuntimeStatePersistence::Sql(repositories) = &runtime_state_persistence {
            let repositories = repositories.clone();
            let delivery_snapshot = delivery_policy.snapshot();
            let menu_snapshot = menu_supply_policy
                .snapshot()
                .map_err(|error| format!("failed to snapshot menu supply state: {error}"))?;
            let payroll_snapshot = payroll_ledger_service
                .snapshot()
                .map_err(|error| format!("failed to snapshot payroll ledger state: {error}"))?;
            let anomaly_snapshot = anomaly_alert_workflow
                .snapshot()
                .map_err(|error| format!("failed to snapshot anomaly alert state: {error}"))?;
            let operations_analytics_snapshot = operations_analytics_warehouse.snapshot();
            tokio::task::block_in_place(|| {
                Handle::current().block_on(async move {
                    repositories
                        .delivery_policy
                        .save_snapshot(&delivery_snapshot)
                        .await?;
                    repositories
                        .menu_supply
                        .save_snapshot(&menu_snapshot)
                        .await?;
                    repositories
                        .payroll_ledger
                        .save_snapshot(&payroll_snapshot)
                        .await?;
                    repositories
                        .anomaly_alert
                        .save_snapshot(&anomaly_snapshot)
                        .await?;
                    repositories
                        .operations_analytics
                        .save_snapshot(&operations_analytics_snapshot)
                        .await?;
                    Ok::<(), JsonStatePersistenceError>(())
                })
            })
            .map_err(|error| format!("failed to persist seeded runtime SQL snapshots: {error}"))?;
        }

        if include_lifecycle_seed_baseline {
            match &compliance_persistence {
                CompliancePersistence::Sql(repository) => {
                    tokio::task::block_in_place(|| {
                        Handle::current().block_on(repository.save_lifecycle(&compliance_lifecycle))
                    })
                    .map_err(|error| {
                        format!("failed to persist seeded compliance baseline scenarios: {error}")
                    })?;
                }
                #[cfg(test)]
                CompliancePersistence::InMemoryOnly => {}
            }
        }
    }

    if let Some(cache) = runtime_state_cache.as_ref() {
        let delivery_snapshot = delivery_policy.snapshot();
        let menu_snapshot = menu_supply_policy.snapshot().map_err(|error| {
            format!("failed to snapshot menu supply state for cache warmup: {error}")
        })?;
        let payroll_snapshot = payroll_ledger_service.snapshot().map_err(|error| {
            format!("failed to snapshot payroll ledger state for cache warmup: {error}")
        })?;
        let anomaly_snapshot = anomaly_alert_workflow.snapshot().map_err(|error| {
            format!("failed to snapshot anomaly alert state for cache warmup: {error}")
        })?;
        let analytics_snapshot = operations_analytics_warehouse.snapshot();
        tokio::task::block_in_place(|| {
            Handle::current().block_on(async {
                cache
                    .write_through_snapshot(DELIVERY_POLICY_STATE_KEY, &delivery_snapshot)
                    .await?;
                cache
                    .write_through_snapshot(MENU_SUPPLY_STATE_KEY, &menu_snapshot)
                    .await?;
                cache
                    .write_through_snapshot(PAYROLL_LEDGER_STATE_KEY, &payroll_snapshot)
                    .await?;
                cache
                    .write_through_snapshot(ANOMALY_ALERT_STATE_KEY, &anomaly_snapshot)
                    .await?;
                cache
                    .write_through_snapshot(OPERATIONS_ANALYTICS_STATE_KEY, &analytics_snapshot)
                    .await?;
                Ok::<(), String>(())
            })
        })
        .map_err(|error| format!("failed to warm Valkey runtime state cache: {error}"))?;
    }

    Ok(AppState {
        #[cfg(test)]
        next_order_sequence: Arc::new(AtomicU64::new(1)),
        vendor_id,
        plant_id,
        recommendation_engine_runtime_enabled,
        advanced_analytics_dashboard_runtime_enabled,
        rush_reminder_runtime_enabled,
        menu_recommendation_ranker: heuristic_menu_recommendation_ranker,
        rush_reminder_workflow,
        rush_reminder_delivery_gateway,
        object_storage_upload_pipeline,
        terminated_employee_actor_ids: Arc::new(terminated_employee_actor_ids),
        audit_trail,
        payroll_export_field_encryptor,
        #[cfg(test)]
        compliance_lifecycle: Arc::new(RwLock::new(compliance_lifecycle)),
        compliance_persistence,
        runtime_state_persistence,
        runtime_state_cache,
        runtime_state_cache_bypass_keys: Arc::new(Mutex::new(HashSet::new())),
        order_event_backbone,
        pickup_totp_verifier,
        #[cfg(test)]
        operations_analytics_warehouse: Arc::new(RwLock::new(operations_analytics_warehouse)),
        #[cfg(test)]
        payroll_ledger_service,
        #[cfg(test)]
        anomaly_alert_workflow,
        #[cfg(test)]
        delivery_policy: Arc::new(delivery_policy),
        #[cfg(test)]
        menu_supply_policy,
    })
}

#[allow(clippy::too_many_arguments)]
fn seed_runtime_baseline_scenarios(
    committee_actor: &AuthenticatedActorContext,
    vendor_actor: &AuthenticatedActorContext,
    object_storage_upload_pipeline: &ObjectStorageUploadPipeline,
    vendor_id: &VendorId,
    plant_id: &PlantId,
    delivery_epoch_day: i32,
    include_lifecycle_seed_baseline: bool,
    compliance_lifecycle: &mut VendorComplianceLifecycle,
    delivery_policy: &mut VendorPlantDeliveryPolicy,
    menu_supply_policy: &MenuSupplyPolicy,
    payroll_ledger_service: &PayrollLedgerService,
    anomaly_alert_workflow: &AnomalyAlertWorkflow,
) -> Result<(), String> {
    if include_lifecycle_seed_baseline {
        seed_lifecycle_and_mapping_scenarios(
            committee_actor,
            vendor_actor,
            object_storage_upload_pipeline,
            vendor_id,
            plant_id,
            delivery_epoch_day,
            compliance_lifecycle,
            delivery_policy,
        )?;
    } else {
        seed_delivery_mapping_scenarios(vendor_id, plant_id, delivery_epoch_day, delivery_policy)?;
    }
    seed_payroll_dispute_scenario(
        vendor_id,
        plant_id,
        delivery_epoch_day,
        compliance_lifecycle,
        delivery_policy,
        menu_supply_policy,
        payroll_ledger_service,
    )?;
    seed_anomaly_alert_scenario(
        committee_actor,
        vendor_id,
        delivery_epoch_day,
        anomaly_alert_workflow,
    )?;
    Ok(())
}

#[allow(clippy::too_many_arguments)]
fn seed_lifecycle_and_mapping_scenarios(
    committee_actor: &AuthenticatedActorContext,
    vendor_actor: &AuthenticatedActorContext,
    object_storage_upload_pipeline: &ObjectStorageUploadPipeline,
    vendor_id: &VendorId,
    plant_id: &PlantId,
    delivery_epoch_day: i32,
    compliance_lifecycle: &mut VendorComplianceLifecycle,
    delivery_policy: &mut VendorPlantDeliveryPolicy,
) -> Result<(), String> {
    let lifecycle_vendor_id =
        VendorId::parse(DEFAULT_SEED_LIFECYCLE_VENDOR_ID).map_err(|error| error.to_string())?;
    let lifecycle_template_id = DocumentTemplateId::parse(DEFAULT_SEED_LIFECYCLE_TEMPLATE_ID)
        .map_err(|error| error.to_string())?;
    let vendor_category =
        VendorCategory::parse("LIFECYCLE_SEED").map_err(|error| error.to_string())?;
    let lifecycle_submitted_on =
        ComplianceDate::from_epoch_day(delivery_epoch_day.saturating_sub(14));
    let lifecycle_reviewed_on =
        ComplianceDate::from_epoch_day(delivery_epoch_day.saturating_sub(13));

    compliance_lifecycle
        .upsert_document_template(
            committee_actor,
            ComplianceDocumentTemplate::new(
                lifecycle_template_id.clone(),
                vendor_category.clone(),
                "Food Safety Certificate",
                true,
                365,
                vec![30, 7],
                0,
            )
            .map_err(|error| error.to_string())?,
        )
        .map_err(|error| error.to_string())?;

    compliance_lifecycle
        .register_vendor_application(
            vendor_actor,
            lifecycle_vendor_id.clone(),
            "Lifecycle Seed Vendor",
            vendor_category,
            lifecycle_submitted_on,
        )
        .map_err(|error| error.to_string())?;
    compliance_lifecycle
        .submit_document(
            vendor_actor,
            &lifecycle_vendor_id,
            &lifecycle_template_id,
            VendorDocumentSubmission::new(
                provision_seeded_compliance_document_ref(
                    object_storage_upload_pipeline,
                    &lifecycle_vendor_id,
                    "lifecycle-seed-license.pdf",
                )?,
                lifecycle_submitted_on,
                ComplianceDate::from_epoch_day(delivery_epoch_day.saturating_add(7)),
            )
            .map_err(|error| error.to_string())?,
        )
        .map_err(|error| error.to_string())?;
    compliance_lifecycle
        .review_application(
            committee_actor,
            &lifecycle_vendor_id,
            VendorReviewDecision::Approved,
            "Lifecycle seed vendor approved for lifecycle compliance baseline.",
            lifecycle_reviewed_on,
        )
        .map_err(|error| error.to_string())?;

    compliance_lifecycle
        .run_lifecycle(
            committee_actor,
            ComplianceDate::from_epoch_day(delivery_epoch_day),
        )
        .map_err(|error| error.to_string())?;
    compliance_lifecycle
        .run_lifecycle(
            committee_actor,
            ComplianceDate::from_epoch_day(delivery_epoch_day.saturating_add(8)),
        )
        .map_err(|error| error.to_string())?;
    compliance_lifecycle
        .submit_document(
            vendor_actor,
            &lifecycle_vendor_id,
            &lifecycle_template_id,
            VendorDocumentSubmission::new(
                provision_seeded_compliance_document_ref(
                    object_storage_upload_pipeline,
                    &lifecycle_vendor_id,
                    "lifecycle-seed-license-renewed.pdf",
                )?,
                ComplianceDate::from_epoch_day(delivery_epoch_day.saturating_add(8)),
                ComplianceDate::from_epoch_day(delivery_epoch_day.saturating_add(365)),
            )
            .map_err(|error| error.to_string())?,
        )
        .map_err(|error| error.to_string())?;
    compliance_lifecycle
        .run_lifecycle(
            committee_actor,
            ComplianceDate::from_epoch_day(delivery_epoch_day.saturating_add(9)),
        )
        .map_err(|error| error.to_string())?;

    seed_delivery_mapping_scenarios(vendor_id, plant_id, delivery_epoch_day, delivery_policy)
}

fn seed_delivery_mapping_scenarios(
    vendor_id: &VendorId,
    plant_id: &PlantId,
    delivery_epoch_day: i32,
    delivery_policy: &mut VendorPlantDeliveryPolicy,
) -> Result<(), String> {
    let committee_actor = load_gate_committee_admin_actor().map_err(|(_, error)| error.message)?;
    let lifecycle_vendor_id =
        VendorId::parse(DEFAULT_SEED_LIFECYCLE_VENDOR_ID).map_err(|error| error.to_string())?;
    let mapping_window_start = TaipeiBusinessMoment::new(delivery_epoch_day.saturating_sub(30), 0)
        .map_err(|error| error.to_string())?;
    let mapping_window_end =
        TaipeiBusinessMoment::new(delivery_epoch_day.saturating_add(30), 23 * 60 + 59)
            .map_err(|error| error.to_string())?;

    delivery_policy
        .upsert_mapping(
            &committee_actor,
            TaipeiBusinessMoment::new(delivery_epoch_day.saturating_sub(15), 1)
                .map_err(|error| error.to_string())?,
            VendorPlantDeliveryMapping::new(
                DeliveryMappingId::parse(DEFAULT_SEED_LIFECYCLE_ALLOW_MAPPING_ID)
                    .map_err(|error| error.to_string())?,
                lifecycle_vendor_id,
                plant_id.clone(),
                ServiceWindow::new(mapping_window_start, mapping_window_end)
                    .map_err(|error| error.to_string())?,
                DeliveryRuleEffect::Allow,
                90,
            ),
        )
        .map_err(|error| error.to_string())?;

    let deny_plant_id =
        PlantId::parse(DEFAULT_SEED_DENY_PLANT_ID).map_err(|error| error.to_string())?;
    delivery_policy
        .upsert_mapping(
            &committee_actor,
            TaipeiBusinessMoment::new(delivery_epoch_day.saturating_sub(15), 2)
                .map_err(|error| error.to_string())?,
            VendorPlantDeliveryMapping::new(
                DeliveryMappingId::parse(DEFAULT_SEED_DENY_MAPPING_ID)
                    .map_err(|error| error.to_string())?,
                vendor_id.clone(),
                deny_plant_id,
                ServiceWindow::new(mapping_window_start, mapping_window_end)
                    .map_err(|error| error.to_string())?,
                DeliveryRuleEffect::Deny,
                110,
            ),
        )
        .map_err(|error| error.to_string())?;

    Ok(())
}

#[allow(clippy::too_many_arguments)]
fn seed_payroll_dispute_scenario(
    vendor_id: &VendorId,
    plant_id: &PlantId,
    delivery_epoch_day: i32,
    compliance_lifecycle: &mut VendorComplianceLifecycle,
    delivery_policy: &mut VendorPlantDeliveryPolicy,
    menu_supply_policy: &MenuSupplyPolicy,
    payroll_ledger_service: &PayrollLedgerService,
) -> Result<(), String> {
    let employee_actor = AuthenticatedActorContext::new(
        ActorId::parse(DEFAULT_SEED_DISPUTE_EMPLOYEE_ACTOR_ID)
            .map_err(|error| error.to_string())?,
        Role::Employee,
        PlantScope::restricted(vec![plant_id.clone()]).map_err(|error| error.to_string())?,
        AuthenticationSource::CorporateSso,
    )
    .map_err(|error| error.to_string())?;
    let payroll_actor = load_gate_payroll_actor().map_err(|(_, error)| error.message)?;
    let triage_owner_actor_id =
        ActorId::parse("payroll-dispute-owner-seed").map_err(|error| error.to_string())?;

    let order_id =
        OrderId::parse(DEFAULT_SEED_DISPUTE_ORDER_ID).map_err(|error| error.to_string())?;
    let ordering_gateway = HttpOrderingExecutionGateway::new(
        compliance_lifecycle,
        delivery_policy,
        menu_supply_policy,
    );
    let ordered_at = TaipeiBusinessMoment::new(delivery_epoch_day.saturating_sub(1), 600)
        .map_err(|error| error.to_string())?;
    ordering_gateway
        .execute_create_employee_order(
            &employee_actor,
            order_id.clone(),
            vendor_id,
            plant_id,
            delivery_epoch_day,
            vec![OrderLineItemRequest::new(
                MenuItemId::parse("menu-1").map_err(|error| error.to_string())?,
                1,
                vec![SpecialRequest::NoUtensils],
            )
            .map_err(|error| error.to_string())?],
            ordered_at,
        )
        .map_err(|error| error.to_string())?;

    let seeded_snapshot = menu_supply_policy
        .order_snapshot(&order_id)
        .map_err(|error| error.to_string())?
        .ok_or_else(|| {
            format!(
                "seeded payroll dispute order `{}` was not found",
                order_id.as_str()
            )
        })?;
    seed_reconcile_order_snapshot(
        menu_supply_policy,
        payroll_ledger_service,
        &employee_actor,
        "seedDisputeScenario",
        &seeded_snapshot,
        ordered_at,
    )?;

    let opened_at = AuditTimestamp::from_taipei_business_moment(delivery_epoch_day, 601)
        .map_err(|error| error.to_string())?;
    let default_owner_actor_id =
        load_gate_payroll_dispute_owner_actor_id().map_err(|(_, error)| error.message)?;
    let dispute = payroll_ledger_service
        .open_dispute(
            &employee_actor,
            &order_id,
            &default_owner_actor_id,
            "Seed baseline dispute: item quality issue.",
            opened_at,
        )
        .map_err(|error| error.to_string())?;
    payroll_ledger_service
        .assign_dispute_owner(
            &payroll_actor,
            dispute.dispute_id(),
            &triage_owner_actor_id,
            AuditTimestamp::from_taipei_business_moment(delivery_epoch_day, 602)
                .map_err(|error| error.to_string())?,
            Some("Seed baseline triage assignment".to_owned()),
        )
        .map_err(|error| error.to_string())?;
    payroll_ledger_service
        .resolve_dispute_refund(
            &payroll_actor,
            dispute.dispute_id(),
            AuditTimestamp::from_taipei_business_moment(delivery_epoch_day, 603)
                .map_err(|error| error.to_string())?,
            "Seed baseline partial refund approved.",
            Some(6000),
        )
        .map_err(|error| error.to_string())?;

    Ok(())
}

fn seed_reconcile_order_snapshot(
    menu_supply_policy: &MenuSupplyPolicy,
    payroll_ledger_service: &PayrollLedgerService,
    employee_actor: &AuthenticatedActorContext,
    operation_id: &str,
    snapshot: &OrderSnapshot,
    occurred_at: TaipeiBusinessMoment,
) -> Result<(), String> {
    let mut total_minor: u64 = 0;
    let mut currency: Option<String> = None;
    for (menu_item_id, quantity) in snapshot.line_items() {
        let menu_item = menu_supply_policy
            .menu_item(menu_item_id)
            .map_err(|error| error.to_string())?
            .ok_or_else(|| {
                format!(
                    "seed payroll reconciliation references missing menu item `{}`",
                    menu_item_id.as_str()
                )
            })?;
        match currency.as_deref() {
            Some(existing_currency) if existing_currency != menu_item.price().currency() => {
                return Err(format!(
                    "seed payroll reconciliation found mixed currencies `{existing_currency}` and `{}`",
                    menu_item.price().currency()
                ));
            }
            Some(_) => {}
            None => currency = Some(menu_item.price().currency().to_owned()),
        }
        total_minor = total_minor
            .checked_add(u64::from(menu_item.price().amount_minor()) * u64::from(*quantity))
            .ok_or_else(|| "seed payroll reconciliation amount overflowed".to_owned())?;
    }
    let currency = currency
        .ok_or_else(|| "seed payroll reconciliation requires at least one line item".to_owned())?;
    let total_minor = u32::try_from(total_minor)
        .map_err(|_| "seed payroll reconciliation amount exceeded supported range".to_owned())?;
    let source_event = PayrollLedgerSourceRef::new(
        PayrollLedgerSourceKind::OrderMutation,
        format!(
            "order:{}:state:{}",
            snapshot.order_id().as_str(),
            snapshot.state().as_str()
        ),
    )
    .map_err(|error| error.to_string())?;
    payroll_ledger_service
        .reconcile_order_charge(
            employee_actor,
            operation_id,
            snapshot.order_id(),
            snapshot.employee_actor_id(),
            employee_actor.employment_status(),
            snapshot.delivery_epoch_day(),
            &currency,
            expected_payroll_target_amount(snapshot, total_minor),
            AuditTimestamp::from_taipei_business_moment(
                occurred_at.epoch_day(),
                occurred_at.minute_of_day(),
            )
            .map_err(|error| error.to_string())?,
            source_event,
        )
        .map_err(|error| error.to_string())?;
    Ok(())
}

fn seed_anomaly_alert_scenario(
    committee_actor: &AuthenticatedActorContext,
    vendor_id: &VendorId,
    delivery_epoch_day: i32,
    anomaly_alert_workflow: &AnomalyAlertWorkflow,
) -> Result<(), String> {
    let observed_at = AuditTimestamp::from_taipei_business_moment(delivery_epoch_day, 900)
        .map_err(|error| error.to_string())?;
    let default_owner_actor_id =
        load_gate_anomaly_alert_owner_actor_id().map_err(|(_, error)| error.message)?;
    let evaluation = anomaly_alert_workflow
        .evaluate_rules(
            committee_actor,
            AnomalySignalSnapshot::new(vendor_id.clone(), observed_at).with_on_time_rate(Some(0.8)),
            &default_owner_actor_id,
        )
        .map_err(|error| error.to_string())?;
    let seeded_alert_id = evaluation
        .triggered_alerts()
        .iter()
        .find(|alert| alert.rule_kind() == AnomalyRuleKind::OnTimeDegradation)
        .or_else(|| evaluation.triggered_alerts().first())
        .map(|alert| alert.alert_id().clone())
        .ok_or_else(|| "seed anomaly baseline failed to trigger any alert".to_owned())?;

    let triage_owner_actor_id =
        ActorId::parse("committee-owner-seed").map_err(|error| error.to_string())?;
    anomaly_alert_workflow
        .assign_owner(
            committee_actor,
            &seeded_alert_id,
            &triage_owner_actor_id,
            AuditTimestamp::from_taipei_business_moment(delivery_epoch_day, 905)
                .map_err(|error| error.to_string())?,
            Some("Seed baseline ownership assignment.".to_owned()),
        )
        .map_err(|error| error.to_string())?;
    anomaly_alert_workflow
        .transition_alert(
            committee_actor,
            &seeded_alert_id,
            AnomalyAlertTransition::StartRemediation,
            AuditTimestamp::from_taipei_business_moment(delivery_epoch_day, 910)
                .map_err(|error| error.to_string())?,
            Some("Seed baseline remediation started.".to_owned()),
            None,
            Vec::new(),
            None,
        )
        .map_err(|error| error.to_string())?;
    anomaly_alert_workflow
        .transition_alert(
            committee_actor,
            &seeded_alert_id,
            AnomalyAlertTransition::Close,
            AuditTimestamp::from_taipei_business_moment(delivery_epoch_day, 920)
                .map_err(|error| error.to_string())?,
            Some("Seed baseline closure approved.".to_owned()),
            Some("Seed baseline anomaly mitigated with vendor retraining.".to_owned()),
            vec![
                "runbook://anomaly/on-time-degradation".to_owned(),
                "evidence://seed/anomaly/on-time-degradation".to_owned(),
            ],
            Some("jira://SEED-ANOMALY-1".to_owned()),
        )
        .map_err(|error| error.to_string())?;

    Ok(())
}

async fn ready_probe() -> (StatusCode, Json<HealthPayload>) {
    health_probe_response(HealthProbeKind::Readiness, true, "dependencies ready")
}

async fn live_probe() -> (StatusCode, Json<HealthPayload>) {
    health_probe_response(HealthProbeKind::Liveness, true, "heartbeat healthy")
}

async fn startup_probe() -> (StatusCode, Json<HealthPayload>) {
    health_probe_response(HealthProbeKind::Startup, true, "startup complete")
}

fn health_probe_response(
    probe_kind: HealthProbeKind,
    dependencies_ready: bool,
    detail: &str,
) -> (StatusCode, Json<HealthPayload>) {
    let operation_id = match probe_kind {
        HealthProbeKind::Readiness => "healthReadyProbe",
        HealthProbeKind::Liveness => "healthLiveProbe",
        HealthProbeKind::Startup => "healthStartupProbe",
    };
    let telemetry = TelemetryService::HttpApi.begin_operation(operation_id, None, None);

    let report = evaluate_probe(probe_kind, dependencies_ready, detail);
    let (status_code, status_text) = match report.state() {
        HealthState::Healthy => (StatusCode::OK, "ok"),
        HealthState::Unhealthy => (StatusCode::SERVICE_UNAVAILABLE, "degraded"),
    };
    telemetry.finish_with_http_status(status_code.as_u16());

    (
        status_code,
        Json(HealthPayload {
            status: status_text,
            probe: report.probe_kind().path(),
            detail: report.detail().to_owned(),
        }),
    )
}

async fn list_employee_orders(
    State(state): State<AppState>,
    headers: HeaderMap,
    Query(query): Query<EmployeeOrderListQuery>,
) -> (StatusCode, Json<serde_json::Value>) {
    let telemetry = TelemetryService::HttpApi.begin_operation(
        "listEmployeeOrders",
        None::<&str>,
        Some(state.plant_id.as_str()),
    );
    let request_id = telemetry.correlation_context().request_id().to_owned();
    let employee_actor = match require_corporate_actor_for_role(&headers, Role::Employee) {
        Ok(actor) => actor,
        Err((status, error)) => {
            telemetry.finish_with_http_status(status.as_u16());
            return (
                status,
                Json(
                    serde_json::to_value(error.with_request_id(request_id.as_str()))
                        .expect("authorization error payload serialization should succeed"),
                ),
            );
        }
    };

    let response = match handle_list_employee_orders(&state, &employee_actor, query) {
        Ok(payload) => {
            telemetry.finish_with_http_status(StatusCode::OK.as_u16());
            (
                StatusCode::OK,
                Json(
                    serde_json::to_value(payload)
                        .expect("employee order page payload serialization should succeed"),
                ),
            )
        }
        Err((status, error)) => {
            telemetry.finish_with_http_status(status.as_u16());
            (
                status,
                Json(
                    serde_json::to_value(error.with_request_id(request_id.as_str()))
                        .expect("employee order page error payload serialization should succeed"),
                ),
            )
        }
    };

    response
}

fn handle_list_employee_orders(
    state: &AppState,
    employee_actor: &AuthenticatedActorContext,
    query: EmployeeOrderListQuery,
) -> Result<EmployeeOrderPagePayload, (StatusCode, ErrorPayload)> {
    if employee_actor.role() != Role::Employee {
        return Err(domain_error(
            StatusCode::FORBIDDEN,
            "FORBIDDEN",
            format!(
                "operation requires role {:?}, got {:?}",
                Role::Employee,
                employee_actor.role()
            ),
        ));
    }

    let request_plant_id = query.plant_id.as_deref().ok_or_else(|| {
        domain_error(
            StatusCode::BAD_REQUEST,
            "BAD_REQUEST",
            "plantId query parameter is required".to_owned(),
        )
    })?;
    if request_plant_id != state.plant_id.as_str() {
        return Err(domain_error(
            StatusCode::BAD_REQUEST,
            "UNSUPPORTED_PLANT_ID",
            format!(
                "plantId `{request_plant_id}` is unsupported by this runtime, expected `{}`",
                state.plant_id.as_str()
            ),
        ));
    }
    if !employee_actor.plant_scope().contains(&state.plant_id) {
        return Err(domain_error(
            StatusCode::FORBIDDEN,
            "FORBIDDEN",
            format!(
                "actor `{}` is not authorized for plant `{}`",
                employee_actor.actor_id().as_str(),
                state.plant_id.as_str()
            ),
        ));
    }

    let now_epoch_day = current_taipei_business_moment()
        .map_err(|error| {
            domain_error(
                StatusCode::INTERNAL_SERVER_ERROR,
                "TIME_RESOLUTION_FAILED",
                error,
            )
        })?
        .epoch_day();
    let (from_epoch_day, to_epoch_day) = resolve_order_query_range(
        query.from_date.as_deref(),
        query.to_date.as_deref(),
        now_epoch_day,
    )?;
    let status_filter = query
        .status
        .as_deref()
        .map(parse_order_status_filter)
        .transpose()?;

    let page = query.page.unwrap_or(1);
    if page == 0 {
        return Err(domain_error(
            StatusCode::BAD_REQUEST,
            "BAD_REQUEST",
            "page must be greater than or equal to 1".to_owned(),
        ));
    }
    let page_size = query.page_size.unwrap_or(20);
    if page_size == 0 || page_size > 200 {
        return Err(domain_error(
            StatusCode::BAD_REQUEST,
            "BAD_REQUEST",
            "pageSize must be between 1 and 200".to_owned(),
        ));
    }

    let mut snapshots = with_menu_supply_policy(state, |menu_supply_policy| {
        menu_supply_policy
            .order_snapshots_for_employee(employee_actor.actor_id())
            .map_err(|error| {
                domain_error(
                    StatusCode::INTERNAL_SERVER_ERROR,
                    "ORDER_POLICY_VIOLATION",
                    error.to_string(),
                )
            })
    })?;
    snapshots.retain(|snapshot| {
        snapshot.plant_id() == &state.plant_id
            && snapshot.delivery_epoch_day() >= from_epoch_day
            && snapshot.delivery_epoch_day() <= to_epoch_day
            && status_filter
                .map(|status| snapshot.state() == status)
                .unwrap_or(true)
    });

    let sort_by = query
        .sort_by
        .unwrap_or(EmployeeOrderSortFieldQuery::CreatedAt);
    let sort_order = query.sort_order.unwrap_or(SortOrderQuery::Desc);
    snapshots
        .sort_by(|left, right| compare_employee_order_snapshot(left, right, sort_by, sort_order));

    let total_items = snapshots.len();
    let total_pages = if total_items == 0 {
        0
    } else {
        (total_items - 1) / page_size + 1
    };
    let start = page.saturating_sub(1).saturating_mul(page_size);
    let end = start.saturating_add(page_size).min(total_items);
    let paged_snapshots = if start >= total_items {
        Vec::new()
    } else {
        snapshots[start..end].to_vec()
    };

    let mut items = Vec::with_capacity(paged_snapshots.len());
    for snapshot in &paged_snapshots {
        items.push(build_employee_order_payload(state, snapshot)?);
    }

    Ok(EmployeeOrderPagePayload {
        items,
        page: PageMetaPayload {
            page,
            page_size,
            total_items,
            total_pages,
        },
    })
}

async fn list_vendor_orders(
    State(state): State<AppState>,
    headers: HeaderMap,
    Query(query): Query<VendorOrderListQuery>,
) -> (StatusCode, Json<serde_json::Value>) {
    let telemetry = TelemetryService::HttpApi.begin_operation(
        "listVendorOrders",
        None::<&str>,
        Some(state.plant_id.as_str()),
    );
    let request_id = telemetry.correlation_context().request_id().to_owned();
    let vendor_actor = match require_vendor_operator_actor(&headers) {
        Ok(actor) => actor,
        Err((status, error)) => {
            telemetry.finish_with_http_status(status.as_u16());
            return (
                status,
                Json(
                    serde_json::to_value(error.with_request_id(request_id.as_str()))
                        .expect("authorization error payload serialization should succeed"),
                ),
            );
        }
    };

    let response = match handle_list_vendor_orders(&state, &vendor_actor, query) {
        Ok(payload) => {
            telemetry.finish_with_http_status(StatusCode::OK.as_u16());
            (
                StatusCode::OK,
                Json(
                    serde_json::to_value(payload)
                        .expect("vendor order page payload serialization should succeed"),
                ),
            )
        }
        Err((status, error)) => {
            telemetry.finish_with_http_status(status.as_u16());
            (
                status,
                Json(
                    serde_json::to_value(error.with_request_id(request_id.as_str()))
                        .expect("vendor order page error payload serialization should succeed"),
                ),
            )
        }
    };

    response
}

fn handle_list_vendor_orders(
    state: &AppState,
    vendor_actor: &AuthenticatedActorContext,
    query: VendorOrderListQuery,
) -> Result<VendorOrderPagePayload, (StatusCode, ErrorPayload)> {
    if vendor_actor.role() != Role::VendorOperator {
        return Err(domain_error(
            StatusCode::FORBIDDEN,
            "FORBIDDEN",
            format!(
                "operation requires role {:?}, got {:?}",
                Role::VendorOperator,
                vendor_actor.role()
            ),
        ));
    }

    let request_plant_id = query.plant_id.as_deref().ok_or_else(|| {
        domain_error(
            StatusCode::BAD_REQUEST,
            "BAD_REQUEST",
            "plantId query parameter is required".to_owned(),
        )
    })?;
    if request_plant_id != state.plant_id.as_str() {
        return Err(domain_error(
            StatusCode::BAD_REQUEST,
            "UNSUPPORTED_PLANT_ID",
            format!(
                "plantId `{request_plant_id}` is unsupported by this runtime, expected `{}`",
                state.plant_id.as_str()
            ),
        ));
    }
    if !vendor_actor.plant_scope().contains(&state.plant_id) {
        return Err(domain_error(
            StatusCode::FORBIDDEN,
            "FORBIDDEN",
            format!(
                "actor `{}` is not authorized for plant `{}`",
                vendor_actor.actor_id().as_str(),
                state.plant_id.as_str()
            ),
        ));
    }

    let now_epoch_day = current_taipei_business_moment()
        .map_err(|error| {
            domain_error(
                StatusCode::INTERNAL_SERVER_ERROR,
                "TIME_RESOLUTION_FAILED",
                error,
            )
        })?
        .epoch_day();
    let (from_epoch_day, to_epoch_day) = resolve_order_query_range(
        query.from_date.as_deref(),
        query.to_date.as_deref(),
        now_epoch_day,
    )?;
    let status_filter = query
        .status
        .as_deref()
        .map(parse_order_status_filter)
        .transpose()?;

    let page = query.page.unwrap_or(1);
    if page == 0 {
        return Err(domain_error(
            StatusCode::BAD_REQUEST,
            "BAD_REQUEST",
            "page must be greater than or equal to 1".to_owned(),
        ));
    }
    let page_size = query.page_size.unwrap_or(20);
    if page_size == 0 || page_size > 200 {
        return Err(domain_error(
            StatusCode::BAD_REQUEST,
            "BAD_REQUEST",
            "pageSize must be between 1 and 200".to_owned(),
        ));
    }

    let mut snapshots = with_menu_supply_policy(state, |menu_supply_policy| {
        menu_supply_policy
            .order_snapshots_for_vendor_date_range(&state.vendor_id, from_epoch_day, to_epoch_day)
            .map_err(|error| {
                domain_error(
                    StatusCode::INTERNAL_SERVER_ERROR,
                    "ORDER_POLICY_VIOLATION",
                    error.to_string(),
                )
            })
    })?;
    snapshots.retain(|snapshot| {
        snapshot.plant_id() == &state.plant_id
            && status_filter
                .map(|status| snapshot.state() == status)
                .unwrap_or(true)
    });

    let sort_by = query
        .sort_by
        .unwrap_or(VendorOrderSortFieldQuery::DeliveryDate);
    let sort_order = query.sort_order.unwrap_or(SortOrderQuery::Asc);
    snapshots
        .sort_by(|left, right| compare_vendor_order_snapshot(left, right, sort_by, sort_order));

    let total_items = snapshots.len();
    let total_pages = if total_items == 0 {
        0
    } else {
        (total_items - 1) / page_size + 1
    };
    let start = page.saturating_sub(1).saturating_mul(page_size);
    let end = start.saturating_add(page_size).min(total_items);
    let paged_snapshots = if start >= total_items {
        Vec::new()
    } else {
        snapshots[start..end].to_vec()
    };

    let mut items = Vec::with_capacity(paged_snapshots.len());
    for snapshot in &paged_snapshots {
        items.push(build_vendor_order_board_entry_payload(state, snapshot)?);
    }

    Ok(VendorOrderPagePayload {
        items,
        page: PageMetaPayload {
            page,
            page_size,
            total_items,
            total_pages,
        },
    })
}

async fn create_vendor_object_storage_upload_plan(
    State(state): State<AppState>,
    headers: HeaderMap,
    Json(request): Json<ObjectStorageUploadRequestPayload>,
) -> (StatusCode, Json<serde_json::Value>) {
    let telemetry = TelemetryService::HttpApi.begin_operation(
        "createVendorObjectStorageUploadPlan",
        None::<&str>,
        Some(state.plant_id.as_str()),
    );
    let request_id = telemetry.correlation_context().request_id().to_owned();
    let vendor_actor = match require_vendor_operator_actor(&headers) {
        Ok(actor) => actor,
        Err((status, error)) => {
            telemetry.finish_with_http_status(status.as_u16());
            return (
                status,
                Json(
                    serde_json::to_value(error.with_request_id(request_id.as_str()))
                        .expect("authorization error payload serialization should succeed"),
                ),
            );
        }
    };

    let response =
        match handle_create_vendor_object_storage_upload_plan(&state, &vendor_actor, request) {
            Ok(payload) => {
                telemetry.finish_with_http_status(StatusCode::OK.as_u16());
                (
                    StatusCode::OK,
                    Json(
                        serde_json::to_value(payload)
                            .expect("object storage upload payload serialization should succeed"),
                    ),
                )
            }
            Err((status, error)) => {
                telemetry.finish_with_http_status(status.as_u16());
                (
                    status,
                    Json(
                        serde_json::to_value(error.with_request_id(request_id.as_str())).expect(
                            "object storage upload error payload serialization should succeed",
                        ),
                    ),
                )
            }
        };

    response
}

fn handle_create_vendor_object_storage_upload_plan(
    state: &AppState,
    vendor_actor: &AuthenticatedActorContext,
    request: ObjectStorageUploadRequestPayload,
) -> Result<ObjectStorageUploadPlanPayload, (StatusCode, ErrorPayload)> {
    if vendor_actor.role() != Role::VendorOperator {
        return Err(domain_error(
            StatusCode::FORBIDDEN,
            "FORBIDDEN",
            format!(
                "operation requires role {:?}, got {:?}",
                Role::VendorOperator,
                vendor_actor.role()
            ),
        ));
    }
    if !vendor_actor.plant_scope().contains(&state.plant_id) {
        return Err(domain_error(
            StatusCode::FORBIDDEN,
            "FORBIDDEN",
            format!(
                "actor `{}` is not authorized for plant `{}`",
                vendor_actor.actor_id().as_str(),
                state.plant_id.as_str()
            ),
        ));
    }

    let locale = StorageLocale::from_language_tag(request.locale.as_deref());
    let artifact_class = parse_storage_artifact_class_label(request.artifact_class.as_str())
        .ok_or_else(|| {
            domain_error(
                StatusCode::BAD_REQUEST,
                "OBJECT_STORAGE_INVALID_ARTIFACT_CLASS",
                localized_invalid_artifact_class_message(locale, request.artifact_class.as_str()),
            )
        })?;
    let plan = state
        .object_storage_upload_pipeline
        .create_upload_plan(
            ObjectUploadIntent {
                artifact_class,
                owner_scope: Some(state.vendor_id.as_str().to_owned()),
                file_name: request.file_name,
                mime_type: request.mime_type,
                size_bytes: request.size_bytes,
                thumbnail_size_bytes: request.thumbnail_size_bytes,
            },
            SystemTime::now(),
        )
        .map_err(|error| map_object_storage_error(error, locale))?;
    Ok(to_object_storage_upload_plan_payload(plan))
}

async fn create_vendor_object_storage_access_link(
    State(state): State<AppState>,
    headers: HeaderMap,
    Json(request): Json<ObjectStorageAccessLinkRequestPayload>,
) -> (StatusCode, Json<serde_json::Value>) {
    let telemetry = TelemetryService::HttpApi.begin_operation(
        "createVendorObjectStorageAccessLink",
        None::<&str>,
        Some(state.plant_id.as_str()),
    );
    let request_id = telemetry.correlation_context().request_id().to_owned();
    let vendor_actor = match require_vendor_operator_actor(&headers) {
        Ok(actor) => actor,
        Err((status, error)) => {
            telemetry.finish_with_http_status(status.as_u16());
            return (
                status,
                Json(
                    serde_json::to_value(error.with_request_id(request_id.as_str()))
                        .expect("authorization error payload serialization should succeed"),
                ),
            );
        }
    };
    let response =
        match handle_create_vendor_object_storage_access_link(&state, &vendor_actor, request) {
            Ok(payload) => {
                telemetry.finish_with_http_status(StatusCode::OK.as_u16());
                (
                    StatusCode::OK,
                    Json(
                        serde_json::to_value(payload).expect(
                            "object storage access-link payload serialization should succeed",
                        ),
                    ),
                )
            }
            Err((status, error)) => {
                telemetry.finish_with_http_status(status.as_u16());
                (
                    status,
                    Json(
                        serde_json::to_value(error.with_request_id(request_id.as_str())).expect(
                            "object storage access-link error payload serialization should succeed",
                        ),
                    ),
                )
            }
        };

    response
}

async fn create_admin_object_storage_access_link(
    State(state): State<AppState>,
    headers: HeaderMap,
    Json(request): Json<ObjectStorageAccessLinkRequestPayload>,
) -> (StatusCode, Json<serde_json::Value>) {
    let telemetry = TelemetryService::HttpApi.begin_operation(
        "createAdminObjectStorageAccessLink",
        None::<&str>,
        Some(state.plant_id.as_str()),
    );
    let request_id = telemetry.correlation_context().request_id().to_owned();
    let _committee_actor = match require_corporate_actor_for_role(&headers, Role::CommitteeAdmin) {
        Ok(actor) => actor,
        Err((status, error)) => {
            telemetry.finish_with_http_status(status.as_u16());
            return (
                status,
                Json(
                    serde_json::to_value(error.with_request_id(request_id.as_str()))
                        .expect("authorization error payload serialization should succeed"),
                ),
            );
        }
    };
    let response = match handle_create_admin_object_storage_access_link(&state, request) {
        Ok(payload) => {
            telemetry.finish_with_http_status(StatusCode::OK.as_u16());
            (
                StatusCode::OK,
                Json(
                    serde_json::to_value(payload)
                        .expect("object storage access-link payload serialization should succeed"),
                ),
            )
        }
        Err((status, error)) => {
            telemetry.finish_with_http_status(status.as_u16());
            (
                status,
                Json(
                    serde_json::to_value(error.with_request_id(request_id.as_str())).expect(
                        "object storage access-link error payload serialization should succeed",
                    ),
                ),
            )
        }
    };

    response
}

fn handle_create_vendor_object_storage_access_link(
    state: &AppState,
    vendor_actor: &AuthenticatedActorContext,
    request: ObjectStorageAccessLinkRequestPayload,
) -> Result<ObjectStorageAccessLinkPayload, (StatusCode, ErrorPayload)> {
    if vendor_actor.role() != Role::VendorOperator {
        return Err(domain_error(
            StatusCode::FORBIDDEN,
            "FORBIDDEN",
            format!(
                "operation requires role {:?}, got {:?}",
                Role::VendorOperator,
                vendor_actor.role()
            ),
        ));
    }
    if !vendor_actor.plant_scope().contains(&state.plant_id) {
        return Err(domain_error(
            StatusCode::FORBIDDEN,
            "FORBIDDEN",
            format!(
                "actor `{}` is not authorized for plant `{}`",
                vendor_actor.actor_id().as_str(),
                state.plant_id.as_str()
            ),
        ));
    }
    handle_create_object_storage_access_link(state, request, Some(vendor_actor))
}

fn handle_create_admin_object_storage_access_link(
    state: &AppState,
    request: ObjectStorageAccessLinkRequestPayload,
) -> Result<ObjectStorageAccessLinkPayload, (StatusCode, ErrorPayload)> {
    handle_create_object_storage_access_link(state, request, None)
}

fn handle_create_object_storage_access_link(
    state: &AppState,
    request: ObjectStorageAccessLinkRequestPayload,
    vendor_actor: Option<&AuthenticatedActorContext>,
) -> Result<ObjectStorageAccessLinkPayload, (StatusCode, ErrorPayload)> {
    let locale = StorageLocale::from_language_tag(request.locale.as_deref());
    let object_ref = ObjectStorageReference::parse(request.object_ref)
        .map_err(|error| map_object_storage_error(error, locale))?;
    if let Some(vendor_actor) = vendor_actor {
        ensure_vendor_object_storage_access(state, vendor_actor, &object_ref)?;
    }
    let plan = state
        .object_storage_upload_pipeline
        .create_download_plan(&object_ref, SystemTime::now())
        .map_err(|error| map_object_storage_error(error, locale))?;
    Ok(to_object_storage_access_link_payload(plan))
}

fn ensure_vendor_object_storage_access(
    state: &AppState,
    _vendor_actor: &AuthenticatedActorContext,
    object_ref: &ObjectStorageReference,
) -> Result<(), (StatusCode, ErrorPayload)> {
    let owned_by_scope =
        object_ref_matches_vendor_owner_scope(state, object_ref, state.vendor_id.as_str());
    let referenced_by_menu = vendor_has_menu_image_reference(state, &state.vendor_id, object_ref)?;
    let referenced_by_compliance =
        vendor_has_compliance_document_reference(state, &state.vendor_id, object_ref)?;
    if owned_by_scope || referenced_by_menu || referenced_by_compliance {
        return Ok(());
    }
    Err(domain_error(
        StatusCode::FORBIDDEN,
        "FORBIDDEN",
        format!(
            "object reference `{}` is not accessible for vendor `{}`",
            object_ref.as_str(),
            state.vendor_id.as_str()
        ),
    ))
}

fn vendor_has_menu_image_reference(
    state: &AppState,
    vendor_id: &VendorId,
    object_ref: &ObjectStorageReference,
) -> Result<bool, (StatusCode, ErrorPayload)> {
    with_menu_supply_policy(state, |menu_supply_policy| {
        menu_supply_policy
            .vendor_has_menu_image_reference(vendor_id, object_ref)
            .map_err(|error| {
                domain_error(
                    StatusCode::INTERNAL_SERVER_ERROR,
                    "ORDER_POLICY_VIOLATION",
                    format!("failed to inspect vendor menu image references: {error}"),
                )
            })
    })
}

fn vendor_has_compliance_document_reference(
    state: &AppState,
    vendor_id: &VendorId,
    object_ref: &ObjectStorageReference,
) -> Result<bool, (StatusCode, ErrorPayload)> {
    let lifecycle = load_compliance_lifecycle_snapshot(state)?;
    Ok(lifecycle.vendor_has_document_reference(vendor_id, object_ref))
}

fn object_ref_matches_vendor_owner_scope(
    state: &AppState,
    object_ref: &ObjectStorageReference,
    vendor_scope: &str,
) -> bool {
    let (bucket, key) = object_ref.split_parts();
    let artifact_prefix = if bucket == state.object_storage_upload_pipeline.menu_bucket() {
        "menu-images"
    } else if bucket == state.object_storage_upload_pipeline.compliance_bucket() {
        "compliance-documents"
    } else if bucket == state.object_storage_upload_pipeline.fulfillment_bucket() {
        "fulfillment-artifacts"
    } else {
        return false;
    };
    object_key_matches_vendor_owner_scope(key, artifact_prefix, vendor_scope)
}

#[cfg(test)]
fn configured_menu_object_bucket() -> String {
    std::env::var(MINIO_BUCKET_MENU_IMAGES_ENV)
        .ok()
        .map(|bucket| bucket.trim().to_owned())
        .filter(|bucket| !bucket.is_empty())
        .unwrap_or_else(|| DEFAULT_MENU_IMAGE_BUCKET.to_owned())
}

#[cfg(test)]
fn configured_compliance_object_bucket() -> String {
    std::env::var(MINIO_BUCKET_COMPLIANCE_EVIDENCE_ENV)
        .ok()
        .map(|bucket| bucket.trim().to_owned())
        .filter(|bucket| !bucket.is_empty())
        .unwrap_or_else(|| DEFAULT_COMPLIANCE_BUCKET.to_owned())
}

#[cfg(test)]
fn configured_object_storage_key_namespace() -> String {
    std::env::var(OBJECT_STORAGE_KEY_NAMESPACE_ENV)
        .ok()
        .map(|namespace| namespace.trim().trim_matches('/').to_owned())
        .filter(|namespace| !namespace.is_empty())
        .unwrap_or_else(|| DEFAULT_OBJECT_STORAGE_KEY_NAMESPACE.to_owned())
}

fn object_key_matches_vendor_owner_scope(
    object_key: &str,
    artifact_prefix: &str,
    vendor_scope: &str,
) -> bool {
    let owner_scope = normalize_owner_scope_segment(vendor_scope);
    if owner_scope.is_empty() {
        return false;
    }
    let segments = object_key
        .split('/')
        .filter(|segment| !segment.is_empty())
        .collect::<Vec<_>>();
    segments
        .iter()
        .position(|segment| *segment == artifact_prefix)
        .and_then(|index| segments.get(index + 1))
        .is_some_and(|candidate| *candidate == owner_scope)
}

fn normalize_owner_scope_segment(value: &str) -> String {
    let mut normalized = String::with_capacity(value.len());
    for character in value.trim().chars() {
        if character.is_ascii_alphanumeric() || matches!(character, '-' | '_' | '.') {
            normalized.push(character.to_ascii_lowercase());
        } else {
            normalized.push('-');
        }
    }
    normalized.trim_matches('-').to_owned()
}

fn parse_storage_artifact_class_label(value: &str) -> Option<StorageArtifactClass> {
    match value.trim().to_ascii_uppercase().replace('-', "_").as_str() {
        "MENU_IMAGE" => Some(StorageArtifactClass::MenuImage),
        "MENU_IMAGE_THUMBNAIL" => Some(StorageArtifactClass::MenuImageThumbnail),
        "COMPLIANCE_DOCUMENT" => Some(StorageArtifactClass::ComplianceDocument),
        "FULFILLMENT_DAILY_SUMMARY" => Some(StorageArtifactClass::FulfillmentDailySummary),
        "FULFILLMENT_PLANT_PARTITION_SHEET" => {
            Some(StorageArtifactClass::FulfillmentPlantPartitionSheet)
        }
        "FULFILLMENT_LABELS" => Some(StorageArtifactClass::FulfillmentLabels),
        "FULFILLMENT_BASKET_LIST" => Some(StorageArtifactClass::FulfillmentBasketList),
        _ => None,
    }
}

fn localized_invalid_artifact_class_message(locale: StorageLocale, value: &str) -> String {
    match locale {
        StorageLocale::EnUs => format!("artifact class `{value}` is not supported"),
        StorageLocale::ZhTw => format!("artifact class `{value}` 不支援。"),
    }
}

fn map_object_storage_error(
    error: ObjectStorageError,
    locale: StorageLocale,
) -> (StatusCode, ErrorPayload) {
    let status = match error {
        ObjectStorageError::InvalidObjectReference(_)
        | ObjectStorageError::InvalidMimeType { .. }
        | ObjectStorageError::SizeLimitExceeded { .. }
        | ObjectStorageError::InvalidFileName(_) => StatusCode::BAD_REQUEST,
        ObjectStorageError::InvalidConfiguration(_) | ObjectStorageError::PresignFailed(_) => {
            StatusCode::INTERNAL_SERVER_ERROR
        }
    };
    domain_error(status, error.error_code(), error.localized_message(locale))
}

fn to_object_storage_upload_plan_payload(
    plan: PresignedUploadPlan,
) -> ObjectStorageUploadPlanPayload {
    let metadata = ObjectStorageUploadMetadataPayload {
        artifact_class: plan.metadata.artifact_class.as_str().to_owned(),
        file_name: plan.metadata.file_name,
        mime_type: plan.metadata.mime_type,
        size_bytes: plan.metadata.size_bytes,
        thumbnail_ref: plan
            .metadata
            .thumbnail_ref
            .map(|thumbnail_ref| thumbnail_ref.as_str().to_owned()),
    };
    ObjectStorageUploadPlanPayload {
        primary: to_object_storage_upload_target_payload(plan.primary),
        thumbnail: plan.thumbnail.map(to_object_storage_upload_target_payload),
        metadata,
    }
}

fn to_object_storage_upload_target_payload(
    target: PresignedUploadTarget,
) -> ObjectStorageUploadTargetPayload {
    ObjectStorageUploadTargetPayload {
        object_ref: target.object_ref.as_str().to_owned(),
        upload_url: target.upload_url,
        upload_expires_at_epoch_seconds: target.upload_expires_at_epoch_seconds,
        required_headers: target.required_headers,
    }
}

fn to_object_storage_access_link_payload(
    plan: PresignedDownloadPlan,
) -> ObjectStorageAccessLinkPayload {
    ObjectStorageAccessLinkPayload {
        object_ref: plan.object_ref.as_str().to_owned(),
        download_url: plan.download_url,
        download_expires_at_epoch_seconds: plan.download_expires_at_epoch_seconds,
    }
}

async fn list_employee_menus(
    State(state): State<AppState>,
    Query(query): Query<EmployeeMenuDiscoveryQuery>,
) -> (StatusCode, Json<serde_json::Value>) {
    let telemetry = TelemetryService::HttpApi.begin_operation(
        "listEmployeeMenus",
        Some("load-gate"),
        Some(state.plant_id.as_str()),
    );
    let request_id = telemetry.correlation_context().request_id().to_owned();

    let response = match handle_list_employee_menus(&state, query) {
        Ok(payload) => {
            telemetry.finish_with_http_status(StatusCode::OK.as_u16());
            (
                StatusCode::OK,
                Json(
                    serde_json::to_value(payload)
                        .expect("menu discovery payload serialization should succeed"),
                ),
            )
        }
        Err((status, error)) => {
            telemetry.finish_with_http_status(status.as_u16());
            (
                status,
                Json(
                    serde_json::to_value(error.with_request_id(request_id.as_str()))
                        .expect("menu discovery error payload serialization should succeed"),
                ),
            )
        }
    };

    response
}

async fn upsert_employee_rush_reminder_preferences(
    State(state): State<AppState>,
    Json(request): Json<EmployeeRushReminderPreferencesUpsertRequest>,
) -> (StatusCode, Json<serde_json::Value>) {
    let telemetry = TelemetryService::HttpApi.begin_operation(
        "upsertEmployeeRushReminderPreferences",
        Some(LOAD_GATE_EMPLOYEE_ACTOR_ID),
        Some(state.plant_id.as_str()),
    );
    let request_id = telemetry.correlation_context().request_id().to_owned();

    let response = match handle_upsert_employee_rush_reminder_preferences(&state, request) {
        Ok(payload) => {
            telemetry.finish_with_http_status(StatusCode::OK.as_u16());
            (
                StatusCode::OK,
                Json(
                    serde_json::to_value(payload)
                        .expect("rush reminder preferences payload serialization should succeed"),
                ),
            )
        }
        Err((status, error)) => {
            telemetry.finish_with_http_status(status.as_u16());
            (
                status,
                Json(
                    serde_json::to_value(error.with_request_id(request_id.as_str()))
                        .expect(
                            "rush reminder preference upsert error payload serialization should succeed",
                        ),
                ),
            )
        }
    };

    response
}

fn handle_upsert_employee_rush_reminder_preferences(
    state: &AppState,
    request: EmployeeRushReminderPreferencesUpsertRequest,
) -> Result<EmployeeRushReminderPreferencesPayload, (StatusCode, ErrorPayload)> {
    if !state.rush_reminder_runtime_enabled {
        return Err(domain_error(
            StatusCode::NOT_FOUND,
            "NOT_FOUND",
            "rush reminder preferences endpoint is unavailable while feature flag is disabled"
                .to_owned(),
        ));
    }

    if request.plant_id != state.plant_id.as_str() {
        return Err(domain_error(
            StatusCode::BAD_REQUEST,
            "UNSUPPORTED_PLANT_ID",
            format!(
                "plantId `{}` is unsupported by this runtime, expected `{}`",
                request.plant_id,
                state.plant_id.as_str()
            ),
        ));
    }

    let employee_actor = load_gate_employee_actor_for_plant(state, &state.plant_id)?;
    state
        .rush_reminder_workflow
        .upsert_preferences(
            employee_actor.actor_id().clone(),
            RushReminderPreferences::new(
                request.preorder_open_enabled,
                request.demand_spike_enabled,
            ),
        )
        .map_err(|error| {
            domain_error(
                StatusCode::INTERNAL_SERVER_ERROR,
                "INTERNAL_SERVER_ERROR",
                format!("failed to persist rush reminder preferences: {error}"),
            )
        })?;

    let saved = state
        .rush_reminder_workflow
        .preferences_for(employee_actor.actor_id())
        .map_err(|error| {
            domain_error(
                StatusCode::INTERNAL_SERVER_ERROR,
                "INTERNAL_SERVER_ERROR",
                format!("failed to load persisted rush reminder preferences: {error}"),
            )
        })?;

    Ok(EmployeeRushReminderPreferencesPayload {
        employee_actor_id: employee_actor.actor_id().as_str().to_owned(),
        plant_id: state.plant_id.as_str().to_owned(),
        preorder_open_enabled: saved.preorder_open_enabled(),
        demand_spike_enabled: saved.demand_spike_enabled(),
    })
}

fn handle_list_employee_menus(
    state: &AppState,
    query: EmployeeMenuDiscoveryQuery,
) -> Result<MenuDiscoveryResponse, (StatusCode, ErrorPayload)> {
    let request_plant_id = query.plant_id.as_deref().ok_or_else(|| {
        domain_error(
            StatusCode::BAD_REQUEST,
            "INVALID_MENU_DISCOVERY_QUERY",
            "plantId query parameter is required".to_owned(),
        )
    })?;
    if request_plant_id != state.plant_id.as_str() {
        return Err(domain_error(
            StatusCode::BAD_REQUEST,
            "UNSUPPORTED_PLANT_ID",
            format!(
                "plantId `{request_plant_id}` is unsupported by this runtime, expected `{}`",
                state.plant_id.as_str()
            ),
        ));
    }

    let moment = current_taipei_business_moment().map_err(|error| {
        domain_error(
            StatusCode::INTERNAL_SERVER_ERROR,
            "TIME_RESOLUTION_FAILED",
            error,
        )
    })?;
    handle_list_employee_menus_at(state, query, moment)
}

fn handle_list_employee_menus_at(
    state: &AppState,
    query: EmployeeMenuDiscoveryQuery,
    moment: TaipeiBusinessMoment,
) -> Result<MenuDiscoveryResponse, (StatusCode, ErrorPayload)> {
    let view = query.view.unwrap_or(MenuDiscoveryViewQuery::Week);
    let (from_epoch_day, to_epoch_day) = resolve_discovery_window(view, &query, moment.epoch_day())
        .map_err(|message| {
            domain_error(
                StatusCode::BAD_REQUEST,
                "INVALID_MENU_DISCOVERY_QUERY",
                message,
            )
        })?;
    let health_tag_filter = query
        .health_tag
        .as_deref()
        .map(MenuHealthTag::parse)
        .transpose()
        .map_err(|error| {
            domain_error(
                StatusCode::BAD_REQUEST,
                "INVALID_MENU_DISCOVERY_QUERY",
                format!("healthTag is invalid: {error}"),
            )
        })?;
    let menu_type_filter = query
        .menu_type
        .as_deref()
        .map(normalize_menu_type_filter)
        .transpose()
        .map_err(|message| {
            domain_error(
                StatusCode::BAD_REQUEST,
                "INVALID_MENU_DISCOVERY_QUERY",
                message,
            )
        })?;
    if let (Some(price_min_minor), Some(price_max_minor)) =
        (query.price_min_minor, query.price_max_minor)
    {
        if price_min_minor > price_max_minor {
            return Err(domain_error(
                StatusCode::BAD_REQUEST,
                "INVALID_MENU_DISCOVERY_QUERY",
                "priceMinMinor must be less than or equal to priceMaxMinor".to_owned(),
            ));
        }
    }

    let page = query.page.unwrap_or(1);
    if page == 0 {
        return Err(domain_error(
            StatusCode::BAD_REQUEST,
            "INVALID_MENU_DISCOVERY_QUERY",
            "page must be greater than or equal to 1".to_owned(),
        ));
    }
    let page_size = query.page_size.unwrap_or(20);
    if page_size == 0 || page_size > 200 {
        return Err(domain_error(
            StatusCode::BAD_REQUEST,
            "INVALID_MENU_DISCOVERY_QUERY",
            "pageSize must be between 1 and 200".to_owned(),
        ));
    }
    let recommendation_requested = state.recommendation_engine_runtime_enabled;

    let compliance_lifecycle = load_compliance_lifecycle_snapshot(state)?;
    let delivery_policy = with_delivery_policy(state, |policy| Ok(policy.clone()))?;
    let menu_supply_policy = with_menu_supply_policy(state, |policy| Ok(policy.clone()))?;
    let discovery_gateway = HttpEmployeeDiscoveryExecutionGateway::new(
        &compliance_lifecycle,
        &delivery_policy,
        &menu_supply_policy,
    );
    let for_search = query_has_search_filters(&query);
    let mut entries = discovery_gateway
        .execute_discovery_snapshot(&state.plant_id, moment, for_search)
        .map_err(|error| {
            domain_error(
                StatusCode::INTERNAL_SERVER_ERROR,
                "MENU_DISCOVERY_INTERNAL_ERROR",
                error.to_string(),
            )
        })?;

    let search_query = query.search.map(|value| value.trim().to_ascii_lowercase());
    entries.retain(|entry| {
        if !entry.preorder_open() {
            return false;
        }
        if entry.menu_item().delivery_epoch_day() < from_epoch_day
            || entry.menu_item().delivery_epoch_day() > to_epoch_day
        {
            return false;
        }
        if let Some(search_query) = search_query.as_deref() {
            let name = entry.menu_item().name().to_ascii_lowercase();
            let description = entry.menu_item().description().to_ascii_lowercase();
            if !name.contains(search_query) && !description.contains(search_query) {
                return false;
            }
        }
        if let Some(menu_type_filter) = menu_type_filter.as_deref() {
            if entry.menu_item().menu_type() != menu_type_filter {
                return false;
            }
        }
        if let Some(health_tag_filter) = health_tag_filter {
            if !entry.menu_item().health_tags().contains(&health_tag_filter) {
                return false;
            }
        }
        if let Some(price_min_minor) = query.price_min_minor {
            if entry.menu_item().price().amount_minor() < price_min_minor {
                return false;
            }
        }
        if let Some(price_max_minor) = query.price_max_minor {
            if entry.menu_item().price().amount_minor() > price_max_minor {
                return false;
            }
        }
        if let Some(remaining_quantity) = query.remaining_quantity {
            if entry.remaining_quantity() != remaining_quantity {
                return false;
            }
        }
        true
    });

    let sort_by = query.sort_by.unwrap_or(MenuSortFieldQuery::DeliveryDate);
    let sort_order = query.sort_order.unwrap_or(SortOrderQuery::Asc);
    entries.sort_by(|left, right| compare_menu_discovery_entry(left, right, sort_by, sort_order));
    let recommendation_applied = maybe_apply_menu_recommendation(
        state,
        recommendation_requested,
        &mut entries,
        moment,
        sort_by,
        sort_order,
    );
    let reminder_subscribers = reminder_subscriber_actor_ids_for_load_gate_employee(state);
    schedule_and_dispatch_rush_reminders_best_effort(
        state,
        &reminder_subscribers,
        &entries,
        moment,
        "listEmployeeMenus",
    );

    let total_items = entries.len();
    let total_pages = if total_items == 0 {
        0
    } else {
        (total_items - 1) / page_size + 1
    };
    let start = page.saturating_sub(1).saturating_mul(page_size);
    let end = start.saturating_add(page_size).min(total_items);
    let paged_entries = if start >= total_items {
        Vec::new()
    } else {
        entries[start..end].to_vec()
    };
    let items = paged_entries
        .iter()
        .map(to_menu_discovery_item_payload)
        .collect::<Vec<_>>();

    let mut items_by_delivery_date = BTreeMap::<i32, Vec<MenuDiscoveryItem>>::new();
    for day in from_epoch_day..=to_epoch_day {
        items_by_delivery_date.insert(day, Vec::new());
    }
    for item in &items {
        if let Some(items_for_day) = items_by_delivery_date.get_mut(
            &parse_iso_date_to_epoch_day(&item.delivery_date)
                .expect("response item deliveryDate should always parse"),
        ) {
            items_for_day.push(item.clone());
        }
    }
    let days = (from_epoch_day..=to_epoch_day)
        .map(|epoch_day| MenuDiscoveryDay {
            delivery_date: epoch_day_to_iso_date(epoch_day),
            items: items_by_delivery_date
                .remove(&epoch_day)
                .unwrap_or_default(),
        })
        .collect::<Vec<_>>();

    Ok(MenuDiscoveryResponse {
        timezone: "Asia/Taipei",
        view: view.as_str(),
        recommendation_requested,
        recommendation_applied,
        from_date: epoch_day_to_iso_date(from_epoch_day),
        to_date: epoch_day_to_iso_date(to_epoch_day),
        days,
        items,
        page: MenuDiscoveryPageMeta {
            page,
            page_size,
            total_items,
            total_pages,
        },
    })
}

fn query_has_search_filters(query: &EmployeeMenuDiscoveryQuery) -> bool {
    query.search.is_some()
        || query.menu_type.is_some()
        || query.health_tag.is_some()
        || query.price_min_minor.is_some()
        || query.price_max_minor.is_some()
        || query.remaining_quantity.is_some()
}

const DEFAULT_ORDER_LIST_LOOKBACK_DAYS: i32 = 30;
const DEFAULT_ORDER_LIST_LOOKAHEAD_DAYS: i32 = 30;
const MAX_ORDER_LIST_RANGE_DAYS: i32 = 366;

fn resolve_order_query_range(
    from_date: Option<&str>,
    to_date: Option<&str>,
    now_epoch_day: i32,
) -> Result<(i32, i32), (StatusCode, ErrorPayload)> {
    let parsed_from = from_date
        .map(parse_iso_date_to_epoch_day)
        .transpose()
        .map_err(|error| {
            domain_error(
                StatusCode::BAD_REQUEST,
                "BAD_REQUEST",
                format!("fromDate is invalid: {error}"),
            )
        })?;
    let parsed_to = to_date
        .map(parse_iso_date_to_epoch_day)
        .transpose()
        .map_err(|error| {
            domain_error(
                StatusCode::BAD_REQUEST,
                "BAD_REQUEST",
                format!("toDate is invalid: {error}"),
            )
        })?;

    let from_epoch_day = parsed_from
        .unwrap_or_else(|| now_epoch_day.saturating_sub(DEFAULT_ORDER_LIST_LOOKBACK_DAYS));
    let to_epoch_day = parsed_to
        .unwrap_or_else(|| now_epoch_day.saturating_add(DEFAULT_ORDER_LIST_LOOKAHEAD_DAYS));

    if to_epoch_day < from_epoch_day {
        return Err(domain_error(
            StatusCode::BAD_REQUEST,
            "BAD_REQUEST",
            "toDate must be greater than or equal to fromDate".to_owned(),
        ));
    }
    let range_days = to_epoch_day
        .saturating_sub(from_epoch_day)
        .saturating_add(1);
    if range_days > MAX_ORDER_LIST_RANGE_DAYS {
        return Err(domain_error(
            StatusCode::BAD_REQUEST,
            "BAD_REQUEST",
            format!(
                "order query range exceeds maximum supported span of {MAX_ORDER_LIST_RANGE_DAYS} days"
            ),
        ));
    }
    Ok((from_epoch_day, to_epoch_day))
}

fn parse_order_status_filter(
    value: &str,
) -> Result<OrderLifecycleState, (StatusCode, ErrorPayload)> {
    match value.trim().to_ascii_uppercase().as_str() {
        "PENDING" => Ok(OrderLifecycleState::Pending),
        "MODIFIED" => Ok(OrderLifecycleState::Modified),
        "CANCELLED" => Ok(OrderLifecycleState::Cancelled),
        "SOLD_OUT" => Ok(OrderLifecycleState::SoldOut),
        "REFUND_PENDING" => Ok(OrderLifecycleState::RefundPending),
        "REFUNDED" => Ok(OrderLifecycleState::Refunded),
        "FULFILLED" => Ok(OrderLifecycleState::Fulfilled),
        _ => Err(domain_error(
            StatusCode::BAD_REQUEST,
            "BAD_REQUEST",
            format!("status `{}` is unsupported", value.trim()),
        )),
    }
}

fn order_created_at(snapshot: &OrderSnapshot) -> Option<TaipeiBusinessMoment> {
    snapshot.timeline().first().map(|event| event.occurred_at())
}

fn compare_employee_order_snapshot(
    left: &OrderSnapshot,
    right: &OrderSnapshot,
    sort_by: EmployeeOrderSortFieldQuery,
    sort_order: SortOrderQuery,
) -> CmpOrdering {
    let ordering = match sort_by {
        EmployeeOrderSortFieldQuery::DeliveryDate => {
            left.delivery_epoch_day().cmp(&right.delivery_epoch_day())
        }
        EmployeeOrderSortFieldQuery::Status => left.state().as_str().cmp(right.state().as_str()),
        EmployeeOrderSortFieldQuery::CreatedAt => {
            order_created_at(left).cmp(&order_created_at(right))
        }
    }
    .then_with(|| left.delivery_epoch_day().cmp(&right.delivery_epoch_day()))
    .then_with(|| order_created_at(left).cmp(&order_created_at(right)))
    .then_with(|| left.order_id().cmp(right.order_id()));
    match sort_order {
        SortOrderQuery::Asc => ordering,
        SortOrderQuery::Desc => ordering.reverse(),
    }
}

fn compare_vendor_order_snapshot(
    left: &OrderSnapshot,
    right: &OrderSnapshot,
    sort_by: VendorOrderSortFieldQuery,
    sort_order: SortOrderQuery,
) -> CmpOrdering {
    let ordering = match sort_by {
        VendorOrderSortFieldQuery::DeliveryDate => {
            left.delivery_epoch_day().cmp(&right.delivery_epoch_day())
        }
        VendorOrderSortFieldQuery::PlantId => left.plant_id().cmp(right.plant_id()),
        VendorOrderSortFieldQuery::Status => left.state().as_str().cmp(right.state().as_str()),
        VendorOrderSortFieldQuery::CreatedAt => {
            order_created_at(left).cmp(&order_created_at(right))
        }
    }
    .then_with(|| left.delivery_epoch_day().cmp(&right.delivery_epoch_day()))
    .then_with(|| left.plant_id().cmp(right.plant_id()))
    .then_with(|| order_created_at(left).cmp(&order_created_at(right)))
    .then_with(|| left.order_id().cmp(right.order_id()));
    match sort_order {
        SortOrderQuery::Asc => ordering,
        SortOrderQuery::Desc => ordering.reverse(),
    }
}

fn resolve_discovery_window(
    view: MenuDiscoveryViewQuery,
    query: &EmployeeMenuDiscoveryQuery,
    now_epoch_day: i32,
) -> Result<(i32, i32), String> {
    let menu_date = query
        .menu_date
        .as_deref()
        .map(parse_iso_date_to_epoch_day)
        .transpose()?;
    let from_date = query
        .from_date
        .as_deref()
        .map(parse_iso_date_to_epoch_day)
        .transpose()?;
    let to_date = query
        .to_date
        .as_deref()
        .map(parse_iso_date_to_epoch_day)
        .transpose()?;

    let (from_epoch_day, to_epoch_day) = match view {
        MenuDiscoveryViewQuery::Week => {
            if to_date.is_some() {
                return Err("toDate is not allowed when view=week".to_owned());
            }
            let from_epoch_day = from_date.or(menu_date).unwrap_or(now_epoch_day);
            (from_epoch_day, from_epoch_day.saturating_add(6))
        }
        MenuDiscoveryViewQuery::Calendar => {
            let from_epoch_day = from_date.or(menu_date).unwrap_or(now_epoch_day);
            let to_epoch_day = to_date.unwrap_or(from_epoch_day.saturating_add(13));
            (from_epoch_day, to_epoch_day)
        }
    };

    if to_epoch_day < from_epoch_day {
        return Err("toDate must be greater than or equal to fromDate".to_owned());
    }
    if to_epoch_day.saturating_sub(from_epoch_day) > 30 {
        return Err("discovery date range must be at most 31 days".to_owned());
    }
    Ok((from_epoch_day, to_epoch_day))
}

fn compare_menu_discovery_entry(
    left: &EmployeeMenuDiscoveryEntry,
    right: &EmployeeMenuDiscoveryEntry,
    sort_by: MenuSortFieldQuery,
    sort_order: SortOrderQuery,
) -> CmpOrdering {
    let ordering = match sort_by {
        MenuSortFieldQuery::Name => left.menu_item().name().cmp(right.menu_item().name()),
        MenuSortFieldQuery::PriceMinor => left
            .menu_item()
            .price()
            .amount_minor()
            .cmp(&right.menu_item().price().amount_minor()),
        MenuSortFieldQuery::RemainingQuantity => {
            left.remaining_quantity().cmp(&right.remaining_quantity())
        }
        MenuSortFieldQuery::DeliveryDate => left
            .menu_item()
            .delivery_epoch_day()
            .cmp(&right.menu_item().delivery_epoch_day()),
    }
    .then_with(|| {
        left.menu_item()
            .delivery_epoch_day()
            .cmp(&right.menu_item().delivery_epoch_day())
    })
    .then_with(|| left.menu_item().name().cmp(right.menu_item().name()))
    .then_with(|| {
        left.menu_item()
            .vendor_id()
            .cmp(right.menu_item().vendor_id())
    })
    .then_with(|| {
        left.menu_item()
            .menu_item_id()
            .cmp(right.menu_item().menu_item_id())
    });
    match sort_order {
        SortOrderQuery::Asc => ordering,
        SortOrderQuery::Desc => ordering.reverse(),
    }
}

fn maybe_apply_menu_recommendation(
    state: &AppState,
    recommendation_requested: bool,
    entries: &mut Vec<EmployeeMenuDiscoveryEntry>,
    moment: TaipeiBusinessMoment,
    sort_by: MenuSortFieldQuery,
    sort_order: SortOrderQuery,
) -> bool {
    if !recommendation_requested || !state.recommendation_engine_runtime_enabled {
        return false;
    }

    match (state.menu_recommendation_ranker)(entries, moment, sort_by, sort_order) {
        Ok(()) => true,
        Err(error) => {
            tracing::warn!(
                recommendation_error = %error,
                "menu recommendation ranking failed; continuing with deterministic ordering"
            );
            false
        }
    }
}

fn reminder_subscriber_actor_ids_for_load_gate_employee(state: &AppState) -> HashSet<ActorId> {
    let actor_id = ActorId::parse(LOAD_GATE_EMPLOYEE_ACTOR_ID)
        .expect("load-gate employee actor id constant must remain valid");
    if state.terminated_employee_actor_ids.contains(&actor_id) {
        return HashSet::new();
    }
    HashSet::from([actor_id])
}

fn schedule_and_dispatch_rush_reminders_best_effort(
    state: &AppState,
    subscriber_actor_ids: &HashSet<ActorId>,
    entries: &[EmployeeMenuDiscoveryEntry],
    at: TaipeiBusinessMoment,
    operation_id: &str,
) {
    let schedule_report = match state.rush_reminder_workflow.schedule_from_discovery(
        state.rush_reminder_runtime_enabled,
        subscriber_actor_ids,
        entries,
        at,
    ) {
        Ok(report) => report,
        Err(error) => {
            tracing::warn!(
                operation_id,
                reminder_error = %error,
                "rush reminder scheduling failed; continuing without notification side effects"
            );
            return;
        }
    };

    let dispatch_report = match state.rush_reminder_workflow.dispatch_pending(
        state.rush_reminder_runtime_enabled,
        state.rush_reminder_delivery_gateway.as_ref(),
        at,
    ) {
        Ok(report) => report,
        Err(error) => {
            tracing::warn!(
                operation_id,
                reminder_error = %error,
                "rush reminder dispatch failed; continuing without notification side effects"
            );
            return;
        }
    };

    if dispatch_report.failed_count > 0 {
        tracing::warn!(
            operation_id,
            scheduled_count = schedule_report.scheduled_count,
            throttled_count = schedule_report.throttled_count,
            opted_out_count = schedule_report.opted_out_count,
            delivery_failures = dispatch_report.failed_count,
            "rush reminder delivery failures were isolated from transaction flow"
        );
    }
}

fn heuristic_menu_recommendation_ranker(
    entries: &mut Vec<EmployeeMenuDiscoveryEntry>,
    moment: TaipeiBusinessMoment,
    sort_by: MenuSortFieldQuery,
    sort_order: SortOrderQuery,
) -> Result<(), String> {
    if entries.len() <= 1 {
        return Ok(());
    }

    let mut scored_entries = entries
        .iter()
        .cloned()
        .map(|entry| {
            let score = menu_recommendation_score(&entry, moment)?;
            Ok((entry, score))
        })
        .collect::<Result<Vec<_>, String>>()?;

    scored_entries.sort_by(|(left_entry, left_score), (right_entry, right_score)| {
        right_score.cmp(left_score).then_with(|| {
            compare_menu_discovery_entry(left_entry, right_entry, sort_by, sort_order)
        })
    });

    entries.clear();
    entries.extend(scored_entries.into_iter().map(|(entry, _)| entry));
    Ok(())
}

fn menu_recommendation_score(
    entry: &EmployeeMenuDiscoveryEntry,
    moment: TaipeiBusinessMoment,
) -> Result<i64, String> {
    let delivery_epoch_day = entry.menu_item().delivery_epoch_day();
    let lead_days = delivery_epoch_day.saturating_sub(moment.epoch_day());
    if lead_days < 0 {
        return Err(format!(
            "menu item `{}` delivery day {} is before request day {}",
            entry.menu_item().menu_item_id().as_str(),
            delivery_epoch_day,
            moment.epoch_day()
        ));
    }
    let recency_score = i64::from(31_i32.saturating_sub(lead_days))
        .checked_mul(1_000)
        .ok_or_else(|| "recency score overflowed".to_owned())?;
    let inventory_score = i64::from(entry.remaining_quantity())
        .checked_mul(100)
        .ok_or_else(|| "inventory score overflowed".to_owned())?;
    recency_score
        .checked_add(inventory_score)
        .ok_or_else(|| "recommendation score overflowed".to_owned())
}

fn to_menu_discovery_item_payload(entry: &EmployeeMenuDiscoveryEntry) -> MenuDiscoveryItem {
    let menu_item = entry.menu_item();
    MenuDiscoveryItem {
        menu_item_id: menu_item.menu_item_id().as_str().to_owned(),
        vendor_id: menu_item.vendor_id().as_str().to_owned(),
        name: menu_item.name().to_owned(),
        description: menu_item.description().to_owned(),
        image_url: menu_item.image_url().map(|value| value.as_str().to_owned()),
        menu_type: menu_item.menu_type().to_owned(),
        health_tags: menu_item
            .health_tags()
            .iter()
            .map(|tag| tag.as_str().to_owned())
            .collect(),
        price: MenuPricePayload {
            currency: menu_item.price().currency().to_owned(),
            amount_minor: menu_item.price().amount_minor(),
        },
        remaining_quantity: entry.remaining_quantity(),
        preorder_open: entry.preorder_open(),
        preorder_open_days_ahead: entry.preorder_open_days_ahead(),
        modify_cancel_cutoff_minute_of_day: entry.modify_cancel_cutoff_minute_of_day(),
        delivery_date: epoch_day_to_iso_date(menu_item.delivery_epoch_day()),
        earliest_delivery_date: epoch_day_to_iso_date(entry.earliest_delivery_epoch_day()),
        latest_delivery_date: epoch_day_to_iso_date(entry.latest_delivery_epoch_day()),
        cutoff_date: epoch_day_to_iso_date(entry.cutoff_epoch_day()),
    }
}

fn normalize_menu_type_filter(value: &str) -> Result<String, String> {
    let normalized = value.trim().to_ascii_uppercase();
    if normalized.is_empty() {
        return Err("menuType must be non-empty when provided".to_owned());
    }
    if normalized.len() > 32 {
        return Err("menuType must be at most 32 characters".to_owned());
    }
    if !normalized
        .chars()
        .all(|ch| ch.is_ascii_uppercase() || ch.is_ascii_digit() || ch == '_')
    {
        return Err("menuType must be uppercase snake case".to_owned());
    }
    Ok(normalized)
}

fn parse_iso_date_to_epoch_day(value: &str) -> Result<i32, String> {
    let trimmed = value.trim();
    let mut parts = trimmed.split('-');
    let year = parts
        .next()
        .ok_or_else(|| "date must use YYYY-MM-DD format".to_owned())?
        .parse::<i32>()
        .map_err(|_| "date year is invalid".to_owned())?;
    let month = parts
        .next()
        .ok_or_else(|| "date must use YYYY-MM-DD format".to_owned())?
        .parse::<u32>()
        .map_err(|_| "date month is invalid".to_owned())?;
    let day = parts
        .next()
        .ok_or_else(|| "date must use YYYY-MM-DD format".to_owned())?
        .parse::<u32>()
        .map_err(|_| "date day is invalid".to_owned())?;
    if parts.next().is_some() {
        return Err("date must use YYYY-MM-DD format".to_owned());
    }
    if !(1..=12).contains(&month) {
        return Err("date month must be between 1 and 12".to_owned());
    }
    let max_day = days_in_month(year, month);
    if day == 0 || day > max_day {
        return Err(format!(
            "date day must be between 1 and {max_day} for month {month:02}"
        ));
    }

    i32::try_from(days_from_civil(year, month, day))
        .map_err(|_| "date is out of supported epoch-day range".to_owned())
}

fn epoch_day_to_iso_date(epoch_day: i32) -> String {
    let (year, month, day) = civil_from_days(i64::from(epoch_day));
    format!("{year:04}-{month:02}-{day:02}")
}

fn parse_contract_order_id(value: &str) -> Result<OrderId, String> {
    let trimmed = value.trim();
    let Some(suffix) = trimmed.strip_prefix("ord-") else {
        return Err("must start with `ord-`".to_owned());
    };
    if !(8..=32).contains(&suffix.len()) {
        return Err("suffix length must be between 8 and 32 characters".to_owned());
    }
    if !suffix
        .chars()
        .all(|ch| ch.is_ascii_lowercase() || ch.is_ascii_digit())
    {
        return Err("suffix must contain only lowercase letters and digits".to_owned());
    }
    OrderId::parse(trimmed.to_owned()).map_err(|error| error.to_string())
}

fn days_in_month(year: i32, month: u32) -> u32 {
    match month {
        1 | 3 | 5 | 7 | 8 | 10 | 12 => 31,
        4 | 6 | 9 | 11 => 30,
        2 if is_leap_year(year) => 29,
        2 => 28,
        _ => 0,
    }
}

fn is_leap_year(year: i32) -> bool {
    (year % 4 == 0 && year % 100 != 0) || (year % 400 == 0)
}

fn days_from_civil(year: i32, month: u32, day: u32) -> i64 {
    let year = i64::from(year) - if month <= 2 { 1 } else { 0 };
    let era = if year >= 0 { year } else { year - 399 } / 400;
    let year_of_era = year - era * 400;
    let month = i64::from(month);
    let day = i64::from(day);
    let day_of_year = (153 * (month + if month > 2 { -3 } else { 9 }) + 2) / 5 + day - 1;
    let day_of_era = year_of_era * 365 + year_of_era / 4 - year_of_era / 100 + day_of_year;
    era * 146_097 + day_of_era - 719_468
}

fn civil_from_days(days_since_epoch: i64) -> (i32, u32, u32) {
    let shifted_days = days_since_epoch + 719_468;
    let era = if shifted_days >= 0 {
        shifted_days
    } else {
        shifted_days - 146_096
    } / 146_097;
    let day_of_era = shifted_days - era * 146_097;
    let year_of_era =
        (day_of_era - day_of_era / 1_460 + day_of_era / 36_524 - day_of_era / 146_096) / 365;
    let year = year_of_era + era * 400;
    let day_of_year = day_of_era - (365 * year_of_era + year_of_era / 4 - year_of_era / 100);
    let month_piece = (5 * day_of_year + 2) / 153;
    let day = day_of_year - (153 * month_piece + 2) / 5 + 1;
    let month = month_piece + if month_piece < 10 { 3 } else { -9 };
    let year = year + if month <= 2 { 1 } else { 0 };

    (
        i32::try_from(year).expect("civil year should fit in i32"),
        u32::try_from(month).expect("civil month should fit in u32"),
        u32::try_from(day).expect("civil day should fit in u32"),
    )
}

async fn create_employee_order(
    State(state): State<AppState>,
    Json(request): Json<EmployeeOrderCreateRequestPayload>,
) -> (StatusCode, Json<serde_json::Value>) {
    let telemetry = TelemetryService::HttpApi.begin_operation(
        "createEmployeeOrder",
        Some("load-gate"),
        Some(state.plant_id.as_str()),
    );
    let request_id = telemetry.correlation_context().request_id().to_owned();

    let response = match handle_create_employee_order(&state, request) {
        Ok(response) => {
            telemetry.finish_with_http_status(StatusCode::CREATED.as_u16());
            (
                StatusCode::CREATED,
                Json(
                    serde_json::to_value(response)
                        .expect("create order payload serialization should succeed"),
                ),
            )
        }
        Err((status, error)) => {
            telemetry.finish_with_http_status(status.as_u16());
            (
                status,
                Json(
                    serde_json::to_value(error.with_request_id(request_id.as_str()))
                        .expect("error payload serialization should succeed"),
                ),
            )
        }
    };

    response
}

fn handle_create_employee_order(
    state: &AppState,
    request: EmployeeOrderCreateRequestPayload,
) -> Result<EmployeeOrderPayload, (StatusCode, ErrorPayload)> {
    let employee_actor = load_gate_employee_actor_for_plant(state, &state.plant_id)?;
    handle_create_employee_order_for_actor(state, &employee_actor, "createEmployeeOrder", request)
}

fn handle_create_employee_order_for_actor(
    state: &AppState,
    employee_actor: &AuthenticatedActorContext,
    operation_id: &str,
    request: EmployeeOrderCreateRequestPayload,
) -> Result<EmployeeOrderPayload, (StatusCode, ErrorPayload)> {
    if let Some(employee_note) = request.employee_note.as_deref() {
        if employee_note.chars().count() > 200 {
            return Err(domain_error(
                StatusCode::BAD_REQUEST,
                "INVALID_ORDER_REQUEST",
                "employeeNote must be at most 200 characters".to_owned(),
            ));
        }
    }

    if request.plant_id.as_str() != state.plant_id.as_str() {
        return Err(domain_error(
            StatusCode::BAD_REQUEST,
            "UNSUPPORTED_PLANT_ID",
            format!(
                "plantId `{}` is unsupported by this runtime, expected `{}`",
                request.plant_id,
                state.plant_id.as_str()
            ),
        ));
    }

    let delivery_epoch_day =
        parse_iso_date_to_epoch_day(&request.delivery_date).map_err(|error| {
            domain_error(
                StatusCode::BAD_REQUEST,
                "INVALID_ORDER_REQUEST",
                format!("deliveryDate is invalid: {error}"),
            )
        })?;

    let line_items = parse_domain_line_items(request.line_items)?;
    let request_vendor_id = resolve_vendor_for_line_items(state, &line_items)?;
    let requested_at = current_taipei_business_moment().map_err(|error| {
        domain_error(
            StatusCode::INTERNAL_SERVER_ERROR,
            "TIME_RESOLUTION_FAILED",
            error,
        )
    })?;

    let order_id = generate_contract_order_id(state)?;
    let compliance_lifecycle = load_compliance_lifecycle_snapshot(state)?;
    let delivery_policy = with_delivery_policy(state, |policy| Ok(policy.clone()))?;
    let snapshot = mutate_ordering_menu_supply_policy(state, |menu_supply_policy| {
        let ordering_gateway = HttpOrderingExecutionGateway::new(
            &compliance_lifecycle,
            &delivery_policy,
            menu_supply_policy,
        );
        ordering_gateway.execute_create_employee_order(
            &employee_actor,
            order_id.clone(),
            &request_vendor_id,
            &state.plant_id,
            delivery_epoch_day,
            line_items,
            requested_at,
        )?;
        menu_supply_policy
            .order_snapshot(&order_id)
            .map_err(HttpOrderExecutionError::MenuSupply)?
            .ok_or_else(|| {
                HttpOrderExecutionError::MenuSupply(MenuSupplyWindowError::OrderNotFound(
                    order_id.clone(),
                ))
            })
            .map(|snapshot| {
                let outbox_events = build_order_state_changed_outbox_records(
                    state,
                    employee_actor,
                    operation_id,
                    &snapshot,
                    requested_at,
                );
                (snapshot, outbox_events)
            })
    })?;
    sync_payroll_ledger_from_order_snapshot(
        state,
        employee_actor,
        operation_id,
        &snapshot,
        requested_at,
    )?;
    build_employee_order_payload(state, &snapshot)
}

async fn update_employee_order(
    State(state): State<AppState>,
    Path(order_id): Path<String>,
    Json(request): Json<UpdateOrderRequest>,
) -> (StatusCode, Json<serde_json::Value>) {
    let telemetry = TelemetryService::HttpApi.begin_operation(
        "updateEmployeeOrder",
        Some("load-gate"),
        Some(state.plant_id.as_str()),
    );
    let request_id = telemetry.correlation_context().request_id().to_owned();

    let response = match handle_update_employee_order(&state, order_id, request) {
        Ok(response) => {
            telemetry.finish_with_http_status(StatusCode::OK.as_u16());
            (
                StatusCode::OK,
                Json(
                    serde_json::to_value(response)
                        .expect("update order payload serialization should succeed"),
                ),
            )
        }
        Err((status, error)) => {
            telemetry.finish_with_http_status(status.as_u16());
            (
                status,
                Json(
                    serde_json::to_value(error.with_request_id(request_id.as_str()))
                        .expect("error payload serialization should succeed"),
                ),
            )
        }
    };

    response
}

fn handle_update_employee_order(
    state: &AppState,
    order_id_raw: String,
    request: UpdateOrderRequest,
) -> Result<EmployeeOrderPayload, (StatusCode, ErrorPayload)> {
    let order_id = parse_contract_order_id(&order_id_raw).map_err(|error| {
        domain_error(
            StatusCode::BAD_REQUEST,
            "INVALID_ORDER_UPDATE_REQUEST",
            format!("orderId path parameter is invalid: {error}"),
        )
    })?;
    let current_snapshot = load_order_snapshot_or_not_found(state, &order_id)?;
    let employee_actor = load_gate_employee_actor_for_plant(state, current_snapshot.plant_id())?;
    handle_update_employee_order_for_actor(
        state,
        &employee_actor,
        "updateEmployeeOrder",
        order_id_raw,
        request,
    )
}

fn handle_update_employee_order_for_actor(
    state: &AppState,
    actor: &AuthenticatedActorContext,
    operation_id: &str,
    order_id_raw: String,
    request: UpdateOrderRequest,
) -> Result<EmployeeOrderPayload, (StatusCode, ErrorPayload)> {
    let order_id = parse_contract_order_id(&order_id_raw).map_err(|error| {
        domain_error(
            StatusCode::BAD_REQUEST,
            "INVALID_ORDER_UPDATE_REQUEST",
            format!("orderId path parameter is invalid: {error}"),
        )
    })?;
    let mutation = parse_order_mutation(request)?;
    let current_snapshot = load_order_snapshot_or_not_found(state, &order_id)?;
    let requested_at = current_taipei_business_moment().map_err(|error| {
        domain_error(
            StatusCode::INTERNAL_SERVER_ERROR,
            "TIME_RESOLUTION_FAILED",
            error,
        )
    })?;

    let compliance_lifecycle = load_compliance_lifecycle_snapshot(state)?;
    let delivery_policy = with_delivery_policy(state, |policy| Ok(policy.clone()))?;
    let current_vendor_id = current_snapshot.vendor_id().clone();
    let updated_snapshot = mutate_ordering_menu_supply_policy(state, |menu_supply_policy| {
        let ordering_gateway = HttpOrderingExecutionGateway::new(
            &compliance_lifecycle,
            &delivery_policy,
            menu_supply_policy,
        );
        ordering_gateway.execute_update_employee_order(
            actor,
            &order_id,
            &current_vendor_id,
            &state.plant_id,
            mutation,
            requested_at,
        )?;
        menu_supply_policy
            .order_snapshot(&order_id)
            .map_err(HttpOrderExecutionError::MenuSupply)?
            .ok_or_else(|| {
                HttpOrderExecutionError::MenuSupply(MenuSupplyWindowError::OrderNotFound(
                    order_id.clone(),
                ))
            })
            .map(|snapshot| {
                let outbox_events = build_order_state_changed_outbox_records(
                    state,
                    actor,
                    operation_id,
                    &snapshot,
                    requested_at,
                );
                (snapshot, outbox_events)
            })
    })?;
    sync_payroll_ledger_from_order_snapshot(
        state,
        actor,
        operation_id,
        &updated_snapshot,
        requested_at,
    )?;
    build_employee_order_payload(state, &updated_snapshot)
}

async fn get_employee_order_payroll_ledger(
    State(state): State<AppState>,
    Path(order_id): Path<String>,
) -> (StatusCode, Json<serde_json::Value>) {
    let telemetry = TelemetryService::HttpApi.begin_operation(
        "getEmployeeOrderPayrollLedger",
        Some("load-gate"),
        Some(state.plant_id.as_str()),
    );
    let request_id = telemetry.correlation_context().request_id().to_owned();

    let response = match handle_get_employee_order_payroll_ledger(&state, order_id) {
        Ok(payload) => {
            telemetry.finish_with_http_status(StatusCode::OK.as_u16());
            (
                StatusCode::OK,
                Json(
                    serde_json::to_value(payload)
                        .expect("payroll ledger payload serialization should succeed"),
                ),
            )
        }
        Err((status, error)) => {
            telemetry.finish_with_http_status(status.as_u16());
            (
                status,
                Json(
                    serde_json::to_value(error.with_request_id(request_id.as_str()))
                        .expect("payroll ledger error payload serialization should succeed"),
                ),
            )
        }
    };

    response
}

fn handle_get_employee_order_payroll_ledger(
    state: &AppState,
    order_id_raw: String,
) -> Result<EmployeeOrderPayrollLedgerResponse, (StatusCode, ErrorPayload)> {
    let order_id = parse_contract_order_id(&order_id_raw).map_err(|error| {
        domain_error(
            StatusCode::BAD_REQUEST,
            "INVALID_ORDER_REQUEST",
            format!("orderId path parameter is invalid: {error}"),
        )
    })?;
    let snapshot = load_order_snapshot_or_not_found(state, &order_id)?;
    let employee_actor = load_gate_employee_actor_for_plant(state, snapshot.plant_id())?;
    handle_get_employee_order_payroll_ledger_for_actor(state, &employee_actor, order_id_raw)
}

fn handle_get_employee_order_payroll_ledger_for_actor(
    state: &AppState,
    actor: &AuthenticatedActorContext,
    order_id_raw: String,
) -> Result<EmployeeOrderPayrollLedgerResponse, (StatusCode, ErrorPayload)> {
    let order_id = parse_contract_order_id(&order_id_raw).map_err(|error| {
        domain_error(
            StatusCode::BAD_REQUEST,
            "INVALID_ORDER_REQUEST",
            format!("orderId path parameter is invalid: {error}"),
        )
    })?;
    let view = with_payroll_ledger_service(state, |service| {
        service.employee_order_view(actor, &order_id)
    })?;

    Ok(to_employee_order_payroll_ledger_response(&view))
}

async fn create_employee_order_dispute(
    State(state): State<AppState>,
    Path(order_id): Path<String>,
    Json(request): Json<EmployeePayrollDisputeCreateRequest>,
) -> (StatusCode, Json<serde_json::Value>) {
    let telemetry = TelemetryService::HttpApi.begin_operation(
        "createEmployeeOrderDispute",
        Some("load-gate"),
        Some(state.plant_id.as_str()),
    );
    let request_id = telemetry.correlation_context().request_id().to_owned();

    let response = match handle_create_employee_order_dispute(&state, order_id, request) {
        Ok(payload) => {
            telemetry.finish_with_http_status(StatusCode::CREATED.as_u16());
            (
                StatusCode::CREATED,
                Json(
                    serde_json::to_value(payload)
                        .expect("payroll dispute payload serialization should succeed"),
                ),
            )
        }
        Err((status, error)) => {
            telemetry.finish_with_http_status(status.as_u16());
            (
                status,
                Json(
                    serde_json::to_value(error.with_request_id(request_id.as_str()))
                        .expect("payroll dispute error payload serialization should succeed"),
                ),
            )
        }
    };

    response
}

fn handle_create_employee_order_dispute(
    state: &AppState,
    order_id_raw: String,
    request: EmployeePayrollDisputeCreateRequest,
) -> Result<PayrollDisputePayload, (StatusCode, ErrorPayload)> {
    let order_id = parse_contract_order_id(&order_id_raw).map_err(|error| {
        domain_error(
            StatusCode::BAD_REQUEST,
            "INVALID_ORDER_REQUEST",
            format!("orderId path parameter is invalid: {error}"),
        )
    })?;
    let snapshot = load_order_snapshot_or_not_found(state, &order_id)?;
    let employee_actor = load_gate_employee_actor_for_plant(state, snapshot.plant_id())?;
    let requested_at = current_taipei_business_moment().map_err(|error| {
        domain_error(
            StatusCode::INTERNAL_SERVER_ERROR,
            "TIME_RESOLUTION_FAILED",
            error,
        )
    })?;
    sync_payroll_ledger_from_order_snapshot(
        state,
        &employee_actor,
        "createEmployeeOrderDispute",
        &snapshot,
        requested_at,
    )?;
    let occurred_at = AuditTimestamp::from_taipei_business_moment(
        requested_at.epoch_day(),
        requested_at.minute_of_day(),
    )
    .map_err(|error| {
        domain_error(
            StatusCode::INTERNAL_SERVER_ERROR,
            "PAYROLL_LEDGER_INTERNAL_ERROR",
            error.to_string(),
        )
    })?;
    let default_owner_actor_id = load_gate_payroll_dispute_owner_actor_id()?;
    let dispute = mutate_payroll_ledger_service(state, |service| {
        service.open_dispute(
            &employee_actor,
            &order_id,
            &default_owner_actor_id,
            request.reason,
            occurred_at,
        )
    })?;

    Ok(to_payroll_dispute_payload(&dispute))
}

async fn review_vendor_application(
    State(state): State<AppState>,
    headers: HeaderMap,
    Path(vendor_id): Path<String>,
    Json(request): Json<VendorApplicationReviewRequest>,
) -> (StatusCode, Json<serde_json::Value>) {
    let telemetry = TelemetryService::HttpApi.begin_operation(
        "reviewVendorApplication",
        None::<&str>,
        None::<&str>,
    );
    let request_id = telemetry.correlation_context().request_id().to_owned();
    let committee_actor = match require_corporate_actor_for_role(&headers, Role::CommitteeAdmin) {
        Ok(actor) => actor,
        Err((status, error)) => {
            telemetry.finish_with_http_status(status.as_u16());
            return (
                status,
                Json(
                    serde_json::to_value(error.with_request_id(request_id.as_str()))
                        .expect("authorization error payload serialization should succeed"),
                ),
            );
        }
    };

    let response =
        match handle_review_vendor_application(&state, &committee_actor, vendor_id, request) {
            Ok(payload) => {
                telemetry.finish_with_http_status(StatusCode::OK.as_u16());
                (
                    StatusCode::OK,
                    Json(
                        serde_json::to_value(payload)
                            .expect("vendor review payload serialization should succeed"),
                    ),
                )
            }
            Err((status, error)) => {
                telemetry.finish_with_http_status(status.as_u16());
                (
                    status,
                    Json(
                        serde_json::to_value(error.with_request_id(request_id.as_str()))
                            .expect("vendor review error payload serialization should succeed"),
                    ),
                )
            }
        };

    response
}

fn handle_review_vendor_application(
    state: &AppState,
    committee_actor: &AuthenticatedActorContext,
    vendor_id_raw: String,
    request: VendorApplicationReviewRequest,
) -> Result<VendorApplicationReviewResponse, (StatusCode, ErrorPayload)> {
    let vendor_id = VendorId::parse(vendor_id_raw).map_err(|error| {
        domain_error(
            StatusCode::BAD_REQUEST,
            "BAD_REQUEST",
            format!("vendorId path parameter is invalid: {error}"),
        )
    })?;
    let decision = parse_vendor_review_decision(request.decision.as_str()).ok_or_else(|| {
        domain_error(
            StatusCode::BAD_REQUEST,
            "BAD_REQUEST",
            format!("decision `{}` is unsupported", request.decision.trim()),
        )
    })?;

    let status = mutate_compliance_lifecycle(state, |lifecycle| {
        lifecycle.review_application(
            committee_actor,
            &vendor_id,
            decision,
            request.comment,
            ComplianceDate::from_epoch_day(request.decided_on_epoch_day),
        )
    })?;
    Ok(VendorApplicationReviewResponse {
        vendor_id: vendor_id.as_str().to_owned(),
        status: vendor_compliance_status_label(status).to_owned(),
    })
}

async fn run_vendor_compliance_lifecycle(
    State(state): State<AppState>,
    headers: HeaderMap,
    Json(request): Json<VendorLifecycleRunRequest>,
) -> (StatusCode, Json<serde_json::Value>) {
    let telemetry = TelemetryService::HttpApi.begin_operation(
        "runVendorComplianceLifecycle",
        None::<&str>,
        None::<&str>,
    );
    let request_id = telemetry.correlation_context().request_id().to_owned();
    let committee_actor = match require_corporate_actor_for_role(&headers, Role::CommitteeAdmin) {
        Ok(actor) => actor,
        Err((status, error)) => {
            telemetry.finish_with_http_status(status.as_u16());
            return (
                status,
                Json(
                    serde_json::to_value(error.with_request_id(request_id.as_str()))
                        .expect("authorization error payload serialization should succeed"),
                ),
            );
        }
    };

    let response = match handle_run_vendor_compliance_lifecycle(&state, &committee_actor, request) {
        Ok(payload) => {
            telemetry.finish_with_http_status(StatusCode::OK.as_u16());
            (
                StatusCode::OK,
                Json(
                    serde_json::to_value(payload)
                        .expect("vendor lifecycle payload serialization should succeed"),
                ),
            )
        }
        Err((status, error)) => {
            telemetry.finish_with_http_status(status.as_u16());
            (
                status,
                Json(
                    serde_json::to_value(error.with_request_id(request_id.as_str()))
                        .expect("vendor lifecycle error payload serialization should succeed"),
                ),
            )
        }
    };

    response
}

fn handle_run_vendor_compliance_lifecycle(
    state: &AppState,
    committee_actor: &AuthenticatedActorContext,
    request: VendorLifecycleRunRequest,
) -> Result<VendorLifecycleRunResponse, (StatusCode, ErrorPayload)> {
    let result = mutate_compliance_lifecycle(state, |lifecycle| {
        lifecycle.run_lifecycle(
            committee_actor,
            ComplianceDate::from_epoch_day(request.run_on_epoch_day),
        )
    })?;
    Ok(VendorLifecycleRunResponse {
        reminder_count: result.reminders.len(),
        suspension_count: result.suspensions.len(),
        reinstatement_count: result.reinstatements.len(),
    })
}

async fn update_admin_payroll_dispute(
    State(state): State<AppState>,
    Path(dispute_id): Path<String>,
    Json(request): Json<AdminPayrollDisputePatchRequest>,
) -> (StatusCode, Json<serde_json::Value>) {
    let telemetry = TelemetryService::HttpApi.begin_operation(
        "updateAdminPayrollDispute",
        Some("load-gate"),
        None,
    );
    let request_id = telemetry.correlation_context().request_id().to_owned();

    let response = match handle_update_admin_payroll_dispute(&state, dispute_id, request) {
        Ok(payload) => {
            telemetry.finish_with_http_status(StatusCode::OK.as_u16());
            (
                StatusCode::OK,
                Json(
                    serde_json::to_value(payload)
                        .expect("admin payroll dispute payload serialization should succeed"),
                ),
            )
        }
        Err((status, error)) => {
            telemetry.finish_with_http_status(status.as_u16());
            (
                status,
                Json(
                    serde_json::to_value(error.with_request_id(request_id.as_str()))
                        .expect("admin payroll dispute error payload serialization should succeed"),
                ),
            )
        }
    };

    response
}

fn handle_update_admin_payroll_dispute(
    state: &AppState,
    dispute_id_raw: String,
    request: AdminPayrollDisputePatchRequest,
) -> Result<PayrollDisputePayload, (StatusCode, ErrorPayload)> {
    let payroll_actor = load_gate_payroll_actor()?;
    let dispute_id = parse_contract_payroll_dispute_id(&dispute_id_raw).map_err(|error| {
        domain_error(
            StatusCode::BAD_REQUEST,
            "BAD_REQUEST",
            format!("disputeId path parameter is invalid: {error}"),
        )
    })?;
    let occurred_at = current_audit_timestamp()?;

    let dispute = match request.operation.as_str() {
        "ASSIGN_OWNER" => {
            let owner_actor_id_raw = request.owner_actor_id.ok_or_else(|| {
                domain_error(
                    StatusCode::BAD_REQUEST,
                    "BAD_REQUEST",
                    "ownerActorId is required for ASSIGN_OWNER".to_owned(),
                )
            })?;
            let owner_actor_id = ActorId::parse(owner_actor_id_raw).map_err(|error| {
                domain_error(
                    StatusCode::BAD_REQUEST,
                    "BAD_REQUEST",
                    format!("ownerActorId is invalid: {error}"),
                )
            })?;
            let note = normalize_optional_patch_note(request.note)?;
            mutate_payroll_ledger_service(state, |service| {
                service.assign_dispute_owner(
                    &payroll_actor,
                    &dispute_id,
                    &owner_actor_id,
                    occurred_at,
                    note,
                )
            })?
        }
        "RESOLVE_REFUND" => {
            let note = parse_required_patch_note(request.note, "note")?;
            mutate_payroll_ledger_service(state, |service| {
                service.resolve_dispute_refund(
                    &payroll_actor,
                    &dispute_id,
                    occurred_at,
                    note,
                    request.refund_amount_minor,
                )
            })?
        }
        "RESOLVE_REJECTED" => {
            let note = parse_required_patch_note(request.note, "note")?;
            mutate_payroll_ledger_service(state, |service| {
                service.resolve_dispute_rejected(&payroll_actor, &dispute_id, occurred_at, note)
            })?
        }
        other => {
            return Err(domain_error(
                StatusCode::BAD_REQUEST,
                "BAD_REQUEST",
                format!("unsupported payroll dispute operation `{other}`"),
            ));
        }
    };

    Ok(to_payroll_dispute_payload(&dispute))
}

async fn list_anomaly_rules(
    State(state): State<AppState>,
    headers: HeaderMap,
) -> (StatusCode, Json<serde_json::Value>) {
    let telemetry =
        TelemetryService::HttpApi.begin_operation("listAnomalyRules", None::<&str>, None::<&str>);
    let request_id = telemetry.correlation_context().request_id().to_owned();
    let committee_actor = match require_corporate_actor_for_role(&headers, Role::CommitteeAdmin) {
        Ok(actor) => actor,
        Err((status, error)) => {
            telemetry.finish_with_http_status(status.as_u16());
            return (
                status,
                Json(
                    serde_json::to_value(error.with_request_id(request_id.as_str()))
                        .expect("authorization error payload serialization should succeed"),
                ),
            );
        }
    };

    let response = match handle_list_anomaly_rules(&state, &committee_actor) {
        Ok(payload) => {
            telemetry.finish_with_http_status(StatusCode::OK.as_u16());
            (
                StatusCode::OK,
                Json(
                    serde_json::to_value(payload)
                        .expect("anomaly rule list payload serialization should succeed"),
                ),
            )
        }
        Err((status, error)) => {
            telemetry.finish_with_http_status(status.as_u16());
            (
                status,
                Json(
                    serde_json::to_value(error.with_request_id(request_id.as_str()))
                        .expect("anomaly rule list error payload serialization should succeed"),
                ),
            )
        }
    };

    response
}

fn handle_list_anomaly_rules(
    state: &AppState,
    _committee_actor: &AuthenticatedActorContext,
) -> Result<AnomalyRuleListResponse, (StatusCode, ErrorPayload)> {
    let rules = with_anomaly_alert_workflow(state, |workflow| workflow.list_rules())?
        .iter()
        .map(to_anomaly_rule_payload)
        .collect::<Vec<_>>();

    Ok(AnomalyRuleListResponse { items: rules })
}

async fn upsert_anomaly_rule(
    State(state): State<AppState>,
    headers: HeaderMap,
    Path(rule_id): Path<String>,
    Json(request): Json<AnomalyRuleUpsertRequest>,
) -> (StatusCode, Json<serde_json::Value>) {
    let telemetry =
        TelemetryService::HttpApi.begin_operation("upsertAnomalyRule", None::<&str>, None::<&str>);
    let request_id = telemetry.correlation_context().request_id().to_owned();
    let committee_actor = match require_corporate_actor_for_role(&headers, Role::CommitteeAdmin) {
        Ok(actor) => actor,
        Err((status, error)) => {
            telemetry.finish_with_http_status(status.as_u16());
            return (
                status,
                Json(
                    serde_json::to_value(error.with_request_id(request_id.as_str()))
                        .expect("authorization error payload serialization should succeed"),
                ),
            );
        }
    };

    let response = match handle_upsert_anomaly_rule(&state, &committee_actor, rule_id, request) {
        Ok(payload) => {
            telemetry.finish_with_http_status(StatusCode::OK.as_u16());
            (
                StatusCode::OK,
                Json(
                    serde_json::to_value(payload)
                        .expect("anomaly rule payload serialization should succeed"),
                ),
            )
        }
        Err((status, error)) => {
            telemetry.finish_with_http_status(status.as_u16());
            (
                status,
                Json(
                    serde_json::to_value(error.with_request_id(request_id.as_str()))
                        .expect("anomaly rule error payload serialization should succeed"),
                ),
            )
        }
    };

    response
}

fn handle_upsert_anomaly_rule(
    state: &AppState,
    committee_actor: &AuthenticatedActorContext,
    rule_id_raw: String,
    request: AnomalyRuleUpsertRequest,
) -> Result<AnomalyRulePayload, (StatusCode, ErrorPayload)> {
    let rule_id = parse_contract_anomaly_rule_id(&rule_id_raw).map_err(|error| {
        domain_error(
            StatusCode::BAD_REQUEST,
            "BAD_REQUEST",
            format!("ruleId path parameter is invalid: {error}"),
        )
    })?;
    let kind = AnomalyRuleKind::parse(&request.kind).ok_or_else(|| {
        domain_error(
            StatusCode::BAD_REQUEST,
            "BAD_REQUEST",
            format!("kind `{}` is unsupported", request.kind.trim()),
        )
    })?;
    let threshold_comparator = AnomalyThresholdComparator::parse(&request.threshold_comparator)
        .ok_or_else(|| {
            domain_error(
                StatusCode::BAD_REQUEST,
                "BAD_REQUEST",
                format!(
                    "thresholdComparator `{}` is unsupported",
                    request.threshold_comparator.trim()
                ),
            )
        })?;
    let severity = AnomalyAlertSeverity::parse(&request.severity).ok_or_else(|| {
        domain_error(
            StatusCode::BAD_REQUEST,
            "BAD_REQUEST",
            format!("severity `{}` is unsupported", request.severity.trim()),
        )
    })?;

    let rule = AnomalyRule::new(
        rule_id,
        kind,
        request.display_name,
        request.description,
        request.governance_issue_id,
        request.enabled,
        request.threshold_value,
        threshold_comparator,
        request.evaluation_window_days,
        request.sla_minutes,
        severity,
    )
    .map_err(map_anomaly_alert_error)?;

    let occurred_at = current_audit_timestamp()?;
    let upserted = mutate_anomaly_alert_workflow(state, |workflow| {
        workflow.upsert_rule(committee_actor, rule, occurred_at)
    })?;
    Ok(to_anomaly_rule_payload(&upserted))
}

async fn evaluate_anomaly_alerts(
    State(state): State<AppState>,
    headers: HeaderMap,
    Json(request): Json<AnomalyAlertEvaluationRequest>,
) -> (StatusCode, Json<serde_json::Value>) {
    let telemetry = TelemetryService::HttpApi.begin_operation(
        "evaluateAnomalyAlerts",
        None::<&str>,
        None::<&str>,
    );
    let request_id = telemetry.correlation_context().request_id().to_owned();
    let committee_actor = match require_corporate_actor_for_role(&headers, Role::CommitteeAdmin) {
        Ok(actor) => actor,
        Err((status, error)) => {
            telemetry.finish_with_http_status(status.as_u16());
            return (
                status,
                Json(
                    serde_json::to_value(error.with_request_id(request_id.as_str()))
                        .expect("authorization error payload serialization should succeed"),
                ),
            );
        }
    };

    let response = match handle_evaluate_anomaly_alerts(&state, &committee_actor, request) {
        Ok(payload) => {
            telemetry.finish_with_http_status(StatusCode::OK.as_u16());
            (
                StatusCode::OK,
                Json(
                    serde_json::to_value(payload)
                        .expect("anomaly evaluation payload serialization should succeed"),
                ),
            )
        }
        Err((status, error)) => {
            telemetry.finish_with_http_status(status.as_u16());
            (
                status,
                Json(
                    serde_json::to_value(error.with_request_id(request_id.as_str()))
                        .expect("anomaly evaluation error payload serialization should succeed"),
                ),
            )
        }
    };

    response
}

fn handle_evaluate_anomaly_alerts(
    state: &AppState,
    committee_actor: &AuthenticatedActorContext,
    request: AnomalyAlertEvaluationRequest,
) -> Result<AnomalyAlertEvaluationResponse, (StatusCode, ErrorPayload)> {
    let AnomalyAlertEvaluationRequest {
        vendor_id,
        observed_at_epoch_day,
        observed_at_minute_of_day,
        days_until_expiry,
        on_time_rate,
        satisfaction_score,
        complaint_count,
        default_owner_actor_id,
    } = request;

    if days_until_expiry.is_none()
        && on_time_rate.is_none()
        && satisfaction_score.is_none()
        && complaint_count.is_none()
    {
        return Err(domain_error(
            StatusCode::BAD_REQUEST,
            "BAD_REQUEST",
            "at least one anomaly signal metric is required".to_owned(),
        ));
    }

    let days_until_expiry =
        validate_anomaly_metric_minimum("daysUntilExpiry", days_until_expiry, 0.0)?;
    let on_time_rate = validate_anomaly_metric_range("onTimeRate", on_time_rate, 0.0, 1.0)?;
    let satisfaction_score =
        validate_anomaly_metric_range("satisfactionScore", satisfaction_score, 0.0, 5.0)?;
    let complaint_count = validate_anomaly_metric_minimum("complaintCount", complaint_count, 0.0)?;

    let vendor_id = VendorId::parse(vendor_id).map_err(|error| {
        domain_error(
            StatusCode::BAD_REQUEST,
            "BAD_REQUEST",
            format!("vendorId is invalid: {error}"),
        )
    })?;
    let observed_at =
        resolve_anomaly_observed_at_timestamp(observed_at_epoch_day, observed_at_minute_of_day)?;
    let default_owner_actor_id = match default_owner_actor_id {
        Some(value) => ActorId::parse(value).map_err(|error| {
            domain_error(
                StatusCode::BAD_REQUEST,
                "BAD_REQUEST",
                format!("defaultOwnerActorId is invalid: {error}"),
            )
        })?,
        None => load_gate_anomaly_alert_owner_actor_id()?,
    };

    let result = mutate_anomaly_alert_workflow(state, |workflow| {
        workflow.evaluate_rules(
            committee_actor,
            AnomalySignalSnapshot::new(vendor_id, observed_at)
                .with_days_until_expiry(days_until_expiry)
                .with_on_time_rate(on_time_rate)
                .with_satisfaction_score(satisfaction_score)
                .with_complaint_count(complaint_count),
            &default_owner_actor_id,
        )
    })?;

    let as_of = current_anomaly_audit_timestamp()?;
    record_operations_analytics_anomaly_triggered_best_effort(state, result.triggered_alerts());
    let triggered_alerts = result
        .triggered_alerts()
        .iter()
        .map(|alert| to_anomaly_alert_payload(alert, as_of))
        .collect::<Vec<_>>();
    Ok(AnomalyAlertEvaluationResponse { triggered_alerts })
}

fn validate_anomaly_metric_minimum(
    field_name: &'static str,
    value: Option<f64>,
    minimum_inclusive: f64,
) -> Result<Option<f64>, (StatusCode, ErrorPayload)> {
    match value {
        None => Ok(None),
        Some(value) => {
            if !value.is_finite() {
                return Err(domain_error(
                    StatusCode::BAD_REQUEST,
                    "BAD_REQUEST",
                    format!("{field_name} must be a finite number"),
                ));
            }
            if value < minimum_inclusive {
                return Err(domain_error(
                    StatusCode::BAD_REQUEST,
                    "BAD_REQUEST",
                    format!("{field_name} must be greater than or equal to {minimum_inclusive}"),
                ));
            }
            Ok(Some(value))
        }
    }
}

fn validate_anomaly_metric_range(
    field_name: &'static str,
    value: Option<f64>,
    minimum_inclusive: f64,
    maximum_inclusive: f64,
) -> Result<Option<f64>, (StatusCode, ErrorPayload)> {
    match validate_anomaly_metric_minimum(field_name, value, minimum_inclusive)? {
        None => Ok(None),
        Some(value) => {
            if value > maximum_inclusive {
                return Err(domain_error(
                    StatusCode::BAD_REQUEST,
                    "BAD_REQUEST",
                    format!("{field_name} must be less than or equal to {maximum_inclusive}"),
                ));
            }
            Ok(Some(value))
        }
    }
}

async fn list_anomaly_alerts(
    State(state): State<AppState>,
    headers: HeaderMap,
    Query(query): Query<AnomalyAlertQueryRequest>,
) -> (StatusCode, Json<serde_json::Value>) {
    let telemetry =
        TelemetryService::HttpApi.begin_operation("listAnomalyAlerts", None::<&str>, None::<&str>);
    let request_id = telemetry.correlation_context().request_id().to_owned();
    let committee_actor = match require_corporate_actor_for_role(&headers, Role::CommitteeAdmin) {
        Ok(actor) => actor,
        Err((status, error)) => {
            telemetry.finish_with_http_status(status.as_u16());
            return (
                status,
                Json(
                    serde_json::to_value(error.with_request_id(request_id.as_str()))
                        .expect("authorization error payload serialization should succeed"),
                ),
            );
        }
    };

    let response = match handle_list_anomaly_alerts(&state, &committee_actor, query) {
        Ok(payload) => {
            telemetry.finish_with_http_status(StatusCode::OK.as_u16());
            (
                StatusCode::OK,
                Json(
                    serde_json::to_value(payload)
                        .expect("anomaly alert list payload serialization should succeed"),
                ),
            )
        }
        Err((status, error)) => {
            telemetry.finish_with_http_status(status.as_u16());
            (
                status,
                Json(
                    serde_json::to_value(error.with_request_id(request_id.as_str()))
                        .expect("anomaly alert list error payload serialization should succeed"),
                ),
            )
        }
    };

    response
}

fn handle_list_anomaly_alerts(
    state: &AppState,
    committee_actor: &AuthenticatedActorContext,
    query: AnomalyAlertQueryRequest,
) -> Result<AnomalyAlertListResponse, (StatusCode, ErrorPayload)> {
    if committee_actor.role() != Role::CommitteeAdmin {
        return Err(domain_error(
            StatusCode::FORBIDDEN,
            "FORBIDDEN",
            format!(
                "operation requires role {:?}, got {:?}",
                Role::CommitteeAdmin,
                committee_actor.role()
            ),
        ));
    }

    let as_of = resolve_anomaly_as_of_timestamp(query.as_of_epoch_day, query.as_of_minute_of_day)?;
    let vendor_id = query
        .vendor_id
        .map(VendorId::parse)
        .transpose()
        .map_err(|error| {
            domain_error(
                StatusCode::BAD_REQUEST,
                "BAD_REQUEST",
                format!("vendorId is invalid: {error}"),
            )
        })?;
    let owner_actor_id = query
        .owner_actor_id
        .map(ActorId::parse)
        .transpose()
        .map_err(|error| {
            domain_error(
                StatusCode::BAD_REQUEST,
                "BAD_REQUEST",
                format!("ownerActorId is invalid: {error}"),
            )
        })?;
    let status = query
        .status
        .map(|value| {
            AnomalyAlertStatus::parse(&value).ok_or_else(|| {
                domain_error(
                    StatusCode::BAD_REQUEST,
                    "BAD_REQUEST",
                    format!("status `{}` is unsupported", value.trim()),
                )
            })
        })
        .transpose()?;
    let sla_status = query
        .sla_status
        .map(|value| {
            AnomalySlaStatus::parse(&value).ok_or_else(|| {
                domain_error(
                    StatusCode::BAD_REQUEST,
                    "BAD_REQUEST",
                    format!("slaStatus `{}` is unsupported", value.trim()),
                )
            })
        })
        .transpose()?;

    let alerts = with_anomaly_alert_workflow(state, |workflow| {
        workflow.query_alerts(
            &corporate_catering_system::anomaly_alert::AnomalyAlertQuery {
                vendor_id,
                owner_actor_id,
                status,
                escalated_only: query.escalated_only,
                sla_status,
            },
            as_of,
        )
    })?
    .iter()
    .map(|alert| to_anomaly_alert_payload(alert, as_of))
    .collect::<Vec<_>>();

    Ok(AnomalyAlertListResponse { items: alerts })
}

async fn get_admin_operations_analytics_dashboard(
    State(state): State<AppState>,
    headers: HeaderMap,
    Query(query): Query<OperationsAnalyticsDashboardQueryRequest>,
) -> (StatusCode, Json<serde_json::Value>) {
    let telemetry = TelemetryService::HttpApi.begin_operation(
        "getAdminOperationsAnalyticsDashboard",
        None::<&str>,
        Some(state.plant_id.as_str()),
    );
    let request_id = telemetry.correlation_context().request_id().to_owned();
    let committee_actor = match require_corporate_actor_for_role(&headers, Role::CommitteeAdmin) {
        Ok(actor) => actor,
        Err((status, error)) => {
            telemetry.finish_with_http_status(status.as_u16());
            return (
                status,
                Json(
                    serde_json::to_value(error.with_request_id(request_id.as_str()))
                        .expect("authorization error payload serialization should succeed"),
                ),
            );
        }
    };

    let response =
        match handle_get_admin_operations_analytics_dashboard(&state, &committee_actor, query) {
            Ok(payload) => {
                telemetry.finish_with_http_status(StatusCode::OK.as_u16());
                (
                    StatusCode::OK,
                    Json(
                        serde_json::to_value(payload).expect(
                            "admin operations analytics payload serialization should succeed",
                        ),
                    ),
                )
            }
            Err((status, error)) => {
                telemetry.finish_with_http_status(status.as_u16());
                (
                    status,
                    Json(
                        serde_json::to_value(error.with_request_id(request_id.as_str())).expect(
                            "admin operations analytics error payload serialization should succeed",
                        ),
                    ),
                )
            }
        };

    response
}

fn handle_get_admin_operations_analytics_dashboard(
    state: &AppState,
    _committee_actor: &AuthenticatedActorContext,
    query: OperationsAnalyticsDashboardQueryRequest,
) -> Result<OperationsAnalyticsDashboardPayload, (StatusCode, ErrorPayload)> {
    handle_get_operations_analytics_dashboard(state, query, None)
}

async fn get_vendor_operations_analytics_dashboard(
    State(state): State<AppState>,
    headers: HeaderMap,
    Query(query): Query<OperationsAnalyticsDashboardQueryRequest>,
) -> (StatusCode, Json<serde_json::Value>) {
    let telemetry = TelemetryService::HttpApi.begin_operation(
        "getVendorOperationsAnalyticsDashboard",
        None::<&str>,
        Some(state.plant_id.as_str()),
    );
    let request_id = telemetry.correlation_context().request_id().to_owned();
    let vendor_actor = match require_vendor_operator_actor(&headers) {
        Ok(actor) => actor,
        Err((status, error)) => {
            telemetry.finish_with_http_status(status.as_u16());
            return (
                status,
                Json(
                    serde_json::to_value(error.with_request_id(request_id.as_str()))
                        .expect("authorization error payload serialization should succeed"),
                ),
            );
        }
    };

    let response =
        match handle_get_vendor_operations_analytics_dashboard(&state, &vendor_actor, query) {
            Ok(payload) => {
                telemetry.finish_with_http_status(StatusCode::OK.as_u16());
                (
                    StatusCode::OK,
                    Json(serde_json::to_value(payload).expect(
                        "vendor operations analytics payload serialization should succeed",
                    )),
                )
            }
            Err((status, error)) => {
                telemetry.finish_with_http_status(status.as_u16());
                (
                status,
                Json(
                    serde_json::to_value(error.with_request_id(request_id.as_str())).expect(
                        "vendor operations analytics error payload serialization should succeed",
                    ),
                ),
            )
            }
        };

    response
}

fn handle_get_vendor_operations_analytics_dashboard(
    state: &AppState,
    _vendor_actor: &AuthenticatedActorContext,
    query: OperationsAnalyticsDashboardQueryRequest,
) -> Result<OperationsAnalyticsDashboardPayload, (StatusCode, ErrorPayload)> {
    handle_get_operations_analytics_dashboard(state, query, Some(state.vendor_id.as_str()))
}

fn handle_get_operations_analytics_dashboard(
    state: &AppState,
    query: OperationsAnalyticsDashboardQueryRequest,
    vendor_scope: Option<&str>,
) -> Result<OperationsAnalyticsDashboardPayload, (StatusCode, ErrorPayload)> {
    if !state.advanced_analytics_dashboard_runtime_enabled {
        return Err(domain_error(
            StatusCode::NOT_FOUND,
            "NOT_FOUND",
            "advanced operations analytics dashboard endpoint is unavailable while feature flag is disabled"
                .to_owned(),
        ));
    }

    let generated_at = current_anomaly_audit_timestamp()?;
    let (from_epoch_day, to_epoch_day) =
        resolve_operations_analytics_query_range(query, generated_at.epoch_day())?;
    let snapshot = with_operations_analytics_warehouse(state, |warehouse| {
        warehouse.query(OperationsAnalyticsQuery {
            from_epoch_day,
            to_epoch_day,
            vendor_scope,
        })
    })?;

    Ok(to_operations_analytics_dashboard_payload(
        snapshot,
        generated_at,
    ))
}

fn resolve_operations_analytics_query_range(
    query: OperationsAnalyticsDashboardQueryRequest,
    default_to_epoch_day: i32,
) -> Result<(i32, i32), (StatusCode, ErrorPayload)> {
    let default_from_epoch_day =
        default_to_epoch_day.saturating_sub(DEFAULT_ADVANCED_ANALYTICS_LOOKBACK_DAYS - 1);
    let from_epoch_day = query.from_epoch_day.unwrap_or(default_from_epoch_day);
    let to_epoch_day = query.to_epoch_day.unwrap_or(default_to_epoch_day);

    if from_epoch_day <= 0 || to_epoch_day <= 0 {
        return Err(domain_error(
            StatusCode::BAD_REQUEST,
            "BAD_REQUEST",
            "fromEpochDay and toEpochDay must be positive epoch-day integers".to_owned(),
        ));
    }
    if from_epoch_day > to_epoch_day {
        return Err(domain_error(
            StatusCode::BAD_REQUEST,
            "BAD_REQUEST",
            "fromEpochDay must be less than or equal to toEpochDay".to_owned(),
        ));
    }
    let range_days = to_epoch_day
        .saturating_sub(from_epoch_day)
        .saturating_add(1);
    if range_days > MAX_ADVANCED_ANALYTICS_RANGE_DAYS {
        return Err(domain_error(
            StatusCode::BAD_REQUEST,
            "BAD_REQUEST",
            format!(
                "analytics query range exceeds maximum supported span of {MAX_ADVANCED_ANALYTICS_RANGE_DAYS} days"
            ),
        ));
    }

    Ok((from_epoch_day, to_epoch_day))
}

fn to_operations_analytics_dashboard_payload(
    snapshot: OperationsAnalyticsDashboardSnapshot,
    generated_at: AuditTimestamp,
) -> OperationsAnalyticsDashboardPayload {
    OperationsAnalyticsDashboardPayload {
        metric_schema_version: snapshot.metric_schema_version.to_owned(),
        generated_at: audit_timestamp_to_iso_datetime(generated_at),
        from_epoch_day: snapshot.from_epoch_day,
        to_epoch_day: snapshot.to_epoch_day,
        metric_definitions: snapshot
            .metric_definitions
            .into_iter()
            .map(|definition| OperationsAnalyticsMetricDefinitionPayload {
                key: definition.key.to_owned(),
                display_name: definition.display_name.to_owned(),
                unit: definition.unit.to_owned(),
                formula: definition.formula.to_owned(),
                source: definition.source.to_owned(),
                version: definition.version.to_owned(),
            })
            .collect(),
        vendor_breakdown: snapshot
            .vendor_breakdown
            .into_iter()
            .map(|row| OperationsAnalyticsVendorBreakdownPayload {
                vendor_id: row.dimension_value,
                metrics: to_operations_analytics_metric_values_payload(row.metrics),
            })
            .collect(),
        plant_breakdown: snapshot
            .plant_breakdown
            .into_iter()
            .map(|row| OperationsAnalyticsPlantBreakdownPayload {
                plant_id: row.dimension_value,
                metrics: to_operations_analytics_metric_values_payload(row.metrics),
            })
            .collect(),
        time_breakdown: snapshot
            .time_breakdown
            .into_iter()
            .map(|row| OperationsAnalyticsTimeBreakdownPayload {
                epoch_day: row.epoch_day,
                date: epoch_day_to_iso_date(row.epoch_day),
                metrics: to_operations_analytics_metric_values_payload(row.metrics),
            })
            .collect(),
    }
}

fn to_operations_analytics_metric_values_payload(
    metrics: Vec<corporate_catering_system::operations_analytics::OperationsAnalyticsMetricValue>,
) -> Vec<OperationsAnalyticsMetricValuePayload> {
    metrics
        .into_iter()
        .map(|metric| OperationsAnalyticsMetricValuePayload {
            metric_key: metric.key.to_owned(),
            value: metric.value,
        })
        .collect()
}

async fn update_admin_anomaly_alert(
    State(state): State<AppState>,
    headers: HeaderMap,
    Path(alert_id): Path<String>,
    Json(request): Json<AdminAnomalyAlertPatchRequest>,
) -> (StatusCode, Json<serde_json::Value>) {
    let telemetry = TelemetryService::HttpApi.begin_operation(
        "updateAdminAnomalyAlert",
        None::<&str>,
        None::<&str>,
    );
    let request_id = telemetry.correlation_context().request_id().to_owned();
    let committee_actor = match require_corporate_actor_for_role(&headers, Role::CommitteeAdmin) {
        Ok(actor) => actor,
        Err((status, error)) => {
            telemetry.finish_with_http_status(status.as_u16());
            return (
                status,
                Json(
                    serde_json::to_value(error.with_request_id(request_id.as_str()))
                        .expect("authorization error payload serialization should succeed"),
                ),
            );
        }
    };

    let response =
        match handle_update_admin_anomaly_alert(&state, &committee_actor, alert_id, request) {
            Ok(payload) => {
                telemetry.finish_with_http_status(StatusCode::OK.as_u16());
                (
                    StatusCode::OK,
                    Json(
                        serde_json::to_value(payload)
                            .expect("anomaly alert patch payload serialization should succeed"),
                    ),
                )
            }
            Err((status, error)) => {
                telemetry.finish_with_http_status(status.as_u16());
                (
                    status,
                    Json(
                        serde_json::to_value(error.with_request_id(request_id.as_str())).expect(
                            "anomaly alert patch error payload serialization should succeed",
                        ),
                    ),
                )
            }
        };

    response
}

fn handle_update_admin_anomaly_alert(
    state: &AppState,
    committee_actor: &AuthenticatedActorContext,
    alert_id_raw: String,
    request: AdminAnomalyAlertPatchRequest,
) -> Result<AnomalyAlertPayload, (StatusCode, ErrorPayload)> {
    let alert_id = parse_contract_anomaly_alert_id(&alert_id_raw).map_err(|error| {
        domain_error(
            StatusCode::BAD_REQUEST,
            "BAD_REQUEST",
            format!("alertId path parameter is invalid: {error}"),
        )
    })?;
    let occurred_at = current_anomaly_audit_timestamp()?;
    let note = normalize_optional_patch_note(request.note)?;

    let alert = match request.operation.as_str() {
        "ASSIGN_OWNER" => {
            if request.closure_note.is_some()
                || request.closure_evidence_refs.is_some()
                || request.ticket_reference.is_some()
            {
                return Err(domain_error(
                    StatusCode::BAD_REQUEST,
                    "BAD_REQUEST",
                    "closure fields are only allowed for CLOSE operation".to_owned(),
                ));
            }
            let owner_actor_id_raw = request.owner_actor_id.ok_or_else(|| {
                domain_error(
                    StatusCode::BAD_REQUEST,
                    "BAD_REQUEST",
                    "ownerActorId is required for ASSIGN_OWNER".to_owned(),
                )
            })?;
            let owner_actor_id = ActorId::parse(owner_actor_id_raw).map_err(|error| {
                domain_error(
                    StatusCode::BAD_REQUEST,
                    "BAD_REQUEST",
                    format!("ownerActorId is invalid: {error}"),
                )
            })?;
            mutate_anomaly_alert_workflow(state, |workflow| {
                workflow.assign_owner(
                    committee_actor,
                    &alert_id,
                    &owner_actor_id,
                    occurred_at,
                    note,
                )
            })?
        }
        "ACKNOWLEDGE" | "START_REMEDIATION" | "ESCALATE" => {
            if request.owner_actor_id.is_some()
                || request.closure_note.is_some()
                || request.closure_evidence_refs.is_some()
                || request.ticket_reference.is_some()
            {
                return Err(domain_error(
                    StatusCode::BAD_REQUEST,
                    "BAD_REQUEST",
                    "owner and closure fields are not allowed for this operation".to_owned(),
                ));
            }
            let transition =
                AnomalyAlertTransition::parse(&request.operation).ok_or_else(|| {
                    domain_error(
                        StatusCode::BAD_REQUEST,
                        "BAD_REQUEST",
                        format!(
                            "unsupported anomaly alert operation `{}`",
                            request.operation
                        ),
                    )
                })?;
            mutate_anomaly_alert_workflow(state, |workflow| {
                workflow.transition_alert(
                    committee_actor,
                    &alert_id,
                    transition,
                    occurred_at,
                    note,
                    None,
                    Vec::new(),
                    None,
                )
            })?
        }
        "CLOSE" => {
            if request.owner_actor_id.is_some() {
                return Err(domain_error(
                    StatusCode::BAD_REQUEST,
                    "BAD_REQUEST",
                    "ownerActorId is not allowed for CLOSE operation".to_owned(),
                ));
            }
            let closure_note = parse_required_patch_note(request.closure_note, "closureNote")?;
            let closure_evidence_refs =
                parse_required_patch_evidence_refs(request.closure_evidence_refs)?;
            mutate_anomaly_alert_workflow(state, |workflow| {
                workflow.transition_alert(
                    committee_actor,
                    &alert_id,
                    AnomalyAlertTransition::Close,
                    occurred_at,
                    note,
                    Some(closure_note),
                    closure_evidence_refs,
                    request.ticket_reference,
                )
            })?
        }
        other => {
            return Err(domain_error(
                StatusCode::BAD_REQUEST,
                "BAD_REQUEST",
                format!("unsupported anomaly alert operation `{other}`"),
            ));
        }
    };

    if alert.status() == AnomalyAlertStatus::Closed {
        record_operations_analytics_anomaly_closed_best_effort(
            state,
            alert.vendor_id(),
            occurred_at.epoch_day(),
        );
    }

    Ok(to_anomaly_alert_payload(&alert, occurred_at))
}

async fn purge_payroll_data(
    State(state): State<AppState>,
    headers: HeaderMap,
    Json(request): Json<PayrollRetentionPurgeRequest>,
) -> (StatusCode, Json<serde_json::Value>) {
    let telemetry =
        TelemetryService::HttpApi.begin_operation("purgePayrollData", None::<&str>, None::<&str>);
    let request_id = telemetry.correlation_context().request_id().to_owned();
    let committee_actor = match require_corporate_actor_for_role(&headers, Role::CommitteeAdmin) {
        Ok(actor) => actor,
        Err((status, error)) => {
            telemetry.finish_with_http_status(status.as_u16());
            return (
                status,
                Json(
                    serde_json::to_value(error.with_request_id(request_id.as_str()))
                        .expect("authorization error payload serialization should succeed"),
                ),
            );
        }
    };

    let response = match handle_purge_payroll_data(&state, &committee_actor, request) {
        Ok(payload) => {
            telemetry.finish_with_http_status(StatusCode::OK.as_u16());
            (
                StatusCode::OK,
                Json(
                    serde_json::to_value(payload)
                        .expect("payroll purge payload serialization should succeed"),
                ),
            )
        }
        Err((status, error)) => {
            telemetry.finish_with_http_status(status.as_u16());
            (
                status,
                Json(
                    serde_json::to_value(error.with_request_id(request_id.as_str()))
                        .expect("payroll purge error payload serialization should succeed"),
                ),
            )
        }
    };

    response
}

async fn purge_order_data(
    State(state): State<AppState>,
    headers: HeaderMap,
    Json(request): Json<OrderRetentionPurgeRequest>,
) -> (StatusCode, Json<serde_json::Value>) {
    let telemetry =
        TelemetryService::HttpApi.begin_operation("purgeOrderData", None::<&str>, None::<&str>);
    let request_id = telemetry.correlation_context().request_id().to_owned();
    let committee_actor = match require_corporate_actor_for_role(&headers, Role::CommitteeAdmin) {
        Ok(actor) => actor,
        Err((status, error)) => {
            telemetry.finish_with_http_status(status.as_u16());
            return (
                status,
                Json(
                    serde_json::to_value(error.with_request_id(request_id.as_str()))
                        .expect("authorization error payload serialization should succeed"),
                ),
            );
        }
    };

    let response = match handle_purge_order_data(&state, &committee_actor, request) {
        Ok(payload) => {
            telemetry.finish_with_http_status(StatusCode::OK.as_u16());
            (
                StatusCode::OK,
                Json(
                    serde_json::to_value(payload)
                        .expect("order purge payload serialization should succeed"),
                ),
            )
        }
        Err((status, error)) => {
            telemetry.finish_with_http_status(status.as_u16());
            (
                status,
                Json(
                    serde_json::to_value(error.with_request_id(request_id.as_str()))
                        .expect("order purge error payload serialization should succeed"),
                ),
            )
        }
    };

    response
}

fn handle_purge_payroll_data(
    state: &AppState,
    committee_actor: &AuthenticatedActorContext,
    request: PayrollRetentionPurgeRequest,
) -> Result<PayrollRetentionPurgeResponse, (StatusCode, ErrorPayload)> {
    let as_of = match request.as_of_epoch_day {
        Some(epoch_day) => AuditTimestamp::from_epoch_day(epoch_day),
        None => AuditTimestamp::now_taipei().map_err(|error| {
            domain_error(
                StatusCode::INTERNAL_SERVER_ERROR,
                "PAYROLL_LEDGER_INTERNAL_ERROR",
                error.to_string(),
            )
        })?,
    };
    let report = mutate_payroll_ledger_service(state, |service| {
        service.purge_expired_data(committee_actor, as_of)
    })?;

    Ok(PayrollRetentionPurgeResponse {
        purged_ledger_entries: report.purged_ledger_entries,
        purged_disputes: report.purged_disputes,
        purged_exchange_batches: report.purged_exchange_batches,
        as_of_epoch_day: as_of.epoch_day(),
    })
}

fn handle_purge_order_data(
    state: &AppState,
    committee_actor: &AuthenticatedActorContext,
    request: OrderRetentionPurgeRequest,
) -> Result<OrderRetentionPurgeResponse, (StatusCode, ErrorPayload)> {
    let as_of = match request.as_of_epoch_day {
        Some(epoch_day) => AuditTimestamp::from_epoch_day(epoch_day),
        None => AuditTimestamp::now_taipei().map_err(|error| {
            domain_error(
                StatusCode::INTERNAL_SERVER_ERROR,
                "ORDER_RETENTION_PURGE_INTERNAL_ERROR",
                error.to_string(),
            )
        })?,
    };
    let report = mutate_menu_supply_policy(
        state,
        |policy| policy.purge_expired_orders(committee_actor, as_of),
        map_order_retention_purge_error,
        "ORDER_RETENTION_PURGE_INTERNAL_ERROR",
    )?;

    Ok(OrderRetentionPurgeResponse {
        purged_orders: report.purged_orders,
        as_of_epoch_day: as_of.epoch_day(),
    })
}

async fn export_payroll_deductions(
    State(state): State<AppState>,
    headers: HeaderMap,
    Query(query): Query<PayrollExportQuery>,
) -> (StatusCode, Json<serde_json::Value>) {
    let telemetry = TelemetryService::HttpApi.begin_operation(
        "exportPayrollDeductions",
        None::<&str>,
        Some(state.plant_id.as_str()),
    );
    let request_id = telemetry.correlation_context().request_id().to_owned();
    let payroll_actor = match require_corporate_actor_for_role(&headers, Role::PayrollOperator) {
        Ok(actor) => actor,
        Err((status, error)) => {
            telemetry.finish_with_http_status(status.as_u16());
            return (
                status,
                Json(
                    serde_json::to_value(error.with_request_id(request_id.as_str()))
                        .expect("authorization error payload serialization should succeed"),
                ),
            );
        }
    };

    let response = match handle_export_payroll_deductions(&state, &payroll_actor, query) {
        Ok(payload) => {
            telemetry.finish_with_http_status(StatusCode::OK.as_u16());
            (
                StatusCode::OK,
                Json(
                    serde_json::to_value(payload)
                        .expect("payroll deductions payload serialization should succeed"),
                ),
            )
        }
        Err((status, error)) => {
            telemetry.finish_with_http_status(status.as_u16());
            (
                status,
                Json(
                    serde_json::to_value(error.with_request_id(request_id.as_str()))
                        .expect("payroll deductions error payload serialization should succeed"),
                ),
            )
        }
    };

    response
}

fn handle_export_payroll_deductions(
    state: &AppState,
    payroll_actor: &AuthenticatedActorContext,
    query: PayrollExportQuery,
) -> Result<PayrollDeductionPagePayload, (StatusCode, ErrorPayload)> {
    let pay_period = query.pay_period.ok_or_else(|| {
        domain_error(
            StatusCode::BAD_REQUEST,
            "BAD_REQUEST",
            "payPeriod query parameter is required".to_owned(),
        )
    })?;
    let cycle_key = query.cycle_key.ok_or_else(|| {
        domain_error(
            StatusCode::BAD_REQUEST,
            "BAD_REQUEST",
            "cycleKey query parameter is required".to_owned(),
        )
    })?;
    let page = query.page.unwrap_or(1);
    let page_size = query.page_size.unwrap_or(20);
    let sort_by = query
        .sort_by
        .unwrap_or(PayrollSortFieldQuery::DeliveryDate)
        .into_domain();
    let sort_order = query
        .sort_order
        .unwrap_or(SortOrderQuery::Asc)
        .into_payroll_domain();
    let occurred_at = current_audit_timestamp()?;
    let export_page = mutate_payroll_ledger_service(state, |service| {
        service.export_sftp_batch(
            payroll_actor,
            &pay_period,
            &cycle_key,
            page,
            page_size,
            sort_by,
            sort_order,
            occurred_at,
        )
    })?;

    to_payroll_deduction_page_payload(&export_page, &state.payroll_export_field_encryptor)
}

async fn close_payroll_monthly_settlement(
    State(state): State<AppState>,
    headers: HeaderMap,
    request: Option<Json<PayrollMonthlySettlementCloseRequest>>,
) -> (StatusCode, Json<serde_json::Value>) {
    let telemetry = TelemetryService::HttpApi.begin_operation(
        "closePayrollMonthlySettlement",
        None::<&str>,
        Some(state.plant_id.as_str()),
    );
    let request_id = telemetry.correlation_context().request_id().to_owned();
    let payroll_actor = match require_corporate_actor_for_role(&headers, Role::PayrollOperator) {
        Ok(actor) => actor,
        Err((status, error)) => {
            telemetry.finish_with_http_status(status.as_u16());
            return (
                status,
                Json(
                    serde_json::to_value(error.with_request_id(request_id.as_str()))
                        .expect("authorization error payload serialization should succeed"),
                ),
            );
        }
    };

    let response = match handle_close_payroll_monthly_settlement(
        &state,
        &payroll_actor,
        request.map_or_else(PayrollMonthlySettlementCloseRequest::default, |payload| {
            payload.0
        }),
    ) {
        Ok(payload) => {
            telemetry.finish_with_http_status(StatusCode::OK.as_u16());
            (
                StatusCode::OK,
                Json(
                    serde_json::to_value(payload)
                        .expect("monthly settlement payload serialization should succeed"),
                ),
            )
        }
        Err((status, error)) => {
            telemetry.finish_with_http_status(status.as_u16());
            (
                status,
                Json(
                    serde_json::to_value(error.with_request_id(request_id.as_str()))
                        .expect("monthly settlement error payload serialization should succeed"),
                ),
            )
        }
    };

    response
}

fn handle_close_payroll_monthly_settlement(
    state: &AppState,
    payroll_actor: &AuthenticatedActorContext,
    request: PayrollMonthlySettlementCloseRequest,
) -> Result<PayrollDeductionPagePayload, (StatusCode, ErrorPayload)> {
    let page = request.page.unwrap_or(1);
    let page_size = request.page_size.unwrap_or(20);
    let sort_by = request
        .sort_by
        .unwrap_or(PayrollSortFieldQuery::DeliveryDate)
        .into_domain();
    let sort_order = request
        .sort_order
        .unwrap_or(SortOrderQuery::Asc)
        .into_payroll_domain();
    let occurred_at = current_audit_timestamp()?;
    let export_page = mutate_payroll_ledger_service(state, |service| {
        service.close_monthly_settlement(
            payroll_actor,
            request.cycle_key.as_deref(),
            page,
            page_size,
            sort_by,
            sort_order,
            occurred_at,
        )
    })?;

    record_operations_analytics_payroll_settlement_closed_best_effort(
        state,
        occurred_at.epoch_day(),
        export_page.batch(),
    );

    to_payroll_deduction_page_payload(&export_page, &state.payroll_export_field_encryptor)
}

async fn unlock_payroll_settlement_cycle(
    State(state): State<AppState>,
    headers: HeaderMap,
    Path(cycle_key): Path<String>,
    request: Json<PayrollSettlementCycleLockRequest>,
) -> (StatusCode, Json<serde_json::Value>) {
    let telemetry = TelemetryService::HttpApi.begin_operation(
        "unlockPayrollSettlementCycle",
        None::<&str>,
        Some(state.plant_id.as_str()),
    );
    let request_id = telemetry.correlation_context().request_id().to_owned();
    let committee_actor = match require_corporate_actor_for_role(&headers, Role::CommitteeAdmin) {
        Ok(actor) => actor,
        Err((status, error)) => {
            telemetry.finish_with_http_status(status.as_u16());
            return (
                status,
                Json(
                    serde_json::to_value(error.with_request_id(request_id.as_str()))
                        .expect("authorization error payload serialization should succeed"),
                ),
            );
        }
    };

    let response = match handle_unlock_payroll_settlement_cycle(
        &state,
        &committee_actor,
        cycle_key,
        request.0,
    ) {
        Ok(payload) => {
            telemetry.finish_with_http_status(StatusCode::OK.as_u16());
            (
                StatusCode::OK,
                Json(
                    serde_json::to_value(payload)
                        .expect("payroll settlement unlock payload serialization should succeed"),
                ),
            )
        }
        Err((status, error)) => {
            telemetry.finish_with_http_status(status.as_u16());
            (
                status,
                Json(
                    serde_json::to_value(error.with_request_id(request_id.as_str())).expect(
                        "payroll settlement unlock error payload serialization should succeed",
                    ),
                ),
            )
        }
    };

    response
}

fn handle_unlock_payroll_settlement_cycle(
    state: &AppState,
    committee_actor: &AuthenticatedActorContext,
    cycle_key: String,
    request: PayrollSettlementCycleLockRequest,
) -> Result<PayrollSettlementCycleLockResponse, (StatusCode, ErrorPayload)> {
    let occurred_at = current_audit_timestamp()?;
    let reason = parse_required_patch_note(request.reason, "reason")?;
    let receipt = mutate_payroll_ledger_service(state, |service| {
        service.unlock_cycle_for_recompute(committee_actor, &cycle_key, reason, occurred_at)
    })?;

    Ok(PayrollSettlementCycleLockResponse {
        settlement_cycle: to_payroll_settlement_cycle_lock_payload(&receipt),
    })
}

async fn lock_payroll_settlement_cycle(
    State(state): State<AppState>,
    headers: HeaderMap,
    Path(cycle_key): Path<String>,
    request: Json<PayrollSettlementCycleLockRequest>,
) -> (StatusCode, Json<serde_json::Value>) {
    let telemetry = TelemetryService::HttpApi.begin_operation(
        "lockPayrollSettlementCycle",
        None::<&str>,
        Some(state.plant_id.as_str()),
    );
    let request_id = telemetry.correlation_context().request_id().to_owned();
    let committee_actor = match require_corporate_actor_for_role(&headers, Role::CommitteeAdmin) {
        Ok(actor) => actor,
        Err((status, error)) => {
            telemetry.finish_with_http_status(status.as_u16());
            return (
                status,
                Json(
                    serde_json::to_value(error.with_request_id(request_id.as_str()))
                        .expect("authorization error payload serialization should succeed"),
                ),
            );
        }
    };

    let response = match handle_lock_payroll_settlement_cycle(
        &state,
        &committee_actor,
        cycle_key,
        request.0,
    ) {
        Ok(payload) => {
            telemetry.finish_with_http_status(StatusCode::OK.as_u16());
            (
                StatusCode::OK,
                Json(
                    serde_json::to_value(payload)
                        .expect("payroll settlement lock payload serialization should succeed"),
                ),
            )
        }
        Err((status, error)) => {
            telemetry.finish_with_http_status(status.as_u16());
            (
                status,
                Json(
                    serde_json::to_value(error.with_request_id(request_id.as_str())).expect(
                        "payroll settlement lock error payload serialization should succeed",
                    ),
                ),
            )
        }
    };

    response
}

fn handle_lock_payroll_settlement_cycle(
    state: &AppState,
    committee_actor: &AuthenticatedActorContext,
    cycle_key: String,
    request: PayrollSettlementCycleLockRequest,
) -> Result<PayrollSettlementCycleLockResponse, (StatusCode, ErrorPayload)> {
    let occurred_at = current_audit_timestamp()?;
    let reason = parse_required_patch_note(request.reason, "reason")?;
    let receipt = mutate_payroll_ledger_service(state, |service| {
        service.lock_cycle(committee_actor, &cycle_key, reason, occurred_at)
    })?;

    Ok(PayrollSettlementCycleLockResponse {
        settlement_cycle: to_payroll_settlement_cycle_lock_payload(&receipt),
    })
}

async fn sync_payroll_hr_api_adjunct(
    State(state): State<AppState>,
    headers: HeaderMap,
    Path(batch_id): Path<String>,
    Json(request): Json<PayrollHrApiSyncRequest>,
) -> (StatusCode, Json<serde_json::Value>) {
    let telemetry = TelemetryService::HttpApi.begin_operation(
        "syncPayrollHrApiAdjunct",
        None::<&str>,
        Some(state.plant_id.as_str()),
    );
    let request_id = telemetry.correlation_context().request_id().to_owned();
    let payroll_actor = match require_corporate_actor_for_role(&headers, Role::PayrollOperator) {
        Ok(actor) => actor,
        Err((status, error)) => {
            telemetry.finish_with_http_status(status.as_u16());
            return (
                status,
                Json(
                    serde_json::to_value(error.with_request_id(request_id.as_str()))
                        .expect("authorization error payload serialization should succeed"),
                ),
            );
        }
    };

    let response =
        match handle_sync_payroll_hr_api_adjunct(&state, &payroll_actor, batch_id, request) {
            Ok(payload) => {
                telemetry.finish_with_http_status(StatusCode::OK.as_u16());
                (
                    StatusCode::OK,
                    Json(
                        serde_json::to_value(payload)
                            .expect("payroll hr api sync payload serialization should succeed"),
                    ),
                )
            }
            Err((status, error)) => {
                telemetry.finish_with_http_status(status.as_u16());
                (
                    status,
                    Json(
                        serde_json::to_value(error.with_request_id(request_id.as_str())).expect(
                            "payroll hr api sync error payload serialization should succeed",
                        ),
                    ),
                )
            }
        };

    response
}

fn handle_sync_payroll_hr_api_adjunct(
    state: &AppState,
    payroll_actor: &AuthenticatedActorContext,
    batch_id_raw: String,
    request: PayrollHrApiSyncRequest,
) -> Result<PayrollHrApiSyncResponse, (StatusCode, ErrorPayload)> {
    let batch_id = parse_contract_payroll_exchange_batch_id(&batch_id_raw).map_err(|error| {
        domain_error(
            StatusCode::BAD_REQUEST,
            "BAD_REQUEST",
            format!("batchId path parameter is invalid: {error}"),
        )
    })?;
    let occurred_at = current_audit_timestamp()?;
    let outcome = request.outcome.into_domain();
    let batch = mutate_payroll_ledger_service(state, |service| {
        service.sync_hr_api_adjunct(payroll_actor, &batch_id, outcome, request.note, occurred_at)
    })?;

    record_operations_analytics_payroll_hr_sync_best_effort(state, occurred_at.epoch_day(), &batch);

    Ok(PayrollHrApiSyncResponse {
        exchange_batch: to_payroll_exchange_batch_payload(&batch),
    })
}

fn to_employee_order_payroll_ledger_response(
    view: &OrderPayrollView,
) -> EmployeeOrderPayrollLedgerResponse {
    EmployeeOrderPayrollLedgerResponse {
        order_id: view.order_id().as_str().to_owned(),
        employee_actor_id: view.employee_actor_id().as_str().to_owned(),
        delivery_date: epoch_day_to_iso_date(view.delivery_epoch_day()),
        currency: view.currency().to_owned(),
        net_amount_minor: view.net_amount_minor(),
        ledger_entries: view
            .ledger_entries()
            .iter()
            .map(|entry| PayrollLedgerEntryPayload {
                ledger_entry_id: entry.entry_id(),
                kind: entry.kind().as_str().to_owned(),
                amount: MenuPricePayload {
                    currency: entry.amount().currency().to_owned(),
                    amount_minor: entry.amount().amount_minor(),
                },
                occurred_at: audit_timestamp_to_iso_datetime(entry.occurred_at()),
                source_event_kind: entry.source_event().kind().as_str().to_owned(),
                source_event_reference: entry.source_event().event_reference().to_owned(),
            })
            .collect::<Vec<_>>(),
        disputes: view
            .disputes()
            .iter()
            .map(to_payroll_dispute_payload)
            .collect::<Vec<_>>(),
    }
}

fn to_payroll_dispute_payload(dispute: &PayrollDisputeRecord) -> PayrollDisputePayload {
    PayrollDisputePayload {
        dispute_id: dispute.dispute_id().as_str().to_owned(),
        order_id: dispute.order_id().as_str().to_owned(),
        employee_actor_id: dispute.employee_actor_id().as_str().to_owned(),
        owner_actor_id: dispute.owner_actor_id().as_str().to_owned(),
        status: dispute.status().as_str().to_owned(),
        opened_at: audit_timestamp_to_iso_datetime(dispute.opened_at()),
        updated_at: audit_timestamp_to_iso_datetime(dispute.updated_at()),
        trace: dispute
            .trace()
            .iter()
            .map(to_payroll_dispute_trace_payload)
            .collect::<Vec<_>>(),
    }
}

fn to_payroll_dispute_trace_payload(
    event: &PayrollDisputeTraceEvent,
) -> PayrollDisputeTracePayload {
    PayrollDisputeTracePayload {
        occurred_at: audit_timestamp_to_iso_datetime(event.occurred_at()),
        actor_id: event.actor_id().as_str().to_owned(),
        event_type: event.event_type().as_str().to_owned(),
        status: event.status().as_str().to_owned(),
        owner_actor_id: event.owner_actor_id().as_str().to_owned(),
        note: event.note().map(str::to_owned),
        source_event_kind: event.source_event().kind().as_str().to_owned(),
        source_event_reference: event.source_event().event_reference().to_owned(),
        refund_ledger_entry_id: event.refund_ledger_entry_id(),
    }
}

fn to_anomaly_rule_payload(rule: &AnomalyRule) -> AnomalyRulePayload {
    AnomalyRulePayload {
        rule_id: rule.rule_id().as_str().to_owned(),
        kind: rule.kind().as_str().to_owned(),
        display_name: rule.display_name().to_owned(),
        description: rule.description().to_owned(),
        governance_issue_id: rule.governance_issue_id().to_owned(),
        enabled: rule.enabled(),
        threshold_value: rule.threshold_value(),
        threshold_comparator: rule.threshold_comparator().as_str().to_owned(),
        evaluation_window_days: rule.evaluation_window_days(),
        sla_minutes: rule.sla_minutes(),
        severity: rule.severity().as_str().to_owned(),
    }
}

fn to_anomaly_alert_payload(
    alert: &AnomalyAlertRecord,
    as_of: AuditTimestamp,
) -> AnomalyAlertPayload {
    AnomalyAlertPayload {
        alert_id: alert.alert_id().as_str().to_owned(),
        vendor_id: alert.vendor_id().as_str().to_owned(),
        rule_id: alert.rule_id().as_str().to_owned(),
        rule_kind: alert.rule_kind().as_str().to_owned(),
        rule_display_name: alert.rule_display_name().to_owned(),
        governance_issue_id: alert.governance_issue_id().to_owned(),
        status: alert.status().as_str().to_owned(),
        owner_actor_id: alert.owner_actor_id().as_str().to_owned(),
        severity: alert.severity().as_str().to_owned(),
        observed_value: alert.observed_value(),
        threshold_value: alert.threshold_value(),
        threshold_comparator: alert.threshold_comparator().as_str().to_owned(),
        observed_at: audit_timestamp_to_iso_datetime(alert.observed_at()),
        opened_at: audit_timestamp_to_iso_datetime(alert.opened_at()),
        updated_at: audit_timestamp_to_iso_datetime(alert.updated_at()),
        sla_due_at: audit_timestamp_to_iso_datetime(alert.sla_due_at()),
        sla_status: alert.sla_status(as_of).as_str().to_owned(),
        escalated_at: alert.escalated_at().map(audit_timestamp_to_iso_datetime),
        closed_at: alert.closed_at().map(audit_timestamp_to_iso_datetime),
        closure_note: alert.closure_note().map(str::to_owned),
        closure_evidence_refs: alert.closure_evidence_refs().to_vec(),
        ticket_reference: alert.ticket_reference().map(str::to_owned),
        trace: alert
            .trace()
            .iter()
            .map(to_anomaly_alert_trace_payload)
            .collect::<Vec<_>>(),
    }
}

fn to_anomaly_alert_trace_payload(event: &AnomalyAlertTraceEvent) -> AnomalyAlertTracePayload {
    AnomalyAlertTracePayload {
        occurred_at: audit_timestamp_to_iso_datetime(event.occurred_at()),
        actor_id: event.actor_id().as_str().to_owned(),
        event_type: event.event_type().as_str().to_owned(),
        status: event.status().as_str().to_owned(),
        note: event.note().map(str::to_owned),
    }
}

fn to_payroll_deduction_page_payload(
    export_page: &PayrollExportPage,
    field_encryptor: &PayrollExportFieldEncryptor,
) -> Result<PayrollDeductionPagePayload, (StatusCode, ErrorPayload)> {
    let total_pages = if export_page.total_items() == 0 {
        0
    } else {
        (export_page.total_items() - 1) / export_page.page_size() + 1
    };
    Ok(PayrollDeductionPagePayload {
        items: export_page
            .items()
            .iter()
            .map(|record| {
                to_payroll_deduction_record_payload(export_page.batch(), record, field_encryptor)
            })
            .collect::<Result<Vec<_>, _>>()?,
        page: PageMetaPayload {
            page: export_page.page(),
            page_size: export_page.page_size(),
            total_items: export_page.total_items(),
            total_pages,
        },
        exchange_batch: to_payroll_exchange_batch_payload(export_page.batch()),
    })
}

fn to_payroll_deduction_record_payload(
    batch: &PayrollExchangeBatch,
    record: &PayrollDeductionRecord,
    field_encryptor: &PayrollExportFieldEncryptor,
) -> Result<PayrollDeductionRecordPayload, (StatusCode, ErrorPayload)> {
    let sensitive_context_prefix = format!(
        "payroll:{}:{}:{}",
        batch.cycle_key(),
        batch.snapshot_checksum(),
        record.order_id().as_str()
    );
    let employee_actor_ciphertext = field_encryptor
        .encrypt_field(
            format!("{sensitive_context_prefix}:employeeActorId").as_str(),
            record.employee_actor_id().as_str(),
        )
        .map_err(|error| {
            domain_error(
                StatusCode::INTERNAL_SERVER_ERROR,
                "PAYROLL_LEDGER_INTERNAL_ERROR",
                format!("failed to encrypt payroll employee actor id: {error}"),
            )
        })?;
    let order_id_ciphertext = field_encryptor
        .encrypt_field(
            format!("{sensitive_context_prefix}:orderId").as_str(),
            record.order_id().as_str(),
        )
        .map_err(|error| {
            domain_error(
                StatusCode::INTERNAL_SERVER_ERROR,
                "PAYROLL_LEDGER_INTERNAL_ERROR",
                format!("failed to encrypt payroll order id: {error}"),
            )
        })?;
    let amount_plaintext = serde_json::to_string(&MenuPricePayload {
        currency: record.amount().currency().to_owned(),
        amount_minor: record.amount().amount_minor(),
    })
    .map_err(|error| {
        domain_error(
            StatusCode::INTERNAL_SERVER_ERROR,
            "PAYROLL_LEDGER_INTERNAL_ERROR",
            format!("failed to serialize payroll amount for encryption: {error}"),
        )
    })?;
    let amount_ciphertext = field_encryptor
        .encrypt_field(
            format!("{sensitive_context_prefix}:amount").as_str(),
            amount_plaintext.as_str(),
        )
        .map_err(|error| {
            domain_error(
                StatusCode::INTERNAL_SERVER_ERROR,
                "PAYROLL_LEDGER_INTERNAL_ERROR",
                format!("failed to encrypt payroll amount: {error}"),
            )
        })?;

    Ok(PayrollDeductionRecordPayload {
        employee_actor_ciphertext,
        order_id_ciphertext,
        delivery_date: epoch_day_to_iso_date(record.delivery_epoch_day()),
        amount_ciphertext,
        pay_period: record.pay_period().to_owned(),
        status: record.status().as_str().to_owned(),
        dispute_status: record
            .dispute_status()
            .map(|status| status.as_str().to_owned()),
        source_entry_ids: record.source_entry_ids().to_vec(),
    })
}

fn to_payroll_exchange_batch_payload(batch: &PayrollExchangeBatch) -> PayrollExchangeBatchPayload {
    let (hr_api_sync_status, hr_api_synced_at) = match batch.hr_api_sync_receipt() {
        Some(receipt) => (
            receipt.status().as_str().to_owned(),
            Some(audit_timestamp_to_iso_datetime(receipt.synced_at())),
        ),
        None => ("NOT_SYNCED".to_owned(), None),
    };
    PayrollExchangeBatchPayload {
        batch_id: batch.batch_id().as_str().to_owned(),
        pay_period: batch.pay_period().to_owned(),
        cycle_key: batch.cycle_key().to_owned(),
        generated_at: audit_timestamp_to_iso_datetime(batch.generated_at()),
        cycle_start_date: epoch_day_to_iso_date(batch.cycle_start_epoch_day()),
        cycle_end_date: epoch_day_to_iso_date(batch.cycle_end_epoch_day()),
        snapshot_checksum: batch.snapshot_checksum().to_owned(),
        reconciliation: to_payroll_reconciliation_payload(batch.reconciliation()),
        exchange_path: "SFTP_BATCH",
        hr_api_sync_status,
        hr_api_synced_at,
    }
}

fn to_payroll_reconciliation_payload(
    reconciliation: &PayrollReconciliationMetadata,
) -> PayrollReconciliationPayload {
    PayrollReconciliationPayload {
        total_records: reconciliation.total_records(),
        total_amount_minor: reconciliation.total_amount_minor(),
        total_source_entries: reconciliation.total_source_entries(),
        ready_records: reconciliation.ready_records(),
        locked_records: reconciliation.locked_records(),
        refunded_records: reconciliation.refunded_records(),
        disputed_records: reconciliation.disputed_records(),
        deduction_failed_records: reconciliation.deduction_failed_records(),
        employee_terminated_records: reconciliation.employee_terminated_records(),
        required_exception_classes: reconciliation
            .required_exception_classes()
            .iter()
            .map(|class| class.as_str().to_owned())
            .collect::<Vec<_>>(),
        present_exception_classes: reconciliation
            .present_exception_classes()
            .iter()
            .map(|class| class.as_str().to_owned())
            .collect::<Vec<_>>(),
    }
}

fn to_payroll_settlement_cycle_lock_payload(
    receipt: &PayrollSettlementLockReceipt,
) -> PayrollSettlementCycleLockPayload {
    PayrollSettlementCycleLockPayload {
        cycle_key: receipt.cycle_key().to_owned(),
        pay_period: receipt.pay_period().to_owned(),
        lock_state: receipt.lock_state().as_str().to_owned(),
        batch_id: receipt.batch_id().as_str().to_owned(),
        snapshot_checksum: receipt.snapshot_checksum().to_owned(),
        reason: receipt.reason().to_owned(),
        changed_at: audit_timestamp_to_iso_datetime(receipt.changed_at()),
        actor_id: receipt.actor_id().as_str().to_owned(),
    }
}

fn parse_contract_payroll_dispute_id(value: &str) -> Result<PayrollDisputeId, String> {
    let trimmed = value.trim();
    let Some(suffix) = trimmed.strip_prefix("dsp-") else {
        return Err("must start with `dsp-`".to_owned());
    };
    if suffix.len() != 16 {
        return Err("suffix length must be exactly 16 characters".to_owned());
    }
    if !suffix
        .chars()
        .all(|ch| ch.is_ascii_digit() || ('a'..='f').contains(&ch))
    {
        return Err("suffix must contain only lowercase hex digits".to_owned());
    }

    PayrollDisputeId::parse(trimmed.to_owned()).map_err(|error| error.to_string())
}

fn parse_contract_payroll_exchange_batch_id(value: &str) -> Result<PayrollExchangeBatchId, String> {
    let trimmed = value.trim();
    let Some(payload) = trimmed.strip_prefix("sftp-") else {
        return Err("must start with `sftp-`".to_owned());
    };
    let mut parts = payload.split('-');
    let Some(pay_period_compact) = parts.next() else {
        return Err("must include compact pay period segment".to_owned());
    };
    let Some(sequence) = parts.next() else {
        return Err("must include batch sequence segment".to_owned());
    };
    if parts.next().is_some() {
        return Err("must include exactly two `sftp-` segments".to_owned());
    }
    if pay_period_compact.len() != 6
        || !pay_period_compact
            .chars()
            .all(|character| character.is_ascii_digit())
    {
        return Err("pay period segment must be YYYYMM digits".to_owned());
    }
    if sequence.len() != 16
        || !sequence
            .chars()
            .all(|ch| ch.is_ascii_digit() || ('a'..='f').contains(&ch))
    {
        return Err("batch sequence segment must be 16 lowercase hex digits".to_owned());
    }

    PayrollExchangeBatchId::parse(trimmed.to_owned()).map_err(|error| error.to_string())
}

fn parse_contract_anomaly_rule_id(value: &str) -> Result<AnomalyRuleId, String> {
    let Some(suffix) = value.strip_prefix("rule-") else {
        return Err("must start with `rule-`".to_owned());
    };
    if !(3..=64).contains(&suffix.len()) {
        return Err("suffix length must be between 3 and 64 characters".to_owned());
    }
    if !suffix.chars().all(|character| {
        character.is_ascii_lowercase() || character.is_ascii_digit() || character == '-'
    }) {
        return Err("suffix must contain only lowercase letters, digits, or `-`".to_owned());
    }
    AnomalyRuleId::parse(value.to_owned()).map_err(|error| error.to_string())
}

fn parse_contract_anomaly_alert_id(value: &str) -> Result<AnomalyAlertId, String> {
    let Some(suffix) = value.strip_prefix("alt-") else {
        return Err("must start with `alt-`".to_owned());
    };
    if suffix.len() != 16 {
        return Err("suffix length must be exactly 16 characters".to_owned());
    }
    if !suffix
        .chars()
        .all(|ch| ch.is_ascii_digit() || ('a'..='f').contains(&ch))
    {
        return Err("suffix must contain only lowercase hex digits".to_owned());
    }
    AnomalyAlertId::parse(value.to_owned()).map_err(|error| error.to_string())
}

fn resolve_anomaly_observed_at_timestamp(
    observed_at_epoch_day: Option<i32>,
    observed_at_minute_of_day: Option<u16>,
) -> Result<AuditTimestamp, (StatusCode, ErrorPayload)> {
    match (observed_at_epoch_day, observed_at_minute_of_day) {
        (Some(epoch_day), Some(minute_of_day)) => AuditTimestamp::new(epoch_day, minute_of_day)
            .map_err(|error| {
                domain_error(
                    StatusCode::BAD_REQUEST,
                    "BAD_REQUEST",
                    format!("observedAt minute/day combination is invalid: {error}"),
                )
            }),
        (Some(epoch_day), None) => Ok(AuditTimestamp::from_epoch_day(epoch_day)),
        (None, Some(_)) => Err(domain_error(
            StatusCode::BAD_REQUEST,
            "BAD_REQUEST",
            "observedAtMinuteOfDay requires observedAtEpochDay".to_owned(),
        )),
        (None, None) => current_anomaly_audit_timestamp(),
    }
}

fn resolve_anomaly_as_of_timestamp(
    as_of_epoch_day: Option<i32>,
    as_of_minute_of_day: Option<u16>,
) -> Result<AuditTimestamp, (StatusCode, ErrorPayload)> {
    match (as_of_epoch_day, as_of_minute_of_day) {
        (Some(epoch_day), Some(minute_of_day)) => AuditTimestamp::new(epoch_day, minute_of_day)
            .map_err(|error| {
                domain_error(
                    StatusCode::BAD_REQUEST,
                    "BAD_REQUEST",
                    format!("asOf minute/day combination is invalid: {error}"),
                )
            }),
        (Some(epoch_day), None) => Ok(AuditTimestamp::through_epoch_day(epoch_day)),
        (None, Some(_)) => Err(domain_error(
            StatusCode::BAD_REQUEST,
            "BAD_REQUEST",
            "asOfMinuteOfDay requires asOfEpochDay".to_owned(),
        )),
        (None, None) => current_anomaly_audit_timestamp(),
    }
}

fn parse_required_patch_note(
    note: Option<String>,
    field_name: &str,
) -> Result<String, (StatusCode, ErrorPayload)> {
    let note = note.ok_or_else(|| {
        domain_error(
            StatusCode::BAD_REQUEST,
            "BAD_REQUEST",
            format!("{field_name} is required for this operation"),
        )
    })?;
    let trimmed = note.trim();
    if trimmed.is_empty() {
        return Err(domain_error(
            StatusCode::BAD_REQUEST,
            "BAD_REQUEST",
            format!("{field_name} must be non-empty when provided"),
        ));
    }
    Ok(trimmed.to_owned())
}

fn parse_required_patch_evidence_refs(
    refs: Option<Vec<String>>,
) -> Result<Vec<String>, (StatusCode, ErrorPayload)> {
    let refs = refs.ok_or_else(|| {
        domain_error(
            StatusCode::BAD_REQUEST,
            "BAD_REQUEST",
            "closureEvidenceRefs is required for CLOSE operation".to_owned(),
        )
    })?;
    let normalized = refs
        .into_iter()
        .map(|value| value.trim().to_owned())
        .filter(|value| !value.is_empty())
        .collect::<Vec<_>>();
    if normalized.is_empty() {
        return Err(domain_error(
            StatusCode::BAD_REQUEST,
            "BAD_REQUEST",
            "closureEvidenceRefs must include at least one non-empty evidence reference".to_owned(),
        ));
    }
    Ok(normalized)
}

fn normalize_optional_patch_note(
    note: Option<String>,
) -> Result<Option<String>, (StatusCode, ErrorPayload)> {
    match note {
        Some(value) => {
            let trimmed = value.trim();
            if trimmed.is_empty() {
                return Err(domain_error(
                    StatusCode::BAD_REQUEST,
                    "BAD_REQUEST",
                    "note must be non-empty when provided".to_owned(),
                ));
            }
            Ok(Some(trimmed.to_owned()))
        }
        None => Ok(None),
    }
}

fn current_audit_timestamp() -> Result<AuditTimestamp, (StatusCode, ErrorPayload)> {
    let now = current_taipei_business_moment().map_err(|error| {
        domain_error(
            StatusCode::INTERNAL_SERVER_ERROR,
            "TIME_RESOLUTION_FAILED",
            error,
        )
    })?;
    AuditTimestamp::from_taipei_business_moment(now.epoch_day(), now.minute_of_day()).map_err(
        |error| {
            domain_error(
                StatusCode::INTERNAL_SERVER_ERROR,
                "PAYROLL_LEDGER_INTERNAL_ERROR",
                error.to_string(),
            )
        },
    )
}

fn current_anomaly_audit_timestamp() -> Result<AuditTimestamp, (StatusCode, ErrorPayload)> {
    let now = current_taipei_business_moment().map_err(|error| {
        domain_error(
            StatusCode::INTERNAL_SERVER_ERROR,
            "TIME_RESOLUTION_FAILED",
            error,
        )
    })?;
    AuditTimestamp::from_taipei_business_moment(now.epoch_day(), now.minute_of_day()).map_err(
        |error| {
            domain_error(
                StatusCode::INTERNAL_SERVER_ERROR,
                "ANOMALY_ALERT_INTERNAL_ERROR",
                error.to_string(),
            )
        },
    )
}

fn map_payroll_ledger_error(error: PayrollLedgerError) -> (StatusCode, ErrorPayload) {
    match error {
        PayrollLedgerError::UnauthorizedRole { .. } | PayrollLedgerError::NotOrderOwner { .. } => {
            domain_error(StatusCode::FORBIDDEN, "FORBIDDEN", error.to_string())
        }
        PayrollLedgerError::OrderNotRegistered(_)
        | PayrollLedgerError::DisputeNotFound(_)
        | PayrollLedgerError::ExchangeBatchNotFound(_)
        | PayrollLedgerError::SettlementCycleNotFound { .. } => {
            domain_error(StatusCode::NOT_FOUND, "NOT_FOUND", error.to_string())
        }
        PayrollLedgerError::InvalidDisputeTransition { .. }
        | PayrollLedgerError::NoOutstandingPayrollAmount { .. }
        | PayrollLedgerError::OrderOwnerMismatch { .. }
        | PayrollLedgerError::OrderCurrencyMismatch { .. }
        | PayrollLedgerError::OrderDeliveryDateMismatch { .. }
        | PayrollLedgerError::CycleKeyPayPeriodConflict { .. }
        | PayrollLedgerError::PayPeriodSettlementLocked { .. }
        | PayrollLedgerError::SettlementCycleAlreadyLocked { .. }
        | PayrollLedgerError::SettlementCycleAlreadyUnlocked { .. } => {
            domain_error(StatusCode::CONFLICT, "CONFLICT", error.to_string())
        }
        PayrollLedgerError::InvalidOperationId
        | PayrollLedgerError::InvalidRetentionPolicy
        | PayrollLedgerError::InvalidSourceEventReference
        | PayrollLedgerError::InvalidDisputeId
        | PayrollLedgerError::InvalidExchangeBatchId
        | PayrollLedgerError::InvalidDisputeReason(_)
        | PayrollLedgerError::InvalidPayPeriod(_)
        | PayrollLedgerError::InvalidCycleKey(_)
        | PayrollLedgerError::InvalidSettlementReason(_)
        | PayrollLedgerError::InvalidPagination { .. }
        | PayrollLedgerError::InvalidMoney(_)
        | PayrollLedgerError::RefundAmountOutOfRange { .. } => {
            domain_error(StatusCode::BAD_REQUEST, "BAD_REQUEST", error.to_string())
        }
        PayrollLedgerError::AmountOutOfRange { .. }
        | PayrollLedgerError::LedgerSequenceOverflow
        | PayrollLedgerError::DisputeSequenceOverflow
        | PayrollLedgerError::ExchangeBatchSequenceOverflow
        | PayrollLedgerError::SettlementCycleLockStateMissing { .. }
        | PayrollLedgerError::StatePoisoned
        | PayrollLedgerError::AuditTrail(_) => domain_error(
            StatusCode::INTERNAL_SERVER_ERROR,
            "PAYROLL_LEDGER_INTERNAL_ERROR",
            error.to_string(),
        ),
    }
}

fn map_anomaly_alert_error(error: AnomalyAlertError) -> (StatusCode, ErrorPayload) {
    match error {
        AnomalyAlertError::UnauthorizedRole { .. } => {
            domain_error(StatusCode::FORBIDDEN, "FORBIDDEN", error.to_string())
        }
        AnomalyAlertError::AlertNotFound(_) => {
            domain_error(StatusCode::NOT_FOUND, "NOT_FOUND", error.to_string())
        }
        AnomalyAlertError::AlertAlreadyClosed { .. }
        | AnomalyAlertError::InvalidStatusTransition { .. } => {
            domain_error(StatusCode::CONFLICT, "CONFLICT", error.to_string())
        }
        AnomalyAlertError::InvalidRuleId
        | AnomalyAlertError::InvalidAlertId
        | AnomalyAlertError::InvalidRuleText { .. }
        | AnomalyAlertError::InvalidThresholdValue { .. }
        | AnomalyAlertError::InvalidRuleComparator { .. }
        | AnomalyAlertError::InvalidEvaluationWindowDays { .. }
        | AnomalyAlertError::InvalidSlaMinutes { .. }
        | AnomalyAlertError::ClosureNoteRequired
        | AnomalyAlertError::ClosureEvidenceRequired
        | AnomalyAlertError::InvalidClosureEvidence
        | AnomalyAlertError::InvalidTicketReference
        | AnomalyAlertError::InvalidNote => {
            domain_error(StatusCode::BAD_REQUEST, "BAD_REQUEST", error.to_string())
        }
        AnomalyAlertError::AlertSequenceOverflow
        | AnomalyAlertError::TimestampOverflow
        | AnomalyAlertError::StatePoisoned
        | AnomalyAlertError::AuditTrail(_) => domain_error(
            StatusCode::INTERNAL_SERVER_ERROR,
            "ANOMALY_ALERT_INTERNAL_ERROR",
            error.to_string(),
        ),
    }
}

fn require_corporate_actor_for_role(
    headers: &HeaderMap,
    required_role: Role,
) -> Result<AuthenticatedActorContext, (StatusCode, ErrorPayload)> {
    require_bearer_actor_for_role(headers, required_role, AuthenticationSource::CorporateSso)
}

fn require_vendor_operator_actor(
    headers: &HeaderMap,
) -> Result<AuthenticatedActorContext, (StatusCode, ErrorPayload)> {
    require_bearer_actor_for_role(
        headers,
        Role::VendorOperator,
        AuthenticationSource::VendorAccountMfa,
    )
}

fn require_bearer_actor_for_role(
    headers: &HeaderMap,
    required_role: Role,
    authentication_source: AuthenticationSource,
) -> Result<AuthenticatedActorContext, (StatusCode, ErrorPayload)> {
    let authorization = headers
        .get(AUTHORIZATION)
        .ok_or_else(|| {
            domain_error(
                StatusCode::UNAUTHORIZED,
                "UNAUTHORIZED",
                "authorization header is required".to_owned(),
            )
        })?
        .to_str()
        .map_err(|_| {
            domain_error(
                StatusCode::UNAUTHORIZED,
                "UNAUTHORIZED",
                "authorization header must be ASCII".to_owned(),
            )
        })?;
    let token = authorization
        .strip_prefix(AUTHORIZATION_BEARER_PREFIX)
        .ok_or_else(|| {
            domain_error(
                StatusCode::UNAUTHORIZED,
                "UNAUTHORIZED",
                "authorization header must use Bearer token".to_owned(),
            )
        })?;
    let (actor_id_raw, role_raw) = token.split_once('|').ok_or_else(|| {
        domain_error(
            StatusCode::UNAUTHORIZED,
            "UNAUTHORIZED",
            "bearer token must use `actorId|ROLE` format".to_owned(),
        )
    })?;
    let role = parse_role_label(role_raw).ok_or_else(|| {
        domain_error(
            StatusCode::UNAUTHORIZED,
            "UNAUTHORIZED",
            format!("unsupported bearer role `{}`", role_raw.trim()),
        )
    })?;
    if role != required_role {
        return Err(domain_error(
            StatusCode::FORBIDDEN,
            "FORBIDDEN",
            format!("operation requires role {required_role:?}, got {role:?}"),
        ));
    }
    let actor_id = ActorId::parse(actor_id_raw.trim()).map_err(|error| {
        domain_error(
            StatusCode::UNAUTHORIZED,
            "UNAUTHORIZED",
            format!("bearer actor id is invalid: {error}"),
        )
    })?;
    let plant_scope = if role == Role::VendorOperator {
        let plant_id = PlantId::parse(
            std::env::var("PRELAUNCH_PLANT_ID").unwrap_or_else(|_| DEFAULT_PLANT_ID.to_owned()),
        )
        .map_err(|error| {
            domain_error(
                StatusCode::INTERNAL_SERVER_ERROR,
                "IDENTITY_MODEL_ERROR",
                format!("failed to parse runtime plant id for vendor bearer actor: {error}"),
            )
        })?;
        PlantScope::restricted(vec![plant_id]).map_err(|error| {
            domain_error(
                StatusCode::INTERNAL_SERVER_ERROR,
                "IDENTITY_MODEL_ERROR",
                format!("failed to build vendor bearer actor plant scope: {error}"),
            )
        })?
    } else {
        PlantScope::all()
    };
    AuthenticatedActorContext::new(actor_id, role, plant_scope, authentication_source).map_err(
        |error| {
            domain_error(
                StatusCode::UNAUTHORIZED,
                "UNAUTHORIZED",
                format!("bearer actor context is invalid: {error}"),
            )
        },
    )
}

fn parse_role_label(value: &str) -> Option<Role> {
    match value.trim().to_ascii_uppercase().as_str() {
        "EMPLOYEE" => Some(Role::Employee),
        "VENDOR_OPERATOR" => Some(Role::VendorOperator),
        "COMMITTEE_ADMIN" => Some(Role::CommitteeAdmin),
        "PAYROLL_OPERATOR" => Some(Role::PayrollOperator),
        _ => None,
    }
}

fn load_gate_employee_actor_for_plant(
    state: &AppState,
    plant_id: &PlantId,
) -> Result<AuthenticatedActorContext, (StatusCode, ErrorPayload)> {
    let actor_id = ActorId::parse(LOAD_GATE_EMPLOYEE_ACTOR_ID).map_err(|error| {
        domain_error(
            StatusCode::INTERNAL_SERVER_ERROR,
            "IDENTITY_MODEL_ERROR",
            format!("failed to parse load-gate employee actor id: {error}"),
        )
    })?;
    let plant_scope = PlantScope::restricted(vec![plant_id.clone()]).map_err(|error| {
        domain_error(
            StatusCode::INTERNAL_SERVER_ERROR,
            "IDENTITY_MODEL_ERROR",
            format!("failed to build load-gate employee plant scope: {error}"),
        )
    })?;
    let employment_status = if state.terminated_employee_actor_ids.contains(&actor_id) {
        EmploymentStatus::Terminated
    } else {
        EmploymentStatus::Active
    };
    AuthenticatedActorContext::new_with_employment_status(
        actor_id,
        Role::Employee,
        plant_scope,
        AuthenticationSource::CorporateSso,
        employment_status,
    )
    .map_err(|error| {
        domain_error(
            StatusCode::INTERNAL_SERVER_ERROR,
            "IDENTITY_MODEL_ERROR",
            format!("failed to construct load-gate employee actor context: {error}"),
        )
    })
}

fn load_gate_committee_admin_actor() -> Result<AuthenticatedActorContext, (StatusCode, ErrorPayload)>
{
    let actor_id = ActorId::parse(LOAD_GATE_COMMITTEE_ACTOR_ID).map_err(|error| {
        domain_error(
            StatusCode::INTERNAL_SERVER_ERROR,
            "IDENTITY_MODEL_ERROR",
            format!("failed to parse load-gate committee actor id: {error}"),
        )
    })?;
    AuthenticatedActorContext::new(
        actor_id,
        Role::CommitteeAdmin,
        PlantScope::all(),
        AuthenticationSource::CorporateSso,
    )
    .map_err(|error| {
        domain_error(
            StatusCode::INTERNAL_SERVER_ERROR,
            "IDENTITY_MODEL_ERROR",
            format!("failed to construct load-gate committee actor context: {error}"),
        )
    })
}

fn load_gate_payroll_actor() -> Result<AuthenticatedActorContext, (StatusCode, ErrorPayload)> {
    let actor_id = ActorId::parse(LOAD_GATE_PAYROLL_ACTOR_ID).map_err(|error| {
        domain_error(
            StatusCode::INTERNAL_SERVER_ERROR,
            "IDENTITY_MODEL_ERROR",
            format!("failed to parse load-gate payroll actor id: {error}"),
        )
    })?;
    AuthenticatedActorContext::new(
        actor_id,
        Role::PayrollOperator,
        PlantScope::all(),
        AuthenticationSource::CorporateSso,
    )
    .map_err(|error| {
        domain_error(
            StatusCode::INTERNAL_SERVER_ERROR,
            "IDENTITY_MODEL_ERROR",
            format!("failed to construct load-gate payroll actor context: {error}"),
        )
    })
}

fn load_gate_payroll_dispute_owner_actor_id() -> Result<ActorId, (StatusCode, ErrorPayload)> {
    ActorId::parse(LOAD_GATE_PAYROLL_DISPUTE_OWNER_ACTOR_ID).map_err(|error| {
        domain_error(
            StatusCode::INTERNAL_SERVER_ERROR,
            "IDENTITY_MODEL_ERROR",
            format!("failed to parse payroll dispute owner actor id: {error}"),
        )
    })
}

fn load_gate_anomaly_alert_owner_actor_id() -> Result<ActorId, (StatusCode, ErrorPayload)> {
    ActorId::parse(LOAD_GATE_ANOMALY_ALERT_OWNER_ACTOR_ID).map_err(|error| {
        domain_error(
            StatusCode::INTERNAL_SERVER_ERROR,
            "IDENTITY_MODEL_ERROR",
            format!("failed to parse anomaly alert owner actor id: {error}"),
        )
    })
}

fn generate_contract_order_id(state: &AppState) -> Result<OrderId, (StatusCode, ErrorPayload)> {
    let suffix = match &state.compliance_persistence {
        CompliancePersistence::Sql(repository) => tokio::task::block_in_place(|| {
            Handle::current().block_on(allocate_order_id_hex_from_postgres(repository.pool()))
        })
        .map_err(|error| {
            domain_error(
                StatusCode::INTERNAL_SERVER_ERROR,
                "ORDER_ID_GENERATION_FAILED",
                error,
            )
        })?,
        #[cfg(test)]
        CompliancePersistence::InMemoryOnly => {
            let sequence = state
                .next_order_sequence
                .fetch_add(1, AtomicOrdering::Relaxed);
            format!("{sequence:016x}")
        }
    };
    OrderId::parse(format!("ord-{suffix}")).map_err(|error| {
        domain_error(
            StatusCode::INTERNAL_SERVER_ERROR,
            "ORDER_ID_GENERATION_FAILED",
            format!("generated order id is invalid: {error}"),
        )
    })
}

fn resolve_vendor_for_line_items(
    state: &AppState,
    line_items: &[OrderLineItemRequest],
) -> Result<VendorId, (StatusCode, ErrorPayload)> {
    let menu_supply_policy = with_menu_supply_policy(state, |policy| Ok(policy.clone()))?;
    let mut resolved_vendor_id: Option<VendorId> = None;
    for line_item in line_items {
        let menu_item = menu_supply_policy
            .menu_item(line_item.menu_item_id())
            .map_err(|error| {
                domain_error(
                    StatusCode::CONFLICT,
                    "ORDER_POLICY_VIOLATION",
                    error.to_string(),
                )
            })?
            .ok_or_else(|| {
                domain_error(
                    StatusCode::BAD_REQUEST,
                    "INVALID_ORDER_REQUEST",
                    format!(
                        "menuItemId `{}` is unknown for preorder",
                        line_item.menu_item_id().as_str()
                    ),
                )
            })?;

        match resolved_vendor_id.as_ref() {
            Some(existing_vendor_id) if existing_vendor_id != menu_item.vendor_id() => {
                return Err(domain_error(
                    StatusCode::BAD_REQUEST,
                    "INVALID_ORDER_REQUEST",
                    "lineItems must belong to one vendor".to_owned(),
                ));
            }
            Some(_) => {}
            None => resolved_vendor_id = Some(menu_item.vendor_id().clone()),
        }
    }

    resolved_vendor_id.ok_or_else(|| {
        domain_error(
            StatusCode::BAD_REQUEST,
            "INVALID_ORDER_REQUEST",
            "lineItems must include at least one item".to_owned(),
        )
    })
}

fn load_order_snapshot_or_not_found(
    state: &AppState,
    order_id: &OrderId,
) -> Result<OrderSnapshot, (StatusCode, ErrorPayload)> {
    with_menu_supply_policy(state, |menu_supply_policy| {
        menu_supply_policy
            .order_snapshot(order_id)
            .map_err(|error| {
                domain_error(
                    StatusCode::CONFLICT,
                    "ORDER_POLICY_VIOLATION",
                    error.to_string(),
                )
            })?
            .ok_or_else(|| {
                domain_error(
                    StatusCode::NOT_FOUND,
                    "ORDER_NOT_FOUND",
                    format!("order `{}` was not found", order_id.as_str()),
                )
            })
    })
}

fn build_employee_order_payload(
    state: &AppState,
    snapshot: &OrderSnapshot,
) -> Result<EmployeeOrderPayload, (StatusCode, ErrorPayload)> {
    let menu_supply_policy = with_menu_supply_policy(state, |policy| Ok(policy.clone()))?;
    let mut line_items = Vec::with_capacity(snapshot.line_items().len());
    let mut total_minor: u64 = 0;
    let mut order_currency: Option<String> = None;

    for (menu_item_id, quantity) in snapshot.line_items() {
        let menu_item = menu_supply_policy
            .menu_item(menu_item_id)
            .map_err(|error| {
                domain_error(
                    StatusCode::CONFLICT,
                    "ORDER_POLICY_VIOLATION",
                    error.to_string(),
                )
            })?
            .ok_or_else(|| {
                domain_error(
                    StatusCode::CONFLICT,
                    "ORDER_POLICY_VIOLATION",
                    format!(
                        "order `{}` references missing menu item `{}`",
                        snapshot.order_id().as_str(),
                        menu_item_id.as_str()
                    ),
                )
            })?;

        if menu_item.vendor_id() != snapshot.vendor_id() {
            return Err(domain_error(
                StatusCode::CONFLICT,
                "ORDER_POLICY_VIOLATION",
                format!(
                    "order `{}` has vendor mismatch for menu item `{}`",
                    snapshot.order_id().as_str(),
                    menu_item_id.as_str()
                ),
            ));
        }

        let unit_price = menu_item.price();
        match order_currency.as_deref() {
            Some(existing_currency) if existing_currency != unit_price.currency() => {
                return Err(domain_error(
                    StatusCode::CONFLICT,
                    "ORDER_POLICY_VIOLATION",
                    format!(
                        "order `{}` mixes currencies `{existing_currency}` and `{}`",
                        snapshot.order_id().as_str(),
                        unit_price.currency()
                    ),
                ));
            }
            Some(_) => {}
            None => order_currency = Some(unit_price.currency().to_owned()),
        }

        total_minor = total_minor
            .checked_add(u64::from(unit_price.amount_minor()) * u64::from(*quantity))
            .ok_or_else(|| {
                domain_error(
                    StatusCode::CONFLICT,
                    "ORDER_POLICY_VIOLATION",
                    format!(
                        "order `{}` total overflowed supported range",
                        snapshot.order_id().as_str()
                    ),
                )
            })?;

        line_items.push(EmployeeOrderLineItemPayload {
            menu_item_id: menu_item_id.as_str().to_owned(),
            quantity: *quantity,
            price_per_unit: MenuPricePayload {
                currency: unit_price.currency().to_owned(),
                amount_minor: unit_price.amount_minor(),
            },
        });
    }

    let order_currency = order_currency.ok_or_else(|| {
        domain_error(
            StatusCode::CONFLICT,
            "ORDER_POLICY_VIOLATION",
            format!("order `{}` has no line items", snapshot.order_id().as_str()),
        )
    })?;
    let total_minor = u32::try_from(total_minor).map_err(|_| {
        domain_error(
            StatusCode::CONFLICT,
            "ORDER_POLICY_VIOLATION",
            format!(
                "order `{}` total exceeded the maximum supported amount",
                snapshot.order_id().as_str()
            ),
        )
    })?;
    let timeline = snapshot
        .timeline()
        .iter()
        .map(|event| OrderTimelineEventPayload {
            occurred_at: taipei_moment_to_iso_datetime(event.occurred_at()),
            event_type: event.event_type().as_str().to_owned(),
            status: event.state().as_str().to_owned(),
        })
        .collect::<Vec<_>>();
    let created_at = timeline
        .first()
        .map(|event| event.occurred_at.clone())
        .ok_or_else(|| {
            domain_error(
                StatusCode::CONFLICT,
                "ORDER_POLICY_VIOLATION",
                format!("order `{}` has no timeline", snapshot.order_id().as_str()),
            )
        })?;

    Ok(EmployeeOrderPayload {
        order_id: snapshot.order_id().as_str().to_owned(),
        employee_actor_id: snapshot.employee_actor_id().as_str().to_owned(),
        plant_id: state.plant_id.as_str().to_owned(),
        delivery_date: epoch_day_to_iso_date(snapshot.delivery_epoch_day()),
        status: snapshot.state().as_str().to_owned(),
        line_items,
        total: MenuPricePayload {
            currency: order_currency,
            amount_minor: total_minor,
        },
        timeline,
        created_at,
    })
}

fn build_vendor_order_board_entry_payload(
    state: &AppState,
    snapshot: &OrderSnapshot,
) -> Result<VendorOrderBoardEntryPayload, (StatusCode, ErrorPayload)> {
    let employee_payload = build_employee_order_payload(state, snapshot)?;
    Ok(VendorOrderBoardEntryPayload {
        order_id: employee_payload.order_id,
        plant_id: employee_payload.plant_id,
        delivery_date: employee_payload.delivery_date,
        status: employee_payload.status,
        line_items: employee_payload.line_items,
        timeline: employee_payload.timeline,
    })
}

fn compute_order_total_for_payroll(
    state: &AppState,
    snapshot: &OrderSnapshot,
) -> Result<(String, u32), (StatusCode, ErrorPayload)> {
    let menu_supply_policy = with_menu_supply_policy(state, |policy| Ok(policy.clone()))?;
    let mut total_minor: u64 = 0;
    let mut currency: Option<String> = None;
    for (menu_item_id, quantity) in snapshot.line_items() {
        let menu_item = menu_supply_policy
            .menu_item(menu_item_id)
            .map_err(|error| {
                domain_error(
                    StatusCode::CONFLICT,
                    "ORDER_POLICY_VIOLATION",
                    error.to_string(),
                )
            })?
            .ok_or_else(|| {
                domain_error(
                    StatusCode::CONFLICT,
                    "ORDER_POLICY_VIOLATION",
                    format!(
                        "order `{}` references missing menu item `{}`",
                        snapshot.order_id().as_str(),
                        menu_item_id.as_str()
                    ),
                )
            })?;
        let price = menu_item.price();
        match currency.as_deref() {
            Some(existing) if existing != price.currency() => {
                return Err(domain_error(
                    StatusCode::CONFLICT,
                    "ORDER_POLICY_VIOLATION",
                    format!(
                        "order `{}` mixes currencies `{existing}` and `{}`",
                        snapshot.order_id().as_str(),
                        price.currency()
                    ),
                ));
            }
            Some(_) => {}
            None => currency = Some(price.currency().to_owned()),
        }

        total_minor = total_minor
            .checked_add(u64::from(price.amount_minor()) * u64::from(*quantity))
            .ok_or_else(|| {
                domain_error(
                    StatusCode::CONFLICT,
                    "ORDER_POLICY_VIOLATION",
                    format!(
                        "order `{}` total overflowed supported range",
                        snapshot.order_id().as_str()
                    ),
                )
            })?;
    }

    let currency = currency.ok_or_else(|| {
        domain_error(
            StatusCode::CONFLICT,
            "ORDER_POLICY_VIOLATION",
            format!("order `{}` has no line items", snapshot.order_id().as_str()),
        )
    })?;
    let total_minor = u32::try_from(total_minor).map_err(|_| {
        domain_error(
            StatusCode::CONFLICT,
            "ORDER_POLICY_VIOLATION",
            format!(
                "order `{}` total exceeded the maximum supported amount",
                snapshot.order_id().as_str()
            ),
        )
    })?;
    Ok((currency, total_minor))
}

fn expected_payroll_target_amount(snapshot: &OrderSnapshot, gross_total_minor: u32) -> u32 {
    match snapshot.state() {
        OrderLifecycleState::Cancelled
        | OrderLifecycleState::SoldOut
        | OrderLifecycleState::Refunded => 0,
        _ => gross_total_minor,
    }
}

fn sync_payroll_ledger_from_order_snapshot(
    state: &AppState,
    actor: &AuthenticatedActorContext,
    operation_id: &str,
    snapshot: &OrderSnapshot,
    occurred_at: TaipeiBusinessMoment,
) -> Result<(), (StatusCode, ErrorPayload)> {
    let (currency, gross_total_minor) = compute_order_total_for_payroll(state, snapshot)?;
    let source_event = PayrollLedgerSourceRef::new(
        PayrollLedgerSourceKind::OrderMutation,
        format!(
            "order:{}:state:{}",
            snapshot.order_id().as_str(),
            snapshot.state().as_str()
        ),
    )
    .map_err(|error| {
        domain_error(
            StatusCode::INTERNAL_SERVER_ERROR,
            "PAYROLL_LEDGER_INTERNAL_ERROR",
            error.to_string(),
        )
    })?;

    let target_amount_minor = expected_payroll_target_amount(snapshot, gross_total_minor);
    let occurred_at_timestamp = AuditTimestamp::from_taipei_business_moment(
        occurred_at.epoch_day(),
        occurred_at.minute_of_day(),
    )
    .map_err(|error| {
        domain_error(
            StatusCode::INTERNAL_SERVER_ERROR,
            "PAYROLL_LEDGER_INTERNAL_ERROR",
            error.to_string(),
        )
    })?;
    let employee_employment_status =
        if actor.role() == Role::Employee && actor.actor_id() == snapshot.employee_actor_id() {
            actor.employment_status()
        } else {
            EmploymentStatus::Active
        };
    mutate_payroll_ledger_service(state, |service| {
        service.reconcile_order_charge(
            actor,
            operation_id,
            snapshot.order_id(),
            snapshot.employee_actor_id(),
            employee_employment_status,
            snapshot.delivery_epoch_day(),
            &currency,
            target_amount_minor,
            occurred_at_timestamp,
            source_event,
        )
    })?;
    Ok(())
}

fn build_order_state_changed_event(
    actor: &AuthenticatedActorContext,
    operation_id: &str,
    snapshot: &OrderSnapshot,
    occurred_at: TaipeiBusinessMoment,
) -> OrderStateChangedEvent {
    let sequence = ORDER_EVENT_SEQUENCE.fetch_add(1, AtomicOrdering::Relaxed);
    let now_nanos = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|duration| duration.as_nanos())
        .unwrap_or_default();
    OrderStateChangedEvent {
        event_id: format!(
            "evt:{}:{}:{sequence:016x}",
            snapshot.order_id().as_str(),
            now_nanos
        ),
        order_id: snapshot.order_id().as_str().to_owned(),
        vendor_id: snapshot.vendor_id().as_str().to_owned(),
        plant_id: snapshot.plant_id().as_str().to_owned(),
        order_state: snapshot.state().as_str().to_owned(),
        operation_id: operation_id.to_owned(),
        actor_id: actor.actor_id().as_str().to_owned(),
        occurred_at_epoch_millis: taipei_moment_to_epoch_millis(occurred_at),
    }
}

fn build_order_state_changed_outbox_records(
    state: &AppState,
    actor: &AuthenticatedActorContext,
    operation_id: &str,
    snapshot: &OrderSnapshot,
    occurred_at: TaipeiBusinessMoment,
) -> Vec<OutboxEventRecord> {
    let Some(backbone) = state.order_event_backbone.as_ref() else {
        return Vec::new();
    };
    let event = build_order_state_changed_event(actor, operation_id, snapshot, occurred_at);
    vec![OutboxEventRecord {
        event_id: event.event_id.clone(),
        subject: backbone.config().order_subject.clone(),
        payload: order_state_changed_event_payload(&event),
    }]
}

fn order_state_changed_event_payload(event: &OrderStateChangedEvent) -> serde_json::Value {
    serde_json::json!({
        "eventId": event.event_id.as_str(),
        "orderId": event.order_id.as_str(),
        "vendorId": event.vendor_id.as_str(),
        "plantId": event.plant_id.as_str(),
        "orderState": event.order_state.as_str(),
        "operationId": event.operation_id.as_str(),
        "actorId": event.actor_id.as_str(),
        "occurredAtEpochMillis": event.occurred_at_epoch_millis,
    })
}

fn taipei_moment_to_iso_datetime(moment: TaipeiBusinessMoment) -> String {
    let (year, month, day) = civil_from_days(i64::from(moment.epoch_day()));
    let hour = moment.minute_of_day() / 60;
    let minute = moment.minute_of_day() % 60;
    format!("{year:04}-{month:02}-{day:02}T{hour:02}:{minute:02}:00+08:00")
}

fn taipei_moment_to_epoch_millis(moment: TaipeiBusinessMoment) -> i64 {
    const SECONDS_PER_DAY: i64 = 86_400;
    const TAIPEI_OFFSET_SECONDS: i64 = 8 * 60 * 60;
    i64::from(moment.epoch_day())
        .saturating_mul(SECONDS_PER_DAY)
        .saturating_add(i64::from(moment.minute_of_day()) * 60)
        .saturating_sub(TAIPEI_OFFSET_SECONDS)
        .saturating_mul(1000)
}

fn parse_domain_line_items(
    payloads: Vec<OrderLineItemRequestPayload>,
) -> Result<Vec<OrderLineItemRequest>, (StatusCode, ErrorPayload)> {
    payloads
        .into_iter()
        .map(|payload| {
            let menu_item_id = MenuItemId::parse(payload.menu_item_id).map_err(|error| {
                domain_error(
                    StatusCode::BAD_REQUEST,
                    "INVALID_ORDER_REQUEST",
                    format!("menuItemId is invalid: {error}"),
                )
            })?;
            let special_requests = payload
                .special_requests
                .into_iter()
                .map(SpecialRequestOption::into_domain)
                .collect::<Vec<_>>();
            OrderLineItemRequest::new(menu_item_id, payload.quantity, special_requests).map_err(
                |error| {
                    domain_error(
                        StatusCode::BAD_REQUEST,
                        "INVALID_ORDER_REQUEST",
                        format!("line item is invalid: {error}"),
                    )
                },
            )
        })
        .collect::<Result<Vec<_>, _>>()
}

fn parse_order_mutation(
    request: UpdateOrderRequest,
) -> Result<OrderMutation, (StatusCode, ErrorPayload)> {
    match request.operation.as_str() {
        "REPLACE_LINE_ITEMS" => {
            let line_items = request.line_items.ok_or_else(|| {
                domain_error(
                    StatusCode::BAD_REQUEST,
                    "INVALID_ORDER_UPDATE_REQUEST",
                    "lineItems is required for REPLACE_LINE_ITEMS".to_owned(),
                )
            })?;
            let parsed_line_items = parse_domain_line_items(line_items).map_err(|(_, error)| {
                domain_error(
                    StatusCode::BAD_REQUEST,
                    "INVALID_ORDER_UPDATE_REQUEST",
                    error.message,
                )
            })?;
            Ok(OrderMutation::ReplaceLineItems {
                line_items: parsed_line_items,
            })
        }
        "CANCEL" => {
            let cancel_reason = request.cancel_reason.unwrap_or_default();
            if cancel_reason.trim().is_empty() {
                return Err(domain_error(
                    StatusCode::BAD_REQUEST,
                    "INVALID_ORDER_UPDATE_REQUEST",
                    "cancelReason must be non-empty for CANCEL".to_owned(),
                ));
            }
            Ok(OrderMutation::Cancel)
        }
        other => Err(domain_error(
            StatusCode::BAD_REQUEST,
            "INVALID_ORDER_UPDATE_REQUEST",
            format!("unsupported update operation `{other}`"),
        )),
    }
}

fn map_http_order_execution_error(error: HttpOrderExecutionError) -> (StatusCode, ErrorPayload) {
    match error {
        HttpOrderExecutionError::Deliverability(error) => domain_error(
            StatusCode::FORBIDDEN,
            "ORDER_VENDOR_DELIVERY_REJECTED",
            error.to_string(),
        ),
        HttpOrderExecutionError::MenuSupply(error) => domain_error(
            StatusCode::CONFLICT,
            "ORDER_POLICY_VIOLATION",
            error.to_string(),
        ),
        HttpOrderExecutionError::UnsupportedEmployeeMutation { operation } => domain_error(
            StatusCode::BAD_REQUEST,
            "ORDER_MUTATION_NOT_ALLOWED",
            format!("unsupported employee order mutation `{operation}`"),
        ),
    }
}

fn map_order_retention_purge_error(error: MenuSupplyWindowError) -> (StatusCode, ErrorPayload) {
    match error {
        MenuSupplyWindowError::UnauthorizedRole { .. } => {
            domain_error(StatusCode::FORBIDDEN, "FORBIDDEN", error.to_string())
        }
        MenuSupplyWindowError::InvalidOrderRetentionPolicy => {
            domain_error(StatusCode::BAD_REQUEST, "BAD_REQUEST", error.to_string())
        }
        _ => domain_error(
            StatusCode::INTERNAL_SERVER_ERROR,
            "ORDER_RETENTION_PURGE_INTERNAL_ERROR",
            error.to_string(),
        ),
    }
}

fn domain_error(
    status: StatusCode,
    code: &'static str,
    message: String,
) -> (StatusCode, ErrorPayload) {
    (
        status,
        ErrorPayload {
            code,
            message,
            request_id: String::new(),
        },
    )
}

fn spawn_audit_retention_purge_job(
    audit_trail: ImmutableAuditTrail,
    committee_actor: AuthenticatedActorContext,
    interval_seconds: u64,
) {
    tokio::spawn(async move {
        run_audit_retention_purge_once(&audit_trail, &committee_actor);
        let mut interval = time::interval(std::time::Duration::from_secs(interval_seconds));
        interval.set_missed_tick_behavior(MissedTickBehavior::Skip);
        loop {
            interval.tick().await;
            run_audit_retention_purge_once(&audit_trail, &committee_actor);
        }
    });
}

fn run_audit_retention_purge_once(
    audit_trail: &ImmutableAuditTrail,
    committee_actor: &AuthenticatedActorContext,
) {
    let as_of = match AuditTimestamp::now_taipei() {
        Ok(value) => value,
        Err(error) => {
            tracing::error!(error = %error, "audit retention purge skipped: failed to resolve Taipei time");
            return;
        }
    };
    match audit_trail.purge_expired_evidence(committee_actor, as_of) {
        Ok(report) => tracing::info!(
            purged_events = report.purged_events,
            as_of_epoch_day = as_of.epoch_day(),
            as_of_minute_of_day = as_of.minute_of_day(),
            "audit retention purge job completed"
        ),
        Err(error) => tracing::error!(error = %error, "audit retention purge job failed"),
    }
}

fn spawn_payroll_retention_purge_job(
    state: AppState,
    committee_actor: AuthenticatedActorContext,
    interval_seconds: u64,
) {
    tokio::spawn(async move {
        run_payroll_retention_purge_once(&state, &committee_actor);
        let mut interval = time::interval(std::time::Duration::from_secs(interval_seconds));
        interval.set_missed_tick_behavior(MissedTickBehavior::Skip);
        loop {
            interval.tick().await;
            run_payroll_retention_purge_once(&state, &committee_actor);
        }
    });
}

fn run_payroll_retention_purge_once(state: &AppState, committee_actor: &AuthenticatedActorContext) {
    let as_of = match AuditTimestamp::now_taipei() {
        Ok(value) => value,
        Err(error) => {
            tracing::error!(
                error = %error,
                "payroll retention purge skipped: failed to resolve Taipei time"
            );
            return;
        }
    };
    match mutate_payroll_ledger_service(state, |service| {
        service.purge_expired_data(committee_actor, as_of)
    }) {
        Ok(report) => tracing::info!(
            purged_ledger_entries = report.purged_ledger_entries,
            purged_disputes = report.purged_disputes,
            purged_exchange_batches = report.purged_exchange_batches,
            as_of_epoch_day = as_of.epoch_day(),
            as_of_minute_of_day = as_of.minute_of_day(),
            "payroll retention purge job completed"
        ),
        Err((_, error)) => tracing::error!(
            error_code = error.code,
            reason = %error.message,
            "payroll retention purge job failed"
        ),
    }
}

fn spawn_order_retention_purge_job(
    state: AppState,
    committee_actor: AuthenticatedActorContext,
    interval_seconds: u64,
) {
    tokio::spawn(async move {
        run_order_retention_purge_once(&state, &committee_actor);
        let mut interval = time::interval(std::time::Duration::from_secs(interval_seconds));
        interval.set_missed_tick_behavior(MissedTickBehavior::Skip);
        loop {
            interval.tick().await;
            run_order_retention_purge_once(&state, &committee_actor);
        }
    });
}

fn run_order_retention_purge_once(state: &AppState, committee_actor: &AuthenticatedActorContext) {
    let as_of = match AuditTimestamp::now_taipei() {
        Ok(value) => value,
        Err(error) => {
            tracing::error!(
                error = %error,
                "order retention purge skipped: failed to resolve Taipei time"
            );
            return;
        }
    };
    match mutate_menu_supply_policy(
        state,
        |policy| policy.purge_expired_orders(committee_actor, as_of),
        map_order_retention_purge_error,
        "ORDER_RETENTION_PURGE_INTERNAL_ERROR",
    ) {
        Ok(report) => tracing::info!(
            purged_orders = report.purged_orders,
            as_of_epoch_day = as_of.epoch_day(),
            as_of_minute_of_day = as_of.minute_of_day(),
            "order retention purge job completed"
        ),
        Err((_, error)) => tracing::error!(
            error_code = error.code,
            reason = %error.message,
            "order retention purge job failed"
        ),
    }
}

async fn query_audit_investigations(
    State(state): State<AppState>,
    headers: HeaderMap,
    Query(query): Query<AuditInvestigationQuery>,
) -> (StatusCode, Json<serde_json::Value>) {
    let telemetry =
        TelemetryService::HttpApi.begin_operation("queryAuditInvestigations", None::<&str>, None);
    let request_id = telemetry.correlation_context().request_id().to_owned();
    let investigator = match require_corporate_actor_for_role(&headers, Role::CommitteeAdmin) {
        Ok(actor) => actor,
        Err((status, error)) => {
            telemetry.finish_with_http_status(status.as_u16());
            return (
                status,
                Json(
                    serde_json::to_value(error.with_request_id(request_id.as_str()))
                        .expect("authorization error payload serialization should succeed"),
                ),
            );
        }
    };

    let response = match handle_query_audit_investigations(&state, &investigator, query) {
        Ok(payload) => {
            telemetry.finish_with_http_status(StatusCode::OK.as_u16());
            (
                StatusCode::OK,
                Json(
                    serde_json::to_value(payload)
                        .expect("audit investigation payload serialization should succeed"),
                ),
            )
        }
        Err((status, error)) => {
            telemetry.finish_with_http_status(status.as_u16());
            (
                status,
                Json(
                    serde_json::to_value(error.with_request_id(request_id.as_str()))
                        .expect("audit investigation error payload serialization should succeed"),
                ),
            )
        }
    };

    response
}

fn handle_query_audit_investigations(
    state: &AppState,
    investigator: &AuthenticatedActorContext,
    query: AuditInvestigationQuery,
) -> Result<AuditInvestigationResponse, (StatusCode, ErrorPayload)> {
    let filter = build_audit_investigation_filter(query)?;
    let gateway = HttpAuditInvestigationExecutionGateway::new(state.audit_trail.clone());
    let evidences = gateway
        .execute_investigation_query(investigator, &filter)
        .map_err(|error| map_audit_trail_error(error, "AUDIT_INVESTIGATION_INTERNAL_ERROR"))?;
    Ok(AuditInvestigationResponse {
        items: evidences
            .iter()
            .map(to_audit_evidence_payload)
            .collect::<Vec<_>>(),
    })
}

async fn query_audit_responsibilities(
    State(state): State<AppState>,
    headers: HeaderMap,
    Query(query): Query<AuditInvestigationQuery>,
) -> (StatusCode, Json<serde_json::Value>) {
    let telemetry =
        TelemetryService::HttpApi.begin_operation("queryAuditResponsibilities", None::<&str>, None);
    let request_id = telemetry.correlation_context().request_id().to_owned();
    let investigator = match require_corporate_actor_for_role(&headers, Role::CommitteeAdmin) {
        Ok(actor) => actor,
        Err((status, error)) => {
            telemetry.finish_with_http_status(status.as_u16());
            return (
                status,
                Json(
                    serde_json::to_value(error.with_request_id(request_id.as_str()))
                        .expect("authorization error payload serialization should succeed"),
                ),
            );
        }
    };

    let response = match handle_query_audit_responsibilities(&state, &investigator, query) {
        Ok(payload) => {
            telemetry.finish_with_http_status(StatusCode::OK.as_u16());
            (
                StatusCode::OK,
                Json(
                    serde_json::to_value(payload)
                        .expect("audit responsibilities payload serialization should succeed"),
                ),
            )
        }
        Err((status, error)) => {
            telemetry.finish_with_http_status(status.as_u16());
            (
                status,
                Json(
                    serde_json::to_value(error.with_request_id(request_id.as_str())).expect(
                        "audit responsibilities error payload serialization should succeed",
                    ),
                ),
            )
        }
    };

    response
}

fn handle_query_audit_responsibilities(
    state: &AppState,
    investigator: &AuthenticatedActorContext,
    query: AuditInvestigationQuery,
) -> Result<AuditResponsibilityResponse, (StatusCode, ErrorPayload)> {
    let filter = build_audit_investigation_filter(query)?;
    let gateway = HttpAuditInvestigationExecutionGateway::new(state.audit_trail.clone());
    let attributions = gateway
        .execute_responsibility_query(investigator, &filter)
        .map_err(|error| map_audit_trail_error(error, "AUDIT_INVESTIGATION_INTERNAL_ERROR"))?;
    Ok(AuditResponsibilityResponse {
        items: attributions
            .iter()
            .map(to_audit_responsibility_payload)
            .collect::<Vec<_>>(),
    })
}

async fn purge_audit_evidence(
    State(state): State<AppState>,
    headers: HeaderMap,
    Json(request): Json<AuditRetentionPurgeRequest>,
) -> (StatusCode, Json<serde_json::Value>) {
    let telemetry =
        TelemetryService::HttpApi.begin_operation("purgeAuditEvidence", None::<&str>, None::<&str>);
    let request_id = telemetry.correlation_context().request_id().to_owned();
    let investigator = match require_corporate_actor_for_role(&headers, Role::CommitteeAdmin) {
        Ok(actor) => actor,
        Err((status, error)) => {
            telemetry.finish_with_http_status(status.as_u16());
            return (
                status,
                Json(
                    serde_json::to_value(error.with_request_id(request_id.as_str()))
                        .expect("authorization error payload serialization should succeed"),
                ),
            );
        }
    };

    let response = match handle_purge_audit_evidence(&state, &investigator, request) {
        Ok(payload) => {
            telemetry.finish_with_http_status(StatusCode::OK.as_u16());
            (
                StatusCode::OK,
                Json(
                    serde_json::to_value(payload)
                        .expect("audit purge payload serialization should succeed"),
                ),
            )
        }
        Err((status, error)) => {
            telemetry.finish_with_http_status(status.as_u16());
            (
                status,
                Json(
                    serde_json::to_value(error.with_request_id(request_id.as_str()))
                        .expect("audit purge error payload serialization should succeed"),
                ),
            )
        }
    };

    response
}

fn handle_purge_audit_evidence(
    state: &AppState,
    investigator: &AuthenticatedActorContext,
    request: AuditRetentionPurgeRequest,
) -> Result<AuditRetentionPurgeResponse, (StatusCode, ErrorPayload)> {
    let as_of = match request.as_of_epoch_day {
        Some(epoch_day) => AuditTimestamp::from_epoch_day(epoch_day),
        None => AuditTimestamp::now_taipei().map_err(|error| {
            domain_error(
                StatusCode::INTERNAL_SERVER_ERROR,
                "AUDIT_RETENTION_PURGE_INTERNAL_ERROR",
                error.to_string(),
            )
        })?,
    };
    let gateway = HttpAuditInvestigationExecutionGateway::new(state.audit_trail.clone());
    let report = gateway
        .execute_retention_purge(investigator, as_of)
        .map_err(|error| map_audit_trail_error(error, "AUDIT_RETENTION_PURGE_INTERNAL_ERROR"))?;
    Ok(AuditRetentionPurgeResponse {
        purged_events: report.purged_events,
        as_of_epoch_day: as_of.epoch_day(),
    })
}

fn build_audit_investigation_filter(
    query: AuditInvestigationQuery,
) -> Result<AuditInvestigationFilter, (StatusCode, ErrorPayload)> {
    let mut filter = AuditInvestigationFilter::default();
    if let Some(actor_id) = query.actor_id {
        filter = filter.with_actor_id(ActorId::parse(actor_id).map_err(|error| {
            domain_error(
                StatusCode::BAD_REQUEST,
                "INVALID_AUDIT_INVESTIGATION_QUERY",
                format!("actorId is invalid: {error}"),
            )
        })?);
    }
    if let Some(action) = query.action {
        let action = parse_audit_action_filter(&action).ok_or_else(|| {
            domain_error(
                StatusCode::BAD_REQUEST,
                "INVALID_AUDIT_INVESTIGATION_QUERY",
                format!("action `{action}` is not supported"),
            )
        })?;
        filter = filter.with_action(action);
    }
    match (query.entity_type, query.entity_id) {
        (Some(entity_type), Some(entity_id)) => {
            let entity_type = parse_audit_entity_type_filter(&entity_type).ok_or_else(|| {
                domain_error(
                    StatusCode::BAD_REQUEST,
                    "INVALID_AUDIT_INVESTIGATION_QUERY",
                    format!("entityType `{entity_type}` is not supported"),
                )
            })?;
            filter = filter
                .with_entity(entity_type, entity_id)
                .map_err(|error| {
                    domain_error(
                        StatusCode::BAD_REQUEST,
                        "INVALID_AUDIT_INVESTIGATION_QUERY",
                        error.to_string(),
                    )
                })?;
        }
        (Some(_), None) | (None, Some(_)) => {
            return Err(domain_error(
                StatusCode::BAD_REQUEST,
                "INVALID_AUDIT_INVESTIGATION_QUERY",
                "entityType and entityId must be provided together".to_owned(),
            ));
        }
        (None, None) => {}
    }
    let occurred_from = query
        .occurred_from_epoch_day
        .map(AuditTimestamp::from_epoch_day);
    let occurred_to = query
        .occurred_to_epoch_day
        .map(AuditTimestamp::through_epoch_day);
    if let (Some(from), Some(to)) = (occurred_from, occurred_to) {
        if from > to {
            return Err(domain_error(
                StatusCode::BAD_REQUEST,
                "INVALID_AUDIT_INVESTIGATION_QUERY",
                "occurredFromEpochDay must be less than or equal to occurredToEpochDay".to_owned(),
            ));
        }
    }
    filter = filter.with_time_range(occurred_from, occurred_to);
    if let Some(correlation_id) = query.correlation_id {
        filter = filter.with_correlation_id(AuditCorrelationId::parse(correlation_id).map_err(
            |error| {
                domain_error(
                    StatusCode::BAD_REQUEST,
                    "INVALID_AUDIT_INVESTIGATION_QUERY",
                    error.to_string(),
                )
            },
        )?);
    }
    Ok(filter)
}

fn parse_audit_action_filter(value: &str) -> Option<AuditAction> {
    let normalized = value.trim().to_ascii_uppercase();
    ALL_AUDIT_ACTIONS
        .iter()
        .copied()
        .find(|action| action.as_str() == normalized)
}

fn parse_audit_entity_type_filter(value: &str) -> Option<AuditEntityType> {
    let normalized = value.trim().to_ascii_uppercase();
    ALL_AUDIT_ENTITY_TYPES
        .iter()
        .copied()
        .find(|entity_type| entity_type.as_str() == normalized)
}

fn to_audit_evidence_payload(evidence: &ImmutableAuditEvidence) -> AuditEvidencePayload {
    AuditEvidencePayload {
        evidence_id: evidence.evidence_id(),
        occurred_at: audit_timestamp_to_iso_datetime(evidence.occurred_at()),
        actor_id: evidence.audit_identity().actor_id().as_str().to_owned(),
        actor_role: role_to_api_label(evidence.audit_identity().role()).to_owned(),
        authentication_source: authentication_source_to_api_label(
            evidence.audit_identity().authentication_source(),
        )
        .to_owned(),
        operation_id: evidence.audit_identity().operation_id().to_owned(),
        action: evidence.action().as_str().to_owned(),
        entity_type: evidence.entity().entity_type().as_str().to_owned(),
        entity_id: evidence.entity().entity_id().to_owned(),
        reason: evidence.reason().to_owned(),
        correlation_id: evidence.correlation_id().as_str().to_owned(),
    }
}

fn to_audit_responsibility_payload(
    attribution: &ResponsibilityAttribution,
) -> AuditResponsibilityPayload {
    AuditResponsibilityPayload {
        actor_id: attribution.actor_id().as_str().to_owned(),
        role: role_to_api_label(attribution.role()).to_owned(),
        authentication_source: authentication_source_to_api_label(
            attribution.authentication_source(),
        )
        .to_owned(),
        event_count: attribution.event_count(),
        actions: attribution
            .actions()
            .iter()
            .map(|action| action.as_str().to_owned())
            .collect(),
        entities: attribution
            .entities()
            .iter()
            .map(|entity| AuditEntityRefPayload {
                entity_type: entity.entity_type().as_str().to_owned(),
                entity_id: entity.entity_id().to_owned(),
            })
            .collect(),
    }
}

fn role_to_api_label(role: Role) -> &'static str {
    match role {
        Role::Employee => "EMPLOYEE",
        Role::VendorOperator => "VENDOR_OPERATOR",
        Role::CommitteeAdmin => "COMMITTEE_ADMIN",
        Role::PayrollOperator => "PAYROLL_OPERATOR",
    }
}

fn authentication_source_to_api_label(source: AuthenticationSource) -> &'static str {
    match source {
        AuthenticationSource::CorporateSso => "CORPORATE_SSO",
        AuthenticationSource::VendorAccountMfa => "VENDOR_ACCOUNT_MFA",
        AuthenticationSource::OAuthServiceAccount => "OAUTH_SERVICE_ACCOUNT",
    }
}

fn audit_timestamp_to_iso_datetime(timestamp: AuditTimestamp) -> String {
    let (year, month, day) = civil_from_days(i64::from(timestamp.epoch_day()));
    let hour = timestamp.minute_of_day() / 60;
    let minute = timestamp.minute_of_day() % 60;
    format!("{year:04}-{month:02}-{day:02}T{hour:02}:{minute:02}:00+08:00")
}

fn map_audit_trail_error(
    error: AuditTrailError,
    internal_error_code: &'static str,
) -> (StatusCode, ErrorPayload) {
    match error {
        AuditTrailError::UnauthorizedInvestigatorRole { .. } => {
            domain_error(StatusCode::FORBIDDEN, "FORBIDDEN", error.to_string())
        }
        AuditTrailError::InvalidMinuteOfDay { .. }
        | AuditTrailError::InvalidEntityId
        | AuditTrailError::InvalidReason(_)
        | AuditTrailError::InvalidCorrelationId
        | AuditTrailError::InvalidRetentionPolicy => domain_error(
            StatusCode::BAD_REQUEST,
            "INVALID_AUDIT_INVESTIGATION_QUERY",
            error.to_string(),
        ),
        _ => domain_error(
            StatusCode::INTERNAL_SERVER_ERROR,
            internal_error_code,
            error.to_string(),
        ),
    }
}

async fn verify_order_pickup(
    State(state): State<AppState>,
    Path(order_id): Path<String>,
    Json(request): Json<PickupVerificationRequest>,
) -> (StatusCode, Json<serde_json::Value>) {
    let telemetry = TelemetryService::HttpApi.begin_operation(
        "verifyPickupOrder",
        Some("load-gate"),
        Some(state.plant_id.as_str()),
    );
    let request_id = telemetry.correlation_context().request_id().to_owned();
    if request.verification_code.trim().is_empty() {
        emit_pickup_verification_audit_event(
            request_id.as_str(),
            Some(order_id.as_str()),
            "rejected",
            "invalid-format",
            None,
            None,
        );
        telemetry.finish_with_http_status(StatusCode::BAD_REQUEST.as_u16());
        return (
            StatusCode::BAD_REQUEST,
            Json(
                serde_json::to_value(
                    domain_error(
                        StatusCode::BAD_REQUEST,
                        "INVALID_PICKUP_VERIFICATION_REQUEST",
                        "verificationCode must be non-empty".to_owned(),
                    )
                    .1
                    .with_request_id(request_id.as_str()),
                )
                .expect("error payload serialization should succeed"),
            ),
        );
    }

    let response = match handle_verify_order_pickup(&state, order_id, request, request_id.as_str())
    {
        Ok(payload) => {
            telemetry.finish_with_http_status(StatusCode::OK.as_u16());
            (
                StatusCode::OK,
                Json(
                    serde_json::to_value(payload)
                        .expect("pickup verification payload serialization should succeed"),
                ),
            )
        }
        Err((status, error)) => {
            telemetry.finish_with_http_status(status.as_u16());
            (
                status,
                Json(
                    serde_json::to_value(error.with_request_id(request_id.as_str()))
                        .expect("error payload serialization should succeed"),
                ),
            )
        }
    };

    response
}

fn handle_verify_order_pickup(
    state: &AppState,
    order_id_raw: String,
    request: PickupVerificationRequest,
    request_id: &str,
) -> Result<PickupVerificationResponse, (StatusCode, ErrorPayload)> {
    let parsed_order_id = parse_contract_order_id(&order_id_raw).map_err(|error| {
        emit_pickup_verification_audit_event(
            request_id,
            Some(order_id_raw.as_str()),
            "rejected",
            "invalid-order-id",
            None,
            None,
        );
        domain_error(
            StatusCode::BAD_REQUEST,
            "INVALID_PICKUP_VERIFICATION_REQUEST",
            format!("orderId path parameter is invalid: {error}"),
        )
    })?;
    let snapshot = load_order_snapshot_or_not_found(state, &parsed_order_id)?;
    let employee_actor = load_gate_employee_actor_for_plant(state, snapshot.plant_id())?;
    handle_verify_order_pickup_for_actor(state, &employee_actor, order_id_raw, request, request_id)
}

fn handle_verify_order_pickup_for_actor(
    state: &AppState,
    actor: &AuthenticatedActorContext,
    order_id_raw: String,
    request: PickupVerificationRequest,
    request_id: &str,
) -> Result<PickupVerificationResponse, (StatusCode, ErrorPayload)> {
    let verification_code = request.verification_code.trim();
    if verification_code.is_empty() {
        emit_pickup_verification_audit_event(
            request_id,
            None,
            "rejected",
            "invalid-format",
            None,
            None,
        );
        return Err(domain_error(
            StatusCode::BAD_REQUEST,
            "INVALID_PICKUP_VERIFICATION_REQUEST",
            "verificationCode must be non-empty".to_owned(),
        ));
    }

    let order_id = parse_contract_order_id(&order_id_raw).map_err(|error| {
        emit_pickup_verification_audit_event(
            request_id,
            Some(order_id_raw.as_str()),
            "rejected",
            "invalid-order-id",
            None,
            None,
        );
        domain_error(
            StatusCode::BAD_REQUEST,
            "INVALID_PICKUP_VERIFICATION_REQUEST",
            format!("orderId path parameter is invalid: {error}"),
        )
    })?;

    let snapshot = load_order_snapshot_or_not_found(state, &order_id)?;
    if snapshot.state() == OrderLifecycleState::Fulfilled {
        emit_pickup_verification_audit_event(
            request_id,
            Some(order_id.as_str()),
            "rejected",
            "replay-detected",
            None,
            None,
        );
        return Err(domain_error(
            StatusCode::CONFLICT,
            "PICKUP_VERIFICATION_REPLAYED",
            format!(
                "order `{}` has already been claimed via pickup verification",
                order_id.as_str()
            ),
        ));
    }
    if !matches!(
        snapshot.state(),
        OrderLifecycleState::Pending | OrderLifecycleState::Modified
    ) {
        emit_pickup_verification_audit_event(
            request_id,
            Some(order_id.as_str()),
            "rejected",
            "order-state-not-eligible",
            None,
            None,
        );
        return Err(domain_error(
            StatusCode::CONFLICT,
            "PICKUP_VERIFICATION_STATE_CONFLICT",
            format!(
                "order `{}` is in `{}` state and cannot be pickup-verified",
                order_id.as_str(),
                snapshot.state().as_str()
            ),
        ));
    }

    let current_step = PickupTotpVerifier::current_taipei_step().map_err(|error| {
        domain_error(
            StatusCode::INTERNAL_SERVER_ERROR,
            "TIME_RESOLUTION_FAILED",
            format!("failed to resolve pickup TOTP step: {error}"),
        )
    })?;
    let VerifiedTotp {
        step: verified_step,
    } = state
        .pickup_totp_verifier
        .verify(&order_id, verification_code, current_step)
        .map_err(|error| {
            map_pickup_totp_verification_error(&order_id, request_id, error, Some(current_step))
        })?;

    let requested_at = current_taipei_business_moment().map_err(|error| {
        domain_error(
            StatusCode::INTERNAL_SERVER_ERROR,
            "TIME_RESOLUTION_FAILED",
            error,
        )
    })?;

    let _updated_snapshot = mutate_menu_supply_policy_with_outbox(
        state,
        |menu_supply_policy| {
            menu_supply_policy.update_order(
                actor,
                &order_id,
                OrderMutation::MarkFulfilled,
                requested_at,
            )?;
            let updated_snapshot = menu_supply_policy
                .order_snapshot(&order_id)?
                .ok_or_else(|| MenuSupplyWindowError::OrderNotFound(order_id.clone()))?;
            let outbox_events = build_order_state_changed_outbox_records(
                state,
                actor,
                "verifyPickupOrder",
                &updated_snapshot,
                requested_at,
            );
            Ok((updated_snapshot, outbox_events))
        },
        |error| map_pickup_claim_update_error(&order_id, request_id, error),
        "ORDER_POLICY_VIOLATION",
    )?;

    emit_pickup_verification_audit_event(
        request_id,
        Some(order_id.as_str()),
        "accepted",
        "verified-and-claimed",
        Some(verified_step),
        Some(current_step),
    );

    Ok(PickupVerificationResponse {
        order_id: order_id.as_str().to_owned(),
        verified: true,
    })
}

fn map_pickup_totp_verification_error(
    order_id: &OrderId,
    request_id: &str,
    error: PickupTotpVerificationError,
    current_step: Option<u64>,
) -> (StatusCode, ErrorPayload) {
    emit_pickup_verification_audit_event(
        request_id,
        Some(order_id.as_str()),
        "rejected",
        error.as_audit_reason(),
        None,
        current_step,
    );
    match error {
        PickupTotpVerificationError::InvalidFormat(reason) => domain_error(
            StatusCode::BAD_REQUEST,
            "INVALID_PICKUP_VERIFICATION_REQUEST",
            reason.to_owned(),
        ),
        PickupTotpVerificationError::Expired {
            token_step,
            current_step,
        } => domain_error(
            StatusCode::BAD_REQUEST,
            "PICKUP_VERIFICATION_EXPIRED",
            format!("verificationCode step {token_step} is expired at current step {current_step}"),
        ),
        PickupTotpVerificationError::NotYetValid {
            token_step,
            current_step,
        } => domain_error(
            StatusCode::BAD_REQUEST,
            "PICKUP_VERIFICATION_INVALID_WINDOW",
            format!(
                "verificationCode step {token_step} is not yet valid at current step {current_step}"
            ),
        ),
        PickupTotpVerificationError::InvalidCode => domain_error(
            StatusCode::BAD_REQUEST,
            "PICKUP_VERIFICATION_INVALID_CODE",
            "verificationCode does not match the expected pickup TOTP".to_owned(),
        ),
    }
}

fn map_pickup_claim_update_error(
    order_id: &OrderId,
    request_id: &str,
    error: MenuSupplyWindowError,
) -> (StatusCode, ErrorPayload) {
    match error {
        MenuSupplyWindowError::InvalidOrderLifecycleTransition { current_state, .. }
            if current_state == OrderLifecycleState::Fulfilled =>
        {
            emit_pickup_verification_audit_event(
                request_id,
                Some(order_id.as_str()),
                "rejected",
                "replay-detected",
                None,
                None,
            );
            domain_error(
                StatusCode::CONFLICT,
                "PICKUP_VERIFICATION_REPLAYED",
                format!(
                    "order `{}` has already been claimed via pickup verification",
                    order_id.as_str()
                ),
            )
        }
        MenuSupplyWindowError::InvalidOrderLifecycleTransition { current_state, .. } => {
            emit_pickup_verification_audit_event(
                request_id,
                Some(order_id.as_str()),
                "rejected",
                "order-state-not-eligible",
                None,
                None,
            );
            domain_error(
                StatusCode::CONFLICT,
                "PICKUP_VERIFICATION_STATE_CONFLICT",
                format!(
                    "order `{}` is in `{}` state and cannot be pickup-verified",
                    order_id.as_str(),
                    current_state.as_str()
                ),
            )
        }
        other => {
            emit_pickup_verification_audit_event(
                request_id,
                Some(order_id.as_str()),
                "rejected",
                "claim-update-failed",
                None,
                None,
            );
            domain_error(
                StatusCode::CONFLICT,
                "ORDER_POLICY_VIOLATION",
                other.to_string(),
            )
        }
    }
}

fn emit_pickup_verification_audit_event(
    request_id: &str,
    order_id: Option<&str>,
    outcome: &'static str,
    reason: &'static str,
    token_step: Option<u64>,
    current_step: Option<u64>,
) {
    tracing::info!(
        audit_event = "pickup_totp_checkin",
        verification_mode = "totp_qr",
        request_id = request_id,
        order_id = order_id.unwrap_or("n/a"),
        outcome = outcome,
        reason = reason,
        token_step = token_step,
        current_step = current_step,
        "pickup TOTP verification audit event"
    );
}

#[cfg(test)]
mod tests {
    use super::*;
    use corporate_catering_system::audit::{AuditEntityRef, AuditEvidenceWrite, AuditIdentityLink};
    use corporate_catering_system::payroll::PayrollDisputeStatus;
    use corporate_catering_system::rush_reminder::{
        RushReminderDeliveryError, RushReminderPreferences, RushReminderScenario,
    };
    use corporate_catering_system::vendor_compliance::ComplianceHistoryKind;
    use corporate_catering_system::vendor_delivery_mapping::VendorPlantDeliveryError;

    fn actor_id(value: &str) -> ActorId {
        ActorId::parse(value).expect("actor id should be valid")
    }

    fn plant_id(value: &str) -> PlantId {
        PlantId::parse(value).expect("plant id should be valid")
    }

    fn vendor_id(value: &str) -> VendorId {
        VendorId::parse(value).expect("vendor id should be valid")
    }

    fn menu_item_id(value: &str) -> MenuItemId {
        MenuItemId::parse(value).expect("menu item id should be valid")
    }

    fn order_id(value: &str) -> OrderId {
        OrderId::parse(value).expect("order id should be valid")
    }

    fn taipei_moment(epoch_day: i32, minute_of_day: u16) -> TaipeiBusinessMoment {
        TaipeiBusinessMoment::new(epoch_day, minute_of_day).expect("Taipei moment should be valid")
    }

    fn previous_pay_period_for_epoch_day(epoch_day: i32) -> String {
        let (year, month, _) = civil_from_days(i64::from(epoch_day));
        let (previous_year, previous_month) = if month == 1 {
            (year.saturating_sub(1), 12)
        } else {
            (year, month - 1)
        };
        format!("{previous_year:04}-{previous_month:02}")
    }

    fn committee_admin() -> AuthenticatedActorContext {
        AuthenticatedActorContext::new(
            actor_id("committee-discovery-test"),
            Role::CommitteeAdmin,
            PlantScope::all(),
            AuthenticationSource::CorporateSso,
        )
        .expect("committee actor should be valid")
    }

    fn payroll_operator() -> AuthenticatedActorContext {
        AuthenticatedActorContext::new(
            actor_id("payroll-discovery-test"),
            Role::PayrollOperator,
            PlantScope::all(),
            AuthenticationSource::CorporateSso,
        )
        .expect("payroll actor should be valid")
    }

    fn vendor_operator() -> AuthenticatedActorContext {
        AuthenticatedActorContext::new(
            actor_id("vendor-discovery-test"),
            Role::VendorOperator,
            PlantScope::restricted(vec![plant_id("fab-a")]).expect("scope should be valid"),
            AuthenticationSource::VendorAccountMfa,
        )
        .expect("vendor actor should be valid")
    }

    fn employee_actor() -> AuthenticatedActorContext {
        AuthenticatedActorContext::new(
            actor_id("employee-discovery-test"),
            Role::Employee,
            PlantScope::restricted(vec![plant_id("fab-a")]).expect("scope should be valid"),
            AuthenticationSource::CorporateSso,
        )
        .expect("employee actor should be valid")
    }

    fn oauth_service_account_actor(
        actor_id_value: &str,
        role: Role,
        plant_scope: PlantScope,
    ) -> AuthenticatedActorContext {
        AuthenticatedActorContext::new(
            actor_id(actor_id_value),
            role,
            plant_scope,
            AuthenticationSource::OAuthServiceAccount,
        )
        .expect("oauth service-account actor should be valid")
    }

    fn invoke_mcp_write_for_test(
        state: &AppState,
        grant: &McpServiceAccountGrant,
        tool_name: &str,
        args: serde_json::Value,
    ) -> Result<serde_json::Value, (StatusCode, ErrorPayload)> {
        let auth_gateway = McpAuthorizationGateway::new(AccessController::with_default_policy());
        invoke_mcp_write_tool(
            state,
            &auth_gateway,
            grant,
            tool_name,
            args,
            None,
            10_000,
            "mcp-test-request",
        )
    }

    fn payroll_export_field_encryptor() -> PayrollExportFieldEncryptor {
        PayrollExportFieldEncryptor::parse_hex(
            "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff",
        )
        .expect("test payroll export encryption key should parse")
    }

    fn test_object_storage_upload_pipeline() -> Arc<ObjectStorageUploadPipeline> {
        let config = S3ObjectStorageConfig::new(
            "http://127.0.0.1:9000",
            "us-east-1",
            "test-access-key",
            "test-secret-key",
            "menu-assets",
            "compliance-evidence",
            "fulfillment-exports",
        )
        .expect("test object storage config should be valid")
        .with_ttls(900, 600)
        .expect("test object storage ttl should be valid")
        .with_key_namespace("corporate-catering-tests");
        Arc::new(
            ObjectStorageUploadPipeline::new(config)
                .expect("test object storage pipeline should initialize"),
        )
    }

    fn bearer_headers(actor_id: &str, role: &str) -> HeaderMap {
        let mut headers = HeaderMap::new();
        headers.insert(
            AUTHORIZATION,
            axum::http::HeaderValue::from_str(
                format!("{AUTHORIZATION_BEARER_PREFIX}{actor_id}|{role}").as_str(),
            )
            .expect("authorization header should be valid"),
        );
        headers
    }

    fn ensure_test_mcp_oauth_env() {
        std::env::set_var(
            MCP_OAUTH_SERVICE_ACCOUNT_ISSUER_ENV,
            "https://issuer.catering-mcp.test",
        );
        std::env::set_var(
            MCP_OAUTH_SERVICE_ACCOUNT_AUDIENCE_ENV,
            "corporate-catering-mcp-runtime",
        );
        std::env::set_var(
            MCP_OAUTH_SERVICE_ACCOUNT_HS256_SECRET_BASE64_ENV,
            BASE64_STANDARD.encode("mcp-oauth-test-signing-secret-32-bytes".as_bytes()),
        );
        std::env::set_var(
            MCP_BRIDGE_KEY_REGISTRY_JSON_ENV,
            r#"[{
                "keyId": "bridge-runtime-test",
                "issuedAtEpochSeconds": 1000,
                "expiresAtEpochSeconds": 1600,
                "rotatedAtEpochSeconds": 1550,
                "allowedServiceAccountIds": ["svc-mcp-runtime-auth-test"]
            }]"#,
        );
    }

    fn load_test_mcp_oauth_secret() -> Vec<u8> {
        let encoded = std::env::var(MCP_OAUTH_SERVICE_ACCOUNT_HS256_SECRET_BASE64_ENV)
            .expect("test MCP OAuth secret env should be configured");
        BASE64_STANDARD
            .decode(encoded.as_bytes())
            .expect("test MCP OAuth secret should decode")
    }

    fn build_test_mcp_oauth_jwt_token(claims: serde_json::Value) -> String {
        let header_json = serde_json::json!({
            "alg": "HS256",
            "typ": "JWT",
        });
        let header_segment = BASE64_URL_SAFE_NO_PAD.encode(
            serde_json::to_vec(&header_json).expect("jwt header serialization should succeed"),
        );
        let payload_segment = BASE64_URL_SAFE_NO_PAD
            .encode(serde_json::to_vec(&claims).expect("jwt payload serialization should succeed"));
        let signing_input = format!("{header_segment}.{payload_segment}");
        type HmacSha256 = Hmac<Sha256>;
        let mut mac = <HmacSha256 as Mac>::new_from_slice(load_test_mcp_oauth_secret().as_slice())
            .expect("test signing key length should be valid");
        mac.update(signing_input.as_bytes());
        let signature = mac.finalize().into_bytes();
        let signature_segment = BASE64_URL_SAFE_NO_PAD.encode(signature);
        format!("{signing_input}.{signature_segment}")
    }

    fn mcp_oauth_headers_for_test(
        service_account_id: &str,
        role: &str,
        all_plants: bool,
        plant_ids: &[&str],
        allowed_tools: &[&str],
    ) -> HeaderMap {
        ensure_test_mcp_oauth_env();
        let claims = serde_json::json!({
            "iss": std::env::var(MCP_OAUTH_SERVICE_ACCOUNT_ISSUER_ENV).expect("issuer env should be configured"),
            "aud": std::env::var(MCP_OAUTH_SERVICE_ACCOUNT_AUDIENCE_ENV).expect("audience env should be configured"),
            "sub": service_account_id,
            "exp": 4_102_444_800i64,
            "iat": 1_577_836_800i64,
            "nbf": 1_577_836_800i64,
            "role": role,
            "allPlants": all_plants,
            "plantIds": plant_ids,
            "allowedTools": allowed_tools,
        });
        let token = build_test_mcp_oauth_jwt_token(claims);
        let mut headers = HeaderMap::new();
        headers.insert(
            AUTHORIZATION,
            axum::http::HeaderValue::from_str(
                format!("{AUTHORIZATION_BEARER_PREFIX}{token}").as_str(),
            )
            .expect("authorization header should be valid"),
        );
        headers
    }

    fn build_state(now_epoch_day: i32) -> AppState {
        std::env::set_var("OTEL_EXPORTER_OTLP_ENDPOINT", "http://127.0.0.1:4317");

        let committee = committee_admin();
        let vendor_actor = vendor_operator();
        let employee = employee_actor();
        let plant = plant_id("fab-a");
        let vendor_visible = vendor_id("ven-discoverytst-a1");
        let vendor_hidden = vendor_id("ven-discoverytst-b1");
        let audit_trail = ImmutableAuditTrail::new(AuditRetentionPolicy::default());

        let mut compliance_lifecycle = VendorComplianceLifecycle::with_audit_trail(
            HistoryRetentionPolicy::default(),
            audit_trail.clone(),
        );
        let category = VendorCategory::parse("RESTAURANT").expect("category should be valid");
        let template = DocumentTemplateId::parse("tmpl-discovery-license")
            .expect("template id should be valid");
        compliance_lifecycle
            .upsert_document_template(
                &committee,
                ComplianceDocumentTemplate::new(
                    template.clone(),
                    category.clone(),
                    "Business License",
                    true,
                    365,
                    vec![30, 7],
                    0,
                )
                .expect("template should be valid"),
            )
            .expect("template should be upserted");

        for (vendor, display_name) in [
            (&vendor_visible, "Visible Vendor"),
            (&vendor_hidden, "Hidden Vendor"),
        ] {
            compliance_lifecycle
                .register_vendor_application(
                    &vendor_actor,
                    vendor.clone(),
                    display_name,
                    category.clone(),
                    ComplianceDate::from_epoch_day(now_epoch_day.saturating_sub(5)),
                )
                .expect("vendor application should be registered");
            compliance_lifecycle
                .submit_document(
                    &vendor_actor,
                    vendor,
                    &template,
                    VendorDocumentSubmission::new(
                        seeded_compliance_document_ref(vendor, "discovery-license.pdf"),
                        ComplianceDate::from_epoch_day(now_epoch_day.saturating_sub(5)),
                        ComplianceDate::from_epoch_day(now_epoch_day.saturating_add(300)),
                    )
                    .expect("document submission should be valid"),
                )
                .expect("document should be submitted");
            compliance_lifecycle
                .review_application(
                    &committee,
                    vendor,
                    VendorReviewDecision::Approved,
                    "approved",
                    ComplianceDate::from_epoch_day(now_epoch_day.saturating_sub(4)),
                )
                .expect("vendor should be approved");
        }

        let mut delivery_policy = VendorPlantDeliveryPolicy::with_audit_trail(audit_trail.clone());
        delivery_policy
            .upsert_mapping(
                &committee,
                taipei_moment(now_epoch_day.saturating_sub(1), 1),
                VendorPlantDeliveryMapping::new(
                    DeliveryMappingId::parse("map-discovery-allow")
                        .expect("mapping id should be valid"),
                    vendor_visible.clone(),
                    plant.clone(),
                    ServiceWindow::new(
                        taipei_moment(now_epoch_day.saturating_sub(1), 0),
                        taipei_moment(now_epoch_day.saturating_add(10), 23 * 60 + 59),
                    )
                    .expect("service window should be valid"),
                    DeliveryRuleEffect::Allow,
                    100,
                ),
            )
            .expect("allow mapping should be configured");

        let menu_supply_policy =
            MenuSupplyPolicy::with_audit_trail(Default::default(), audit_trail.clone());
        let payroll_ledger_service =
            PayrollLedgerService::new(PayrollRetentionPolicy::default(), audit_trail.clone());
        menu_supply_policy
            .upsert_menu_item(
                &vendor_actor,
                VendorMenuItem::new(
                    menu_item_id("menu-discoverytsta1"),
                    vendor_visible.clone(),
                    VendorMenuItemDraft::new(
                        "Visible Bento",
                        "high protein bento",
                        "BENTO",
                        vec![MenuHealthTag::HighProtein],
                        Some(
                            MenuImageUrl::parse(seeded_menu_image_ref(
                                &vendor_visible,
                                "visible-bento.jpg",
                            ))
                            .expect("image should be valid"),
                        ),
                        Money::new("TWD", 12000).expect("money should be valid"),
                        5,
                        now_epoch_day.saturating_add(2),
                    )
                    .expect("menu draft should be valid"),
                ),
            )
            .expect("visible bento menu should be upserted");
        menu_supply_policy
            .upsert_menu_item(
                &vendor_actor,
                VendorMenuItem::new(
                    menu_item_id("menu-discoverytsta2"),
                    vendor_visible.clone(),
                    VendorMenuItemDraft::new(
                        "Visible Salad",
                        "vegan salad bowl",
                        "SALAD",
                        vec![MenuHealthTag::Vegan],
                        Some(
                            MenuImageUrl::parse(seeded_menu_image_ref(
                                &vendor_visible,
                                "visible-salad.jpg",
                            ))
                            .expect("image should be valid"),
                        ),
                        Money::new("TWD", 9000).expect("money should be valid"),
                        8,
                        now_epoch_day.saturating_add(3),
                    )
                    .expect("menu draft should be valid"),
                ),
            )
            .expect("visible salad menu should be upserted");
        menu_supply_policy
            .upsert_menu_item(
                &vendor_actor,
                VendorMenuItem::new(
                    menu_item_id("menu-discoverytstb1"),
                    vendor_hidden.clone(),
                    VendorMenuItemDraft::new(
                        "Hidden Bento",
                        "should not be discoverable",
                        "BENTO",
                        vec![MenuHealthTag::HighProtein],
                        Some(
                            MenuImageUrl::parse(seeded_menu_image_ref(
                                &vendor_hidden,
                                "hidden-bento.jpg",
                            ))
                            .expect("image should be valid"),
                        ),
                        Money::new("TWD", 11000).expect("money should be valid"),
                        9,
                        now_epoch_day.saturating_add(2),
                    )
                    .expect("menu draft should be valid"),
                ),
            )
            .expect("hidden vendor menu should be upserted");

        menu_supply_policy
            .create_order(
                &employee,
                order_id("ord-discovery-tst-001"),
                &vendor_visible,
                &plant,
                now_epoch_day.saturating_add(2),
                vec![
                    OrderLineItemRequest::new(menu_item_id("menu-discoverytsta1"), 2, vec![])
                        .expect("line item should be valid"),
                ],
                taipei_moment(now_epoch_day, 600),
            )
            .expect("order should consume inventory");

        AppState {
            #[cfg(test)]
            next_order_sequence: Arc::new(AtomicU64::new(1)),
            vendor_id: vendor_visible,
            plant_id: plant,
            recommendation_engine_runtime_enabled: false,
            advanced_analytics_dashboard_runtime_enabled: false,
            rush_reminder_runtime_enabled: false,
            menu_recommendation_ranker: heuristic_menu_recommendation_ranker,
            rush_reminder_workflow: RushReminderWorkflow::new(RushReminderPolicy::default()),
            rush_reminder_delivery_gateway: Arc::new(NoopRushReminderDeliveryGateway),
            object_storage_upload_pipeline: test_object_storage_upload_pipeline(),
            operations_analytics_warehouse: Arc::new(RwLock::new(
                OperationsAnalyticsWarehouse::default(),
            )),
            terminated_employee_actor_ids: Arc::new(HashSet::new()),
            audit_trail: audit_trail.clone(),
            payroll_export_field_encryptor: payroll_export_field_encryptor(),
            payroll_ledger_service,
            anomaly_alert_workflow: AnomalyAlertWorkflow::with_default_rules(audit_trail.clone()),
            compliance_lifecycle: Arc::new(RwLock::new(compliance_lifecycle)),
            compliance_persistence: CompliancePersistence::InMemoryOnly,
            runtime_state_persistence: RuntimeStatePersistence::InMemoryOnly,
            runtime_state_cache: None,
            runtime_state_cache_bypass_keys: Arc::new(Mutex::new(HashSet::new())),
            order_event_backbone: None,
            delivery_policy: Arc::new(delivery_policy),
            menu_supply_policy,
            pickup_totp_verifier: Arc::new(
                PickupTotpVerifier::from_secret("unit-test-pickup-totp-secret".as_bytes())
                    .expect("test pickup verifier should be valid"),
            ),
        }
    }

    fn build_state_with_recommendation_runtime(
        now_epoch_day: i32,
        recommendation_engine_runtime_enabled: bool,
    ) -> AppState {
        let mut state = build_state(now_epoch_day);
        state.recommendation_engine_runtime_enabled = recommendation_engine_runtime_enabled;
        state
    }

    fn build_state_with_rush_reminder_runtime(
        now_epoch_day: i32,
        rush_reminder_runtime_enabled: bool,
    ) -> AppState {
        let mut state = build_state(now_epoch_day);
        state.rush_reminder_runtime_enabled = rush_reminder_runtime_enabled;
        state
    }

    fn build_state_with_advanced_analytics_runtime(
        now_epoch_day: i32,
        advanced_analytics_dashboard_runtime_enabled: bool,
    ) -> AppState {
        let mut state = build_state(now_epoch_day);
        state.advanced_analytics_dashboard_runtime_enabled =
            advanced_analytics_dashboard_runtime_enabled;
        state
    }

    fn seed_previous_pay_period_payroll_record(
        state: &AppState,
        now_epoch_day: i32,
        order_id_value: &str,
        amount_minor: u32,
    ) {
        let previous_pay_period = previous_pay_period_for_epoch_day(now_epoch_day);
        let delivery_epoch_day = parse_iso_date_to_epoch_day(&format!("{previous_pay_period}-15"))
            .expect("previous pay period test date should be valid");
        let employee = employee_actor();
        let source_event = PayrollLedgerSourceRef::new(
            PayrollLedgerSourceKind::OrderMutation,
            format!("order:{order_id_value}:state:CREATED"),
        )
        .expect("payroll source ref should be valid");
        let occurred_at = AuditTimestamp::from_taipei_business_moment(delivery_epoch_day, 600)
            .expect("audit timestamp should be valid");
        state
            .payroll_ledger_service
            .reconcile_order_charge(
                &employee,
                "createEmployeeOrder",
                &order_id(order_id_value),
                employee.actor_id(),
                EmploymentStatus::Active,
                delivery_epoch_day,
                "TWD",
                amount_minor,
                occurred_at,
                source_event,
            )
            .expect("previous-cycle payroll record should be seeded");
    }

    fn vendor_metric_value(
        dashboard: &OperationsAnalyticsDashboardPayload,
        vendor_id: &str,
        metric_key: &str,
    ) -> f64 {
        dashboard
            .vendor_breakdown
            .iter()
            .find(|row| row.vendor_id == vendor_id)
            .and_then(|row| {
                row.metrics
                    .iter()
                    .find(|metric| metric.metric_key == metric_key)
            })
            .map(|metric| metric.value)
            .expect("metric should exist in vendor breakdown")
    }

    fn failing_menu_recommendation_ranker(
        _entries: &mut Vec<EmployeeMenuDiscoveryEntry>,
        _at: TaipeiBusinessMoment,
        _sort_by: MenuSortFieldQuery,
        _sort_order: SortOrderQuery,
    ) -> Result<(), String> {
        Err("simulated recommendation engine outage".to_owned())
    }

    #[derive(Debug)]
    struct FailingRushReminderDeliveryGateway;

    impl RushReminderDeliveryGateway for FailingRushReminderDeliveryGateway {
        fn deliver(
            &self,
            _notification: &corporate_catering_system::rush_reminder::RushReminderNotification,
        ) -> Result<(), RushReminderDeliveryError> {
            Err(RushReminderDeliveryError::new(
                "simulated reminder delivery outage",
            ))
        }
    }

    fn build_state_with_terminated_load_gate_employee(now_epoch_day: i32) -> AppState {
        let mut state = build_state(now_epoch_day);
        state.terminated_employee_actor_ids =
            Arc::new(HashSet::from([actor_id(LOAD_GATE_EMPLOYEE_ACTOR_ID)]));
        state
    }

    #[test]
    fn bootstrap_runtime_state_seeds_local_dev_baseline_scenarios() {
        std::env::set_var("OTEL_EXPORTER_OTLP_ENDPOINT", "http://127.0.0.1:4317");

        let now_epoch_day = current_taipei_business_moment()
            .expect("current time should resolve for test")
            .epoch_day();
        let delivery_epoch_day = now_epoch_day.saturating_add(2);
        let audit_trail = ImmutableAuditTrail::new(AuditRetentionPolicy::default());
        let object_storage_upload_pipeline = test_object_storage_upload_pipeline();
        let compliance_lifecycle = build_seeded_load_gate_compliance_lifecycle(
            audit_trail.clone(),
            HistoryRetentionPolicy::default(),
            object_storage_upload_pipeline.as_ref(),
            vendor_id(DEFAULT_VENDOR_ID),
            plant_id(DEFAULT_PLANT_ID),
            delivery_epoch_day,
        )
        .expect("seeded compliance lifecycle should initialize");
        let state = bootstrap_runtime_state(
            audit_trail,
            vendor_id(DEFAULT_VENDOR_ID),
            plant_id(DEFAULT_PLANT_ID),
            delivery_epoch_day,
            8,
            false,
            false,
            false,
            RushReminderPolicy::default(),
            object_storage_upload_pipeline,
            PayrollRetentionPolicy::default(),
            OrderRetentionPolicy::default(),
            payroll_export_field_encryptor(),
            Arc::new(
                PickupTotpVerifier::from_secret("seed-baseline-pickup-secret".as_bytes())
                    .expect("seed pickup secret should be valid"),
            ),
            compliance_lifecycle,
            CompliancePersistence::InMemoryOnly,
            RuntimeStatePersistence::InMemoryOnly,
            None,
            None,
            true,
        )
        .expect("runtime bootstrap should seed baseline scenarios");

        let lifecycle_vendor_id = vendor_id(DEFAULT_SEED_LIFECYCLE_VENDOR_ID);
        let compliance = read_compliance_lifecycle(&state).expect("compliance state should lock");
        let lifecycle_vendor = compliance
            .vendor(&lifecycle_vendor_id)
            .expect("lifecycle seed vendor should exist");
        assert_eq!(lifecycle_vendor.status(), VendorComplianceStatus::Active);
        assert!(
            lifecycle_vendor.history().iter().any(|entry| matches!(
                entry.kind(),
                ComplianceHistoryKind::ExpiryReminderIssued { .. }
            )),
            "lifecycle seed vendor should include reminder history"
        );
        assert!(
            lifecycle_vendor
                .history()
                .iter()
                .any(|entry| matches!(entry.kind(), ComplianceHistoryKind::Suspended { .. })),
            "lifecycle seed vendor should include suspension history"
        );
        assert!(
            lifecycle_vendor
                .history()
                .iter()
                .any(|entry| matches!(entry.kind(), ComplianceHistoryKind::Reinstated)),
            "lifecycle seed vendor should include reinstatement history"
        );
        let runtime_plant_id = plant_id(DEFAULT_PLANT_ID);
        let runtime_deny_plant_id = plant_id(DEFAULT_SEED_DENY_PLANT_ID);
        let mapping_check_at = taipei_moment(delivery_epoch_day, 600);
        assert!(
            state
                .delivery_policy
                .ensure_vendor_deliverable_for_order(
                    &compliance,
                    &lifecycle_vendor_id,
                    &runtime_plant_id,
                    mapping_check_at,
                )
                .is_ok(),
            "lifecycle vendor should be deliverable for runtime plant"
        );
        let deny_result = state.delivery_policy.ensure_vendor_deliverable_for_order(
            &compliance,
            &vendor_id(DEFAULT_VENDOR_ID),
            &runtime_deny_plant_id,
            mapping_check_at,
        );
        drop(compliance);
        assert!(
            matches!(
                deny_result,
                Err(VendorPlantDeliveryError::DeliverabilityDenied { .. })
            ),
            "default vendor should be denied for the seeded deny-plant mapping"
        );

        let dispute_employee_actor = AuthenticatedActorContext::new(
            actor_id(DEFAULT_SEED_DISPUTE_EMPLOYEE_ACTOR_ID),
            Role::Employee,
            PlantScope::restricted(vec![runtime_plant_id]).expect("scope should be valid"),
            AuthenticationSource::CorporateSso,
        )
        .expect("dispute employee actor should be valid");
        let dispute_view = state
            .payroll_ledger_service
            .employee_order_view(
                &dispute_employee_actor,
                &order_id(DEFAULT_SEED_DISPUTE_ORDER_ID),
            )
            .expect("seeded dispute order ledger should exist");
        assert_eq!(dispute_view.disputes().len(), 1);
        assert_eq!(
            dispute_view.disputes()[0].status(),
            PayrollDisputeStatus::ResolvedRefundApproved
        );

        let closed_alerts = state
            .anomaly_alert_workflow
            .query_alerts(
                &corporate_catering_system::anomaly_alert::AnomalyAlertQuery {
                    vendor_id: Some(vendor_id(DEFAULT_VENDOR_ID)),
                    owner_actor_id: None,
                    status: Some(AnomalyAlertStatus::Closed),
                    escalated_only: None,
                    sla_status: None,
                },
                AuditTimestamp::from_taipei_business_moment(delivery_epoch_day, 1439)
                    .expect("anomaly query timestamp should be valid"),
            )
            .expect("seeded anomaly alerts should be queryable");
        assert!(
            !closed_alerts.is_empty(),
            "seed baseline should include at least one closed anomaly alert"
        );
    }

    #[test]
    fn committee_admin_authorization_requires_bearer_actor_context() {
        let missing = require_corporate_actor_for_role(&HeaderMap::new(), Role::CommitteeAdmin)
            .expect_err("missing authorization header should fail");
        assert_eq!(missing.0, StatusCode::UNAUTHORIZED);

        let payroll_headers = bearer_headers("payroll-test", "PAYROLL_OPERATOR");
        let forbidden = require_corporate_actor_for_role(&payroll_headers, Role::CommitteeAdmin)
            .expect_err("non-committee role should be forbidden");
        assert_eq!(forbidden.0, StatusCode::FORBIDDEN);

        let committee_headers = bearer_headers("committee-test", "COMMITTEE_ADMIN");
        let committee = require_corporate_actor_for_role(&committee_headers, Role::CommitteeAdmin)
            .expect("committee actor header should authorize");
        assert_eq!(committee.actor_id().as_str(), "committee-test");
        assert_eq!(committee.role(), Role::CommitteeAdmin);
    }

    #[test]
    fn payroll_operator_authorization_requires_bearer_actor_context() {
        let missing = require_corporate_actor_for_role(&HeaderMap::new(), Role::PayrollOperator)
            .expect_err("missing authorization header should fail");
        assert_eq!(missing.0, StatusCode::UNAUTHORIZED);

        let committee_headers = bearer_headers("committee-test", "COMMITTEE_ADMIN");
        let forbidden = require_corporate_actor_for_role(&committee_headers, Role::PayrollOperator)
            .expect_err("non-payroll role should be forbidden");
        assert_eq!(forbidden.0, StatusCode::FORBIDDEN);

        let payroll_headers = bearer_headers("payroll-test", "PAYROLL_OPERATOR");
        let payroll = require_corporate_actor_for_role(&payroll_headers, Role::PayrollOperator)
            .expect("payroll actor header should authorize");
        assert_eq!(payroll.actor_id().as_str(), "payroll-test");
        assert_eq!(payroll.role(), Role::PayrollOperator);
    }

    #[test]
    fn vendor_operator_authorization_uses_vendor_mfa_authentication_source() {
        let missing = require_vendor_operator_actor(&HeaderMap::new())
            .expect_err("missing authorization header should fail");
        assert_eq!(missing.0, StatusCode::UNAUTHORIZED);

        let committee_headers = bearer_headers("committee-test", "COMMITTEE_ADMIN");
        let forbidden = require_vendor_operator_actor(&committee_headers)
            .expect_err("non-vendor role should be forbidden");
        assert_eq!(forbidden.0, StatusCode::FORBIDDEN);

        let vendor_headers = bearer_headers("vendor-test", "VENDOR_OPERATOR");
        let vendor =
            require_vendor_operator_actor(&vendor_headers).expect("vendor actor should authorize");
        assert_eq!(vendor.actor_id().as_str(), "vendor-test");
        assert_eq!(vendor.role(), Role::VendorOperator);
        assert_eq!(
            vendor.authentication_source(),
            AuthenticationSource::VendorAccountMfa
        );
    }

    #[test]
    fn object_storage_vendor_upload_and_access_link_pipeline_succeeds() {
        let state = build_state(20_000);
        let vendor_actor = vendor_operator();
        let upload_plan = handle_create_vendor_object_storage_upload_plan(
            &state,
            &vendor_actor,
            ObjectStorageUploadRequestPayload {
                artifact_class: "MENU_IMAGE".to_owned(),
                file_name: "lunch-bento.png".to_owned(),
                mime_type: "image/png".to_owned(),
                size_bytes: 180_000,
                thumbnail_size_bytes: Some(72_000),
                locale: None,
            },
        )
        .expect("menu image upload plan should succeed");
        assert!(upload_plan
            .primary
            .object_ref
            .starts_with("s3://menu-assets/"));
        assert!(upload_plan.primary.upload_url.contains("X-Amz-Signature="));
        assert!(upload_plan.thumbnail.is_some());

        let access_link = handle_create_vendor_object_storage_access_link(
            &state,
            &vendor_actor,
            ObjectStorageAccessLinkRequestPayload {
                object_ref: upload_plan.primary.object_ref.clone(),
                locale: Some("en-US".to_owned()),
            },
        )
        .expect("download access link should succeed");
        assert_eq!(access_link.object_ref, upload_plan.primary.object_ref);
        assert!(access_link.download_url.contains("X-Amz-Signature="));
    }

    #[test]
    fn object_storage_vendor_access_link_rejects_unowned_reference() {
        let state = build_state(20_000);
        let vendor_actor = vendor_operator();
        let (status, error) = handle_create_vendor_object_storage_access_link(
            &state,
            &vendor_actor,
            ObjectStorageAccessLinkRequestPayload {
                object_ref:
                    "s3://menu-assets/corporate-catering/menu-images/ven-other/20260417/262144-deadbeef-not-owned.jpg"
                        .to_owned(),
                locale: Some("en-US".to_owned()),
            },
        )
        .expect_err("vendor should not access object refs outside owned scope");
        assert_eq!(status, StatusCode::FORBIDDEN);
        assert_eq!(error.code, "FORBIDDEN");
    }

    #[test]
    fn object_storage_vendor_upload_plan_rejects_out_of_scope_vendor_actor() {
        let state = build_state(20_000);
        let out_of_scope_vendor = AuthenticatedActorContext::new(
            actor_id("vendor-out-of-scope"),
            Role::VendorOperator,
            PlantScope::restricted(vec![plant_id("fab-b")]).expect("scope should be valid"),
            AuthenticationSource::VendorAccountMfa,
        )
        .expect("vendor actor should be valid");
        let (status, error) = handle_create_vendor_object_storage_upload_plan(
            &state,
            &out_of_scope_vendor,
            ObjectStorageUploadRequestPayload {
                artifact_class: "MENU_IMAGE".to_owned(),
                file_name: "lunch-bento.png".to_owned(),
                mime_type: "image/png".to_owned(),
                size_bytes: 180_000,
                thumbnail_size_bytes: Some(72_000),
                locale: None,
            },
        )
        .expect_err("out-of-scope vendor should be rejected");
        assert_eq!(status, StatusCode::FORBIDDEN);
        assert_eq!(error.code, "FORBIDDEN");
    }

    #[test]
    fn object_storage_upload_rejects_missing_thumbnail_size_for_menu_image() {
        let state = build_state(20_000);
        let vendor_actor = vendor_operator();
        let (status, error) = handle_create_vendor_object_storage_upload_plan(
            &state,
            &vendor_actor,
            ObjectStorageUploadRequestPayload {
                artifact_class: "MENU_IMAGE".to_owned(),
                file_name: "lunch-bento.png".to_owned(),
                mime_type: "image/png".to_owned(),
                size_bytes: 180_000,
                thumbnail_size_bytes: None,
                locale: Some("zh-TW".to_owned()),
            },
        )
        .expect_err("missing thumbnail size should be rejected");
        assert_eq!(status, StatusCode::BAD_REQUEST);
        assert_eq!(error.code, "OBJECT_STORAGE_SIZE_EXCEEDED");
    }

    #[test]
    fn object_storage_upload_rejects_invalid_mime_with_localized_message() {
        let state = build_state(20_000);
        let vendor_actor = vendor_operator();
        let (status, error) = handle_create_vendor_object_storage_upload_plan(
            &state,
            &vendor_actor,
            ObjectStorageUploadRequestPayload {
                artifact_class: "COMPLIANCE_DOCUMENT".to_owned(),
                file_name: "not-allowed.bin".to_owned(),
                mime_type: "application/octet-stream".to_owned(),
                size_bytes: 1024,
                thumbnail_size_bytes: None,
                locale: Some("zh-TW".to_owned()),
            },
        )
        .expect_err("invalid mime should be rejected");
        assert_eq!(status, StatusCode::BAD_REQUEST);
        assert_eq!(error.code, "OBJECT_STORAGE_INVALID_MIME");
        assert!(
            error.message.contains("不支援"),
            "localized invalid mime message should be zh-TW, got `{}`",
            error.message
        );
    }

    #[test]
    fn object_storage_upload_rejects_oversized_payload_with_localized_message() {
        let state = build_state(20_000);
        let vendor_actor = vendor_operator();
        let (status, error) = handle_create_vendor_object_storage_upload_plan(
            &state,
            &vendor_actor,
            ObjectStorageUploadRequestPayload {
                artifact_class: "COMPLIANCE_DOCUMENT".to_owned(),
                file_name: "oversized.pdf".to_owned(),
                mime_type: "application/pdf".to_owned(),
                size_bytes: 25 * 1024 * 1024,
                thumbnail_size_bytes: None,
                locale: Some("zh-TW".to_owned()),
            },
        )
        .expect_err("oversized payload should be rejected");
        assert_eq!(status, StatusCode::BAD_REQUEST);
        assert_eq!(error.code, "OBJECT_STORAGE_SIZE_EXCEEDED");
        assert!(
            error.message.contains("超過"),
            "localized size limit message should be zh-TW, got `{}`",
            error.message
        );
    }

    #[test]
    fn mcp_service_account_grant_requires_signed_oauth_jwt() {
        let headers = mcp_oauth_headers_for_test(
            "svc-mcp-runtime-auth-test",
            "COMMITTEE_ADMIN",
            true,
            &[],
            &[MCP_TOOL_ANOMALY_UPSERT_RULE],
        );
        let grant = require_mcp_service_account_grant(&headers)
            .expect("signed OAuth JWT service-account token should authorize");
        assert_eq!(
            grant.service_account_id().as_str(),
            "svc-mcp-runtime-auth-test"
        );
        assert_eq!(
            grant.actor().authentication_source(),
            AuthenticationSource::OAuthServiceAccount
        );
        assert!(
            grant
                .allowed_tool_names()
                .contains(MCP_TOOL_ANOMALY_UPSERT_RULE),
            "granted MCP tools should include anomaly upsert for this token"
        );

        let valid_auth_header = headers
            .get(AUTHORIZATION)
            .expect("authorization header should be set")
            .to_str()
            .expect("authorization header should be utf-8");
        let valid_token = valid_auth_header
            .strip_prefix(AUTHORIZATION_BEARER_PREFIX)
            .expect("authorization header should contain bearer prefix");
        let segments = valid_token.split('.').collect::<Vec<_>>();
        assert_eq!(
            segments.len(),
            3,
            "JWT token must have exactly three segments"
        );

        let forged_signature = BASE64_URL_SAFE_NO_PAD.encode([0u8; 32].as_slice());
        let forged_token = format!("{}.{}.{}", segments[0], segments[1], forged_signature);
        let mut forged_headers = HeaderMap::new();
        forged_headers.insert(
            AUTHORIZATION,
            axum::http::HeaderValue::from_str(
                format!("{AUTHORIZATION_BEARER_PREFIX}{forged_token}").as_str(),
            )
            .expect("authorization header should be valid"),
        );
        let forged_error = require_mcp_service_account_grant(&forged_headers)
            .expect_err("forged signature should be rejected");
        assert_eq!(forged_error.0, StatusCode::UNAUTHORIZED);
        assert!(
            forged_error
                .1
                .message
                .contains("signature verification failed"),
            "forged token failure should mention signature verification, got `{}`",
            forged_error.1.message
        );

        let mut legacy_headers = HeaderMap::new();
        legacy_headers.insert(
            AUTHORIZATION,
            axum::http::HeaderValue::from_str(
                format!(
                    "{AUTHORIZATION_BEARER_PREFIX}{MCP_OAUTH_SERVICE_ACCOUNT_TOKEN_PREFIX}{}",
                    BASE64_STANDARD.encode(r#"{"serviceAccountId":"svc-mcp-runtime-auth-test"}"#),
                )
                .as_str(),
            )
            .expect("legacy authorization header should be valid"),
        );
        let legacy_error = require_mcp_service_account_grant(&legacy_headers)
            .expect_err("legacy self-asserted prefix token must be rejected");
        assert_eq!(legacy_error.0, StatusCode::UNAUTHORIZED);
        assert!(
            legacy_error.1.message.contains("legacy MCP token format"),
            "legacy token rejection should be explicit, got `{}`",
            legacy_error.1.message
        );
    }

    #[test]
    fn mcp_bridge_headers_require_registered_rotation_metadata() {
        let mut headers = mcp_oauth_headers_for_test(
            "svc-mcp-runtime-auth-test",
            "COMMITTEE_ADMIN",
            true,
            &[],
            &[MCP_TOOL_ANOMALY_UPSERT_RULE],
        );
        headers.insert(
            MCP_BRIDGE_KEY_ID_HEADER,
            axum::http::HeaderValue::from_static("bridge-runtime-test"),
        );
        headers.insert(
            MCP_BRIDGE_ISSUED_AT_HEADER,
            axum::http::HeaderValue::from_static("1000"),
        );
        headers.insert(
            MCP_BRIDGE_EXPIRES_AT_HEADER,
            axum::http::HeaderValue::from_static("1600"),
        );
        headers.insert(
            MCP_BRIDGE_ROTATED_AT_HEADER,
            axum::http::HeaderValue::from_static("1550"),
        );
        headers.insert(
            MCP_BRIDGE_AUDIT_REASON_HEADER,
            axum::http::HeaderValue::from_static("incident runbook mcp bridge"),
        );

        let grant = require_mcp_service_account_grant(&headers)
            .expect("MCP service account grant should be valid");
        let parsed_bridge =
            parse_optional_mcp_short_lived_bridge(&headers, grant.service_account_id())
                .expect("registered bridge key metadata should be accepted");
        assert!(parsed_bridge.is_some());

        headers.insert(
            MCP_BRIDGE_ROTATED_AT_HEADER,
            axum::http::HeaderValue::from_static("1499"),
        );
        let metadata_mismatch =
            parse_optional_mcp_short_lived_bridge(&headers, grant.service_account_id())
                .expect_err("bridge metadata must match server-side registry");
        assert_eq!(metadata_mismatch.0, StatusCode::UNAUTHORIZED);
        assert!(
            metadata_mismatch
                .1
                .message
                .contains("metadata does not match server rotation records"),
            "bridge metadata mismatch should mention server rotation records, got `{}`",
            metadata_mismatch.1.message
        );
    }

    #[test]
    fn audit_retention_purge_job_removes_expired_evidence() {
        let committee = committee_admin();
        let audit_trail =
            ImmutableAuditTrail::new(AuditRetentionPolicy::new(1).expect("policy should be valid"));

        audit_trail
            .append(AuditEvidenceWrite::new(
                AuditTimestamp::from_epoch_day(-100_000),
                AuditIdentityLink::from_actor(&committee, "seedAuditEvidence"),
                AuditAction::RunVendorComplianceLifecycle,
                AuditEntityRef::new(AuditEntityType::Vendor, "ven-audit-retention-seed")
                    .expect("entity ref should be valid"),
                AuditCorrelationId::parse("case:audit-retention-seed")
                    .expect("correlation id should be valid"),
            ))
            .expect("seed audit evidence should be appended");
        assert_eq!(
            audit_trail
                .evidence_count()
                .expect("evidence count should resolve"),
            1
        );

        run_audit_retention_purge_once(&audit_trail, &committee);

        assert_eq!(
            audit_trail
                .evidence_count()
                .expect("evidence count should resolve"),
            1
        );
        let events = audit_trail
            .investigation_query(
                &committee,
                &AuditInvestigationFilter::default().with_action(AuditAction::PurgeAuditEvidence),
            )
            .expect("purge audit event should be queryable");
        assert_eq!(events.len(), 1);
        assert_eq!(events[0].audit_identity().actor_id(), committee.actor_id());
        assert_eq!(
            events[0].entity().entity_type(),
            AuditEntityType::AuditTrail
        );
    }

    #[test]
    fn payroll_retention_purge_is_queryable_via_audit_investigations() {
        let now_epoch_day = current_taipei_business_moment()
            .expect("current time should resolve for test")
            .epoch_day();
        let state = build_state(now_epoch_day);
        let committee = committee_admin();
        let payroll = payroll_operator();
        let create_request = EmployeeOrderCreateRequestPayload {
            plant_id: "fab-a".to_owned(),
            delivery_date: epoch_day_to_iso_date(now_epoch_day.saturating_add(2)),
            line_items: vec![OrderLineItemRequestPayload {
                menu_item_id: "menu-discoverytsta1".to_owned(),
                quantity: 1,
                special_requests: vec![],
            }],
            employee_note: None,
        };
        let created_order =
            handle_create_employee_order(&state, create_request).expect("order should be created");
        let pay_period = created_order.delivery_date[..7].to_owned();

        handle_export_payroll_deductions(
            &state,
            &payroll,
            PayrollExportQuery {
                pay_period: Some(pay_period),
                cycle_key: Some("cycle-payroll-retention-runtime".to_owned()),
                page: Some(1),
                page_size: Some(20),
                sort_by: Some(PayrollSortFieldQuery::DeliveryDate),
                sort_order: Some(SortOrderQuery::Asc),
            },
        )
        .expect("payroll export should succeed");

        let purge_report = handle_purge_payroll_data(
            &state,
            &committee,
            PayrollRetentionPurgeRequest {
                as_of_epoch_day: Some(now_epoch_day.saturating_add(800)),
            },
        )
        .expect("payroll retention purge should succeed");
        assert!(purge_report.purged_ledger_entries > 0);

        let investigations = handle_query_audit_investigations(
            &state,
            &committee,
            AuditInvestigationQuery {
                actor_id: None,
                action: Some("PURGE_PAYROLL_DATA".to_owned()),
                entity_type: None,
                entity_id: None,
                occurred_from_epoch_day: None,
                occurred_to_epoch_day: None,
                correlation_id: None,
            },
        )
        .expect("purge payroll event should be queryable");
        assert_eq!(investigations.items.len(), 1);
        assert_eq!(investigations.items[0].action, "PURGE_PAYROLL_DATA");
    }

    #[test]
    fn order_retention_purge_removes_orders_and_emits_queryable_audit_event() {
        let now_epoch_day = current_taipei_business_moment()
            .expect("current time should resolve for test")
            .epoch_day();
        let state = build_state(now_epoch_day);
        let committee = committee_admin();
        let create_request = EmployeeOrderCreateRequestPayload {
            plant_id: "fab-a".to_owned(),
            delivery_date: epoch_day_to_iso_date(now_epoch_day.saturating_add(2)),
            line_items: vec![OrderLineItemRequestPayload {
                menu_item_id: "menu-discoverytsta1".to_owned(),
                quantity: 1,
                special_requests: vec![],
            }],
            employee_note: None,
        };
        let created_order =
            handle_create_employee_order(&state, create_request).expect("order should be created");
        let created_order_id = parse_contract_order_id(&created_order.order_id)
            .expect("created order id should parse");

        let purge_report = handle_purge_order_data(
            &state,
            &committee,
            OrderRetentionPurgeRequest {
                as_of_epoch_day: Some(now_epoch_day.saturating_add(800)),
            },
        )
        .expect("order retention purge should succeed");
        assert!(purge_report.purged_orders > 0);
        assert!(state
            .menu_supply_policy
            .order_snapshot(&created_order_id)
            .expect("order snapshot should resolve")
            .is_none());

        let investigations = handle_query_audit_investigations(
            &state,
            &committee,
            AuditInvestigationQuery {
                actor_id: None,
                action: Some("PURGE_ORDER_DATA".to_owned()),
                entity_type: None,
                entity_id: None,
                occurred_from_epoch_day: None,
                occurred_to_epoch_day: None,
                correlation_id: None,
            },
        )
        .expect("purge order event should be queryable");
        assert_eq!(investigations.items.len(), 1);
        assert_eq!(investigations.items[0].action, "PURGE_ORDER_DATA");
    }

    #[test]
    fn occurred_to_epoch_day_filter_includes_same_day_events() {
        let committee = committee_admin();
        let audit_trail = ImmutableAuditTrail::new(AuditRetentionPolicy::default());
        let state = AppState {
            #[cfg(test)]
            next_order_sequence: Arc::new(AtomicU64::new(1)),
            vendor_id: vendor_id("ven-filter-day"),
            plant_id: plant_id("fab-a"),
            recommendation_engine_runtime_enabled: false,
            advanced_analytics_dashboard_runtime_enabled: false,
            rush_reminder_runtime_enabled: false,
            menu_recommendation_ranker: heuristic_menu_recommendation_ranker,
            rush_reminder_workflow: RushReminderWorkflow::new(RushReminderPolicy::default()),
            rush_reminder_delivery_gateway: Arc::new(NoopRushReminderDeliveryGateway),
            object_storage_upload_pipeline: test_object_storage_upload_pipeline(),
            operations_analytics_warehouse: Arc::new(RwLock::new(
                OperationsAnalyticsWarehouse::default(),
            )),
            terminated_employee_actor_ids: Arc::new(HashSet::new()),
            audit_trail: audit_trail.clone(),
            payroll_export_field_encryptor: payroll_export_field_encryptor(),
            payroll_ledger_service: PayrollLedgerService::new(
                PayrollRetentionPolicy::default(),
                audit_trail.clone(),
            ),
            anomaly_alert_workflow: AnomalyAlertWorkflow::with_default_rules(audit_trail.clone()),
            compliance_lifecycle: Arc::new(RwLock::new(
                VendorComplianceLifecycle::with_audit_trail(
                    HistoryRetentionPolicy::default(),
                    audit_trail.clone(),
                ),
            )),
            compliance_persistence: CompliancePersistence::InMemoryOnly,
            runtime_state_persistence: RuntimeStatePersistence::InMemoryOnly,
            runtime_state_cache: None,
            runtime_state_cache_bypass_keys: Arc::new(Mutex::new(HashSet::new())),
            order_event_backbone: None,
            delivery_policy: Arc::new(VendorPlantDeliveryPolicy::with_audit_trail(
                audit_trail.clone(),
            )),
            menu_supply_policy: MenuSupplyPolicy::with_audit_trail(
                Default::default(),
                audit_trail.clone(),
            ),
            pickup_totp_verifier: Arc::new(
                PickupTotpVerifier::from_secret("unit-test-pickup-totp-secret".as_bytes())
                    .expect("test pickup verifier should be valid"),
            ),
        };

        audit_trail
            .append(AuditEvidenceWrite::new(
                AuditTimestamp::new(41, 780).expect("timestamp should be valid"),
                AuditIdentityLink::from_actor(&committee, "seedAuditEvidence"),
                AuditAction::RunVendorComplianceLifecycle,
                AuditEntityRef::new(AuditEntityType::Vendor, "ven-filter-day")
                    .expect("entity ref should be valid"),
                AuditCorrelationId::parse("case:filter-day").expect("correlation id should parse"),
            ))
            .expect("seed same-day evidence should append");

        audit_trail
            .append(AuditEvidenceWrite::new(
                AuditTimestamp::new(42, 15).expect("timestamp should be valid"),
                AuditIdentityLink::from_actor(&committee, "seedAuditEvidence"),
                AuditAction::RunVendorComplianceLifecycle,
                AuditEntityRef::new(AuditEntityType::Vendor, "ven-filter-next-day")
                    .expect("entity ref should be valid"),
                AuditCorrelationId::parse("case:filter-next-day")
                    .expect("correlation id should parse"),
            ))
            .expect("seed next-day evidence should append");

        let response = handle_query_audit_investigations(
            &state,
            &committee,
            AuditInvestigationQuery {
                actor_id: None,
                action: None,
                entity_type: None,
                entity_id: None,
                occurred_from_epoch_day: None,
                occurred_to_epoch_day: Some(41),
                correlation_id: None,
            },
        )
        .expect("investigation query should succeed");
        assert_eq!(response.items.len(), 1);
        assert_eq!(response.items[0].entity_id, "ven-filter-day");
    }

    #[test]
    fn create_order_returns_canonical_employee_order_payload() {
        let now_epoch_day = current_taipei_business_moment()
            .expect("current time should resolve for test")
            .epoch_day();
        let state = build_state(now_epoch_day);
        let request = EmployeeOrderCreateRequestPayload {
            plant_id: "fab-a".to_owned(),
            delivery_date: epoch_day_to_iso_date(now_epoch_day.saturating_add(2)),
            line_items: vec![OrderLineItemRequestPayload {
                menu_item_id: "menu-discoverytsta1".to_owned(),
                quantity: 1,
                special_requests: vec![SpecialRequestOption::NoUtensils],
            }],
            employee_note: Some("no utensils please".to_owned()),
        };

        let response =
            handle_create_employee_order(&state, request).expect("create order should succeed");

        assert!(response.order_id.starts_with("ord-"));
        assert_eq!(response.employee_actor_id, LOAD_GATE_EMPLOYEE_ACTOR_ID);
        assert_eq!(response.plant_id, "fab-a");
        assert_eq!(
            response.delivery_date,
            epoch_day_to_iso_date(now_epoch_day + 2)
        );
        assert_eq!(response.status, "PENDING");
        assert_eq!(response.line_items.len(), 1);
        assert_eq!(response.line_items[0].menu_item_id, "menu-discoverytsta1");
        assert_eq!(response.line_items[0].quantity, 1);
        assert_eq!(response.line_items[0].price_per_unit.currency, "TWD");
        assert_eq!(response.line_items[0].price_per_unit.amount_minor, 12000);
        assert_eq!(response.total.currency, "TWD");
        assert_eq!(response.total.amount_minor, 12000);
        assert_eq!(
            response
                .timeline
                .first()
                .map(|event| event.event_type.as_str()),
            Some("CREATED")
        );
        assert!(response.created_at.ends_with("+08:00"));

        let serialized =
            serde_json::to_value(&response).expect("employee order payload should serialize");
        assert!(serialized.get("accepted").is_none());
        assert!(serialized.get("vendorId").is_none());
        assert!(serialized.get("deliveryEpochDay").is_none());
    }

    #[test]
    fn update_order_returns_canonical_employee_order_payload() {
        let now_epoch_day = current_taipei_business_moment()
            .expect("current time should resolve for test")
            .epoch_day();
        let state = build_state(now_epoch_day);
        let create_request = EmployeeOrderCreateRequestPayload {
            plant_id: "fab-a".to_owned(),
            delivery_date: epoch_day_to_iso_date(now_epoch_day.saturating_add(3)),
            line_items: vec![OrderLineItemRequestPayload {
                menu_item_id: "menu-discoverytsta2".to_owned(),
                quantity: 1,
                special_requests: vec![],
            }],
            employee_note: None,
        };
        let created_order = handle_create_employee_order(&state, create_request)
            .expect("create order should succeed");

        let update_request = UpdateOrderRequest {
            operation: "CANCEL".to_owned(),
            line_items: None,
            cancel_reason: Some("schedule changed".to_owned()),
        };
        let updated_order =
            handle_update_employee_order(&state, created_order.order_id.clone(), update_request)
                .expect("update order should succeed");

        assert_eq!(updated_order.order_id, created_order.order_id);
        assert_eq!(updated_order.status, "CANCELLED");
        assert_eq!(
            updated_order
                .timeline
                .last()
                .map(|event| event.event_type.as_str()),
            Some("CANCELLED")
        );

        let serialized =
            serde_json::to_value(&updated_order).expect("employee order payload should serialize");
        assert!(serialized.get("accepted").is_none());
    }

    #[test]
    fn employee_order_list_supports_pagination_filter_and_sorting() {
        let now_epoch_day = current_taipei_business_moment()
            .expect("current time should resolve for test")
            .epoch_day();
        let state = build_state(now_epoch_day);
        let employee = employee_actor();
        let delivery_date = epoch_day_to_iso_date(now_epoch_day.saturating_add(3));

        let first_created = handle_create_employee_order_for_actor(
            &state,
            &employee,
            "createEmployeeOrder",
            EmployeeOrderCreateRequestPayload {
                plant_id: "fab-a".to_owned(),
                delivery_date: delivery_date.clone(),
                line_items: vec![OrderLineItemRequestPayload {
                    menu_item_id: "menu-discoverytsta2".to_owned(),
                    quantity: 1,
                    special_requests: vec![],
                }],
                employee_note: None,
            },
        )
        .expect("first order should be created");

        let second_created = handle_create_employee_order_for_actor(
            &state,
            &employee,
            "createEmployeeOrder",
            EmployeeOrderCreateRequestPayload {
                plant_id: "fab-a".to_owned(),
                delivery_date: delivery_date.clone(),
                line_items: vec![OrderLineItemRequestPayload {
                    menu_item_id: "menu-discoverytsta2".to_owned(),
                    quantity: 1,
                    special_requests: vec![],
                }],
                employee_note: None,
            },
        )
        .expect("second order should be created");

        handle_update_employee_order_for_actor(
            &state,
            &employee,
            "updateEmployeeOrder",
            second_created.order_id.clone(),
            UpdateOrderRequest {
                operation: "CANCEL".to_owned(),
                line_items: None,
                cancel_reason: Some("shift changed".to_owned()),
            },
        )
        .expect("second order should be cancelled");

        let filtered = handle_list_employee_orders(
            &state,
            &employee,
            EmployeeOrderListQuery {
                plant_id: Some("fab-a".to_owned()),
                from_date: Some(delivery_date.clone()),
                to_date: Some(delivery_date.clone()),
                page: Some(1),
                page_size: Some(10),
                sort_by: Some(EmployeeOrderSortFieldQuery::CreatedAt),
                sort_order: Some(SortOrderQuery::Desc),
                status: Some("CANCELLED".to_owned()),
            },
        )
        .expect("employee order list with status filter should succeed");
        assert_eq!(filtered.items.len(), 1);
        assert_eq!(filtered.items[0].order_id, second_created.order_id);
        assert_eq!(filtered.items[0].status, "CANCELLED");
        assert_eq!(filtered.page.total_items, 1);

        let paged = handle_list_employee_orders(
            &state,
            &employee,
            EmployeeOrderListQuery {
                plant_id: Some("fab-a".to_owned()),
                from_date: Some(delivery_date.clone()),
                to_date: Some(delivery_date),
                page: Some(2),
                page_size: Some(1),
                sort_by: Some(EmployeeOrderSortFieldQuery::CreatedAt),
                sort_order: Some(SortOrderQuery::Asc),
                status: None,
            },
        )
        .expect("employee order list pagination should succeed");
        assert_eq!(paged.page.total_items, 2);
        assert_eq!(paged.page.total_pages, 2);
        assert_eq!(paged.items.len(), 1);
        assert_eq!(paged.items[0].order_id, second_created.order_id);
        assert_eq!(first_created.status, "PENDING");
    }

    #[test]
    fn employee_order_list_enforces_employee_role() {
        let now_epoch_day = current_taipei_business_moment()
            .expect("current time should resolve for test")
            .epoch_day();
        let state = build_state(now_epoch_day);
        let vendor = vendor_operator();

        let error = handle_list_employee_orders(
            &state,
            &vendor,
            EmployeeOrderListQuery {
                plant_id: Some("fab-a".to_owned()),
                ..EmployeeOrderListQuery::default()
            },
        )
        .expect_err("non-employee actor should be rejected");
        assert_eq!(error.0, StatusCode::FORBIDDEN);
        assert_eq!(error.1.code, "FORBIDDEN");
    }

    #[test]
    fn vendor_order_list_supports_pagination_filter_and_sorting() {
        let now_epoch_day = current_taipei_business_moment()
            .expect("current time should resolve for test")
            .epoch_day();
        let state = build_state(now_epoch_day);
        let vendor = vendor_operator();
        let delivery_date = epoch_day_to_iso_date(now_epoch_day.saturating_add(3));

        let first_created = handle_create_employee_order(
            &state,
            EmployeeOrderCreateRequestPayload {
                plant_id: "fab-a".to_owned(),
                delivery_date: delivery_date.clone(),
                line_items: vec![OrderLineItemRequestPayload {
                    menu_item_id: "menu-discoverytsta2".to_owned(),
                    quantity: 1,
                    special_requests: vec![],
                }],
                employee_note: None,
            },
        )
        .expect("first vendor-visible order should be created");

        let second_created = handle_create_employee_order(
            &state,
            EmployeeOrderCreateRequestPayload {
                plant_id: "fab-a".to_owned(),
                delivery_date: delivery_date.clone(),
                line_items: vec![OrderLineItemRequestPayload {
                    menu_item_id: "menu-discoverytsta2".to_owned(),
                    quantity: 1,
                    special_requests: vec![],
                }],
                employee_note: None,
            },
        )
        .expect("second vendor-visible order should be created");

        handle_update_employee_order(
            &state,
            second_created.order_id.clone(),
            UpdateOrderRequest {
                operation: "CANCEL".to_owned(),
                line_items: None,
                cancel_reason: Some("vendor board test".to_owned()),
            },
        )
        .expect("second order should be cancelled");

        let filtered = handle_list_vendor_orders(
            &state,
            &vendor,
            VendorOrderListQuery {
                plant_id: Some("fab-a".to_owned()),
                from_date: Some(delivery_date.clone()),
                to_date: Some(delivery_date.clone()),
                page: Some(1),
                page_size: Some(10),
                sort_by: Some(VendorOrderSortFieldQuery::CreatedAt),
                sort_order: Some(SortOrderQuery::Desc),
                status: Some("CANCELLED".to_owned()),
            },
        )
        .expect("vendor order list with status filter should succeed");
        assert_eq!(filtered.items.len(), 1);
        assert_eq!(filtered.items[0].order_id, second_created.order_id);
        assert_eq!(filtered.items[0].status, "CANCELLED");
        assert_eq!(filtered.page.total_items, 1);
        let serialized_entry =
            serde_json::to_value(&filtered.items[0]).expect("vendor order entry should serialize");
        assert!(serialized_entry.get("employeeActorId").is_none());

        let paged = handle_list_vendor_orders(
            &state,
            &vendor,
            VendorOrderListQuery {
                plant_id: Some("fab-a".to_owned()),
                from_date: Some(delivery_date.clone()),
                to_date: Some(delivery_date),
                page: Some(1),
                page_size: Some(1),
                sort_by: Some(VendorOrderSortFieldQuery::CreatedAt),
                sort_order: Some(SortOrderQuery::Asc),
                status: None,
            },
        )
        .expect("vendor order list pagination should succeed");
        assert_eq!(paged.page.total_items, 2);
        assert_eq!(paged.page.total_pages, 2);
        assert_eq!(paged.items.len(), 1);
        assert_eq!(paged.items[0].order_id, first_created.order_id);
    }

    #[test]
    fn vendor_order_list_enforces_vendor_role() {
        let now_epoch_day = current_taipei_business_moment()
            .expect("current time should resolve for test")
            .epoch_day();
        let state = build_state(now_epoch_day);
        let employee = employee_actor();

        let error = handle_list_vendor_orders(
            &state,
            &employee,
            VendorOrderListQuery {
                plant_id: Some("fab-a".to_owned()),
                ..VendorOrderListQuery::default()
            },
        )
        .expect_err("non-vendor actor should be rejected");
        assert_eq!(error.0, StatusCode::FORBIDDEN);
        assert_eq!(error.1.code, "FORBIDDEN");
    }

    #[test]
    fn discovery_filters_are_deterministic_and_use_exact_inventory() {
        let now_epoch_day = 300;
        let state = build_state(now_epoch_day);
        let query = EmployeeMenuDiscoveryQuery {
            plant_id: Some("fab-a".to_owned()),
            view: Some(MenuDiscoveryViewQuery::Week),
            menu_date: Some(epoch_day_to_iso_date(now_epoch_day)),
            menu_type: Some("bento".to_owned()),
            health_tag: Some("HIGH_PROTEIN".to_owned()),
            price_min_minor: Some(10000),
            price_max_minor: Some(13000),
            remaining_quantity: Some(3),
            ..EmployeeMenuDiscoveryQuery::default()
        };

        let response =
            handle_list_employee_menus_at(&state, query, taipei_moment(now_epoch_day, 600))
                .expect("discovery request should succeed");

        assert_eq!(response.items.len(), 1);
        assert_eq!(response.items[0].menu_item_id, "menu-discoverytsta1");
        assert_eq!(response.items[0].vendor_id, "ven-discoverytst-a1");
        assert_eq!(response.items[0].remaining_quantity, 3);
        assert_eq!(response.items[0].menu_type, "BENTO");
        assert_eq!(
            response.days.len(),
            7,
            "week view should provide seven dates"
        );
    }

    #[test]
    fn recommendation_is_not_requested_when_runtime_feature_flag_is_off() {
        let now_epoch_day = 300;
        let state = build_state(now_epoch_day);
        let query = EmployeeMenuDiscoveryQuery {
            plant_id: Some("fab-a".to_owned()),
            view: Some(MenuDiscoveryViewQuery::Week),
            menu_date: Some(epoch_day_to_iso_date(now_epoch_day)),
            ..EmployeeMenuDiscoveryQuery::default()
        };

        let response_a =
            handle_list_employee_menus_at(&state, query, taipei_moment(now_epoch_day, 600))
                .expect("discovery request should succeed");
        let query = EmployeeMenuDiscoveryQuery {
            plant_id: Some("fab-a".to_owned()),
            view: Some(MenuDiscoveryViewQuery::Week),
            menu_date: Some(epoch_day_to_iso_date(now_epoch_day)),
            ..EmployeeMenuDiscoveryQuery::default()
        };
        let response_b =
            handle_list_employee_menus_at(&state, query, taipei_moment(now_epoch_day, 600))
                .expect("discovery request should succeed");

        assert!(!response_a.recommendation_requested);
        assert!(!response_a.recommendation_applied);
        assert_eq!(
            response_a
                .items
                .iter()
                .map(|item| item.menu_item_id.clone())
                .collect::<Vec<_>>(),
            response_b
                .items
                .iter()
                .map(|item| item.menu_item_id.clone())
                .collect::<Vec<_>>(),
            "deterministic ordering should remain stable"
        );
    }

    #[test]
    fn rush_reminder_feature_flag_defaults_off() {
        let now_epoch_day = 300;
        let state = build_state(now_epoch_day);
        assert!(
            !state.rush_reminder_runtime_enabled,
            "rush reminder runtime should default to off"
        );
        let query = EmployeeMenuDiscoveryQuery {
            plant_id: Some("fab-a".to_owned()),
            view: Some(MenuDiscoveryViewQuery::Week),
            menu_date: Some(epoch_day_to_iso_date(now_epoch_day)),
            ..EmployeeMenuDiscoveryQuery::default()
        };

        handle_list_employee_menus_at(&state, query, taipei_moment(now_epoch_day, 600))
            .expect("discovery should succeed when rush reminders are disabled");
        let delivered = state
            .rush_reminder_workflow
            .delivered_notifications()
            .expect("delivered reminders should be queryable");
        assert!(delivered.is_empty());
    }

    #[test]
    fn advanced_analytics_dashboard_feature_flag_defaults_off() {
        let now_epoch_day = 300;
        let state = build_state(now_epoch_day);
        assert!(
            !state.advanced_analytics_dashboard_runtime_enabled,
            "advanced analytics dashboard runtime should default to off"
        );

        let error = handle_get_admin_operations_analytics_dashboard(
            &state,
            &committee_admin(),
            OperationsAnalyticsDashboardQueryRequest::default(),
        )
        .expect_err("feature-disabled analytics dashboard should return 404");
        assert_eq!(error.0, StatusCode::NOT_FOUND);
        assert_eq!(error.1.code, "NOT_FOUND");
    }

    #[test]
    fn advanced_analytics_dashboard_reports_metric_definitions_and_breakdowns_when_enabled() {
        let now_epoch_day = current_taipei_business_moment()
            .expect("current time should resolve for test")
            .epoch_day();
        let state = build_state_with_advanced_analytics_runtime(now_epoch_day, true);
        let committee = committee_admin();
        let payroll = payroll_operator();

        let created_order = handle_create_employee_order(
            &state,
            EmployeeOrderCreateRequestPayload {
                plant_id: "fab-a".to_owned(),
                delivery_date: epoch_day_to_iso_date(now_epoch_day.saturating_add(2)),
                line_items: vec![OrderLineItemRequestPayload {
                    menu_item_id: "menu-discoverytsta1".to_owned(),
                    quantity: 1,
                    special_requests: vec![],
                }],
                employee_note: None,
            },
        )
        .expect("order creation should succeed");

        let payroll_export = handle_export_payroll_deductions(
            &state,
            &payroll,
            PayrollExportQuery {
                pay_period: Some(created_order.delivery_date[..7].to_owned()),
                cycle_key: Some("cycle-analytics-dashboard".to_owned()),
                page: Some(1),
                page_size: Some(20),
                sort_by: Some(PayrollSortFieldQuery::DeliveryDate),
                sort_order: Some(SortOrderQuery::Asc),
            },
        )
        .expect("payroll export should succeed");
        handle_sync_payroll_hr_api_adjunct(
            &state,
            &payroll,
            payroll_export.exchange_batch.batch_id.clone(),
            PayrollHrApiSyncRequest {
                outcome: PayrollHrApiSyncOutcomePayload::Failed,
                note: Some("analytics sync failure drill".to_owned()),
            },
        )
        .expect("payroll hr sync update should succeed");

        let evaluation = handle_evaluate_anomaly_alerts(
            &state,
            &committee,
            AnomalyAlertEvaluationRequest {
                vendor_id: state.vendor_id.as_str().to_owned(),
                observed_at_epoch_day: Some(now_epoch_day),
                observed_at_minute_of_day: Some(600),
                days_until_expiry: None,
                on_time_rate: Some(0.80),
                satisfaction_score: None,
                complaint_count: None,
                default_owner_actor_id: None,
            },
        )
        .expect("anomaly evaluation should succeed");
        assert!(
            !evaluation.triggered_alerts.is_empty(),
            "anomaly evaluation should produce at least one triggered alert"
        );

        let dashboard = handle_get_admin_operations_analytics_dashboard(
            &state,
            &committee,
            OperationsAnalyticsDashboardQueryRequest::default(),
        )
        .expect("admin analytics dashboard should be queryable");
        assert_eq!(dashboard.metric_schema_version, "operations-v1");
        assert!(
            !dashboard.metric_definitions.is_empty(),
            "dashboard must expose metric definitions"
        );
        assert!(
            !dashboard.vendor_breakdown.is_empty(),
            "dashboard must expose vendor breakdown rows"
        );
        assert!(
            !dashboard.plant_breakdown.is_empty(),
            "dashboard must expose plant breakdown rows"
        );
        assert!(
            !dashboard.time_breakdown.is_empty(),
            "dashboard must expose time breakdown rows"
        );

        let vendor_row = dashboard
            .vendor_breakdown
            .iter()
            .find(|row| row.vendor_id == state.vendor_id.as_str())
            .expect("vendor breakdown should include runtime vendor");
        let anomaly_triggered_metric = vendor_row
            .metrics
            .iter()
            .find(|metric| metric.metric_key == "anomaly_triggered_total")
            .expect("vendor breakdown should include anomaly triggered metric");
        assert!(anomaly_triggered_metric.value >= 1.0);
        let payroll_sync_failed_metric = vendor_row
            .metrics
            .iter()
            .find(|metric| metric.metric_key == "payroll_hr_sync_failed_total")
            .expect("vendor breakdown should include payroll hr sync failure metric");
        assert!(payroll_sync_failed_metric.value >= 1.0);
    }

    #[test]
    fn advanced_analytics_settlement_replay_is_idempotent_for_metrics() {
        let now_epoch_day = current_taipei_business_moment()
            .expect("current time should resolve for test")
            .epoch_day();
        let state = build_state_with_advanced_analytics_runtime(now_epoch_day, true);
        let committee = committee_admin();
        let payroll = payroll_operator();

        seed_previous_pay_period_payroll_record(
            &state,
            now_epoch_day,
            "ord-analytics-settlement-replay",
            12_000,
        );

        let first_close = handle_close_payroll_monthly_settlement(
            &state,
            &payroll,
            PayrollMonthlySettlementCloseRequest::default(),
        )
        .expect("first monthly close should succeed");
        let replay_close = handle_close_payroll_monthly_settlement(
            &state,
            &payroll,
            PayrollMonthlySettlementCloseRequest::default(),
        )
        .expect("replayed monthly close should succeed");
        assert_eq!(
            first_close.exchange_batch.batch_id,
            replay_close.exchange_batch.batch_id
        );

        let dashboard = handle_get_admin_operations_analytics_dashboard(
            &state,
            &committee,
            OperationsAnalyticsDashboardQueryRequest::default(),
        )
        .expect("admin analytics dashboard should be queryable");

        assert_eq!(
            vendor_metric_value(
                &dashboard,
                state.vendor_id.as_str(),
                "payroll_settlement_records_total",
            ),
            first_close.exchange_batch.reconciliation.total_records as f64
        );
        assert_eq!(
            vendor_metric_value(
                &dashboard,
                state.vendor_id.as_str(),
                "payroll_disputed_records_total",
            ),
            first_close.exchange_batch.reconciliation.disputed_records as f64
        );
        assert_eq!(
            vendor_metric_value(
                &dashboard,
                state.vendor_id.as_str(),
                "payroll_deduction_failed_records_total",
            ),
            first_close
                .exchange_batch
                .reconciliation
                .deduction_failed_records as f64
        );
    }

    #[test]
    fn advanced_analytics_hr_sync_replay_uses_persisted_batch_status() {
        let now_epoch_day = current_taipei_business_moment()
            .expect("current time should resolve for test")
            .epoch_day();
        let state = build_state_with_advanced_analytics_runtime(now_epoch_day, true);
        let committee = committee_admin();
        let payroll = payroll_operator();

        seed_previous_pay_period_payroll_record(
            &state,
            now_epoch_day,
            "ord-analytics-hr-sync-replay",
            9_500,
        );
        let closed = handle_close_payroll_monthly_settlement(
            &state,
            &payroll,
            PayrollMonthlySettlementCloseRequest::default(),
        )
        .expect("monthly close should succeed");
        let batch_id = closed.exchange_batch.batch_id.clone();

        let initial_sync = handle_sync_payroll_hr_api_adjunct(
            &state,
            &payroll,
            batch_id.clone(),
            PayrollHrApiSyncRequest {
                outcome: PayrollHrApiSyncOutcomePayload::Succeeded,
                note: Some("initial successful sync".to_owned()),
            },
        )
        .expect("first hr sync should succeed");
        assert_eq!(initial_sync.exchange_batch.hr_api_sync_status, "SUCCEEDED");

        let replay_sync = handle_sync_payroll_hr_api_adjunct(
            &state,
            &payroll,
            batch_id,
            PayrollHrApiSyncRequest {
                outcome: PayrollHrApiSyncOutcomePayload::Failed,
                note: Some("replayed request should not change persisted status".to_owned()),
            },
        )
        .expect("replayed hr sync should remain idempotent");
        assert_eq!(replay_sync.exchange_batch.hr_api_sync_status, "SUCCEEDED");

        let dashboard = handle_get_admin_operations_analytics_dashboard(
            &state,
            &committee,
            OperationsAnalyticsDashboardQueryRequest::default(),
        )
        .expect("admin analytics dashboard should be queryable");
        assert_eq!(
            vendor_metric_value(
                &dashboard,
                state.vendor_id.as_str(),
                "payroll_hr_sync_failed_total",
            ),
            0.0
        );
    }

    #[test]
    fn rush_reminder_policy_resolution_defaults_when_runtime_is_disabled() {
        let policy = resolve_rush_reminder_policy(false)
            .expect("disabled runtime should not require parsing reminder policy env");
        assert_eq!(policy, RushReminderPolicy::default());
    }

    #[test]
    fn rush_reminder_preferences_endpoint_is_unavailable_when_feature_flag_is_off() {
        let now_epoch_day = 300;
        let state = build_state(now_epoch_day);

        let error = handle_upsert_employee_rush_reminder_preferences(
            &state,
            EmployeeRushReminderPreferencesUpsertRequest {
                plant_id: "fab-a".to_owned(),
                preorder_open_enabled: false,
                demand_spike_enabled: false,
            },
        )
        .expect_err("feature-disabled reminder preference endpoint should return 404");
        assert_eq!(error.0, StatusCode::NOT_FOUND);
        assert_eq!(error.1.code, "NOT_FOUND");

        let persisted = state
            .rush_reminder_workflow
            .preferences_for(&actor_id(LOAD_GATE_EMPLOYEE_ACTOR_ID))
            .expect("feature-disabled upsert should not mutate reminder preferences");
        assert!(persisted.preorder_open_enabled());
        assert!(persisted.demand_spike_enabled());
    }

    #[test]
    fn rush_reminder_preferences_are_upserted_via_runtime_handler() {
        let now_epoch_day = 300;
        let state = build_state_with_rush_reminder_runtime(now_epoch_day, true);

        let payload = handle_upsert_employee_rush_reminder_preferences(
            &state,
            EmployeeRushReminderPreferencesUpsertRequest {
                plant_id: "fab-a".to_owned(),
                preorder_open_enabled: false,
                demand_spike_enabled: true,
            },
        )
        .expect("preference upsert should succeed");
        assert_eq!(payload.employee_actor_id, LOAD_GATE_EMPLOYEE_ACTOR_ID);
        assert_eq!(payload.plant_id, "fab-a");
        assert!(!payload.preorder_open_enabled);
        assert!(payload.demand_spike_enabled);

        let persisted = state
            .rush_reminder_workflow
            .preferences_for(&actor_id(LOAD_GATE_EMPLOYEE_ACTOR_ID))
            .expect("persisted reminder preferences should be queryable");
        assert!(!persisted.preorder_open_enabled());
        assert!(persisted.demand_spike_enabled());
    }

    #[test]
    fn rush_reminder_preferences_reject_unsupported_plant() {
        let now_epoch_day = 300;
        let state = build_state_with_rush_reminder_runtime(now_epoch_day, true);
        let error = handle_upsert_employee_rush_reminder_preferences(
            &state,
            EmployeeRushReminderPreferencesUpsertRequest {
                plant_id: "fab-b".to_owned(),
                preorder_open_enabled: false,
                demand_spike_enabled: false,
            },
        )
        .expect_err("unsupported plant should be rejected");
        assert_eq!(error.0, StatusCode::BAD_REQUEST);
        assert_eq!(error.1.code, "UNSUPPORTED_PLANT_ID");
    }

    #[test]
    fn recommendation_ranking_is_deterministic_when_feature_flag_enabled() {
        let now_epoch_day = 300;
        let state = build_state_with_recommendation_runtime(now_epoch_day, true);
        let query = EmployeeMenuDiscoveryQuery {
            plant_id: Some("fab-a".to_owned()),
            view: Some(MenuDiscoveryViewQuery::Week),
            menu_date: Some(epoch_day_to_iso_date(now_epoch_day)),
            sort_by: Some(MenuSortFieldQuery::RemainingQuantity),
            sort_order: Some(SortOrderQuery::Desc),
            ..EmployeeMenuDiscoveryQuery::default()
        };

        let response_a =
            handle_list_employee_menus_at(&state, query, taipei_moment(now_epoch_day, 600))
                .expect("discovery request should succeed");
        let query = EmployeeMenuDiscoveryQuery {
            plant_id: Some("fab-a".to_owned()),
            view: Some(MenuDiscoveryViewQuery::Week),
            menu_date: Some(epoch_day_to_iso_date(now_epoch_day)),
            sort_by: Some(MenuSortFieldQuery::RemainingQuantity),
            sort_order: Some(SortOrderQuery::Desc),
            ..EmployeeMenuDiscoveryQuery::default()
        };
        let response_b =
            handle_list_employee_menus_at(&state, query, taipei_moment(now_epoch_day, 600))
                .expect("discovery request should succeed");

        assert!(response_a.recommendation_requested);
        assert!(response_a.recommendation_applied);
        assert_eq!(
            response_a
                .items
                .iter()
                .map(|item| item.menu_item_id.clone())
                .collect::<Vec<_>>(),
            response_b
                .items
                .iter()
                .map(|item| item.menu_item_id.clone())
                .collect::<Vec<_>>(),
            "recommendation ranking must remain deterministic"
        );
    }

    #[test]
    fn recommendation_failure_does_not_block_ordering_flow() {
        let now_epoch_day = current_taipei_business_moment()
            .expect("current time should resolve for recommendation failure test")
            .epoch_day();
        let mut state = build_state_with_recommendation_runtime(now_epoch_day, true);
        state.menu_recommendation_ranker = failing_menu_recommendation_ranker;

        let discovery_query = EmployeeMenuDiscoveryQuery {
            plant_id: Some("fab-a".to_owned()),
            view: Some(MenuDiscoveryViewQuery::Week),
            menu_date: Some(epoch_day_to_iso_date(now_epoch_day)),
            ..EmployeeMenuDiscoveryQuery::default()
        };
        let discovery_response = handle_list_employee_menus_at(
            &state,
            discovery_query,
            taipei_moment(now_epoch_day, 600),
        )
        .expect("discovery should degrade gracefully when recommendations fail");

        assert!(discovery_response.recommendation_requested);
        assert!(!discovery_response.recommendation_applied);
        assert!(
            !discovery_response.items.is_empty(),
            "discovery should still return deterministic items"
        );

        let create_request = EmployeeOrderCreateRequestPayload {
            plant_id: "fab-a".to_owned(),
            delivery_date: epoch_day_to_iso_date(now_epoch_day.saturating_add(2)),
            line_items: vec![OrderLineItemRequestPayload {
                menu_item_id: "menu-discoverytsta1".to_owned(),
                quantity: 1,
                special_requests: vec![],
            }],
            employee_note: None,
        };
        let created = handle_create_employee_order(&state, create_request)
            .expect("ordering flow should remain available");
        assert_eq!(created.status, "PENDING");
    }

    #[test]
    fn rush_reminder_scheduling_supports_preorder_open_and_demand_spike() {
        let now_epoch_day = 300;
        let state = build_state_with_rush_reminder_runtime(now_epoch_day, true);
        let query = EmployeeMenuDiscoveryQuery {
            plant_id: Some("fab-a".to_owned()),
            view: Some(MenuDiscoveryViewQuery::Week),
            menu_date: Some(epoch_day_to_iso_date(now_epoch_day)),
            ..EmployeeMenuDiscoveryQuery::default()
        };

        let response =
            handle_list_employee_menus_at(&state, query, taipei_moment(now_epoch_day, 600))
                .expect("discovery should succeed with rush reminders enabled");
        assert!(!response.items.is_empty());

        let delivered = state
            .rush_reminder_workflow
            .delivered_notifications()
            .expect("delivered reminders should be queryable");
        let scenarios = delivered
            .iter()
            .filter(|notification| notification.menu_item_id().as_str() == "menu-discoverytsta1")
            .map(|notification| notification.scenario())
            .collect::<BTreeSet<_>>();
        assert!(
            scenarios.contains(&RushReminderScenario::PreorderOpen),
            "preorder-open reminder should be scheduled"
        );
        assert!(
            scenarios.contains(&RushReminderScenario::DemandSpike),
            "demand-spike reminder should be scheduled"
        );
    }

    #[test]
    fn rush_reminder_preferences_enforce_opt_out() {
        let now_epoch_day = 300;
        let state = build_state_with_rush_reminder_runtime(now_epoch_day, true);
        state
            .rush_reminder_workflow
            .upsert_preferences(
                actor_id(LOAD_GATE_EMPLOYEE_ACTOR_ID),
                RushReminderPreferences::new(false, false),
            )
            .expect("reminder preference opt-out should persist");
        let query = EmployeeMenuDiscoveryQuery {
            plant_id: Some("fab-a".to_owned()),
            view: Some(MenuDiscoveryViewQuery::Week),
            menu_date: Some(epoch_day_to_iso_date(now_epoch_day)),
            ..EmployeeMenuDiscoveryQuery::default()
        };

        handle_list_employee_menus_at(&state, query, taipei_moment(now_epoch_day, 600))
            .expect("discovery should still succeed when reminders are opted out");
        let delivered = state
            .rush_reminder_workflow
            .delivered_notifications()
            .expect("delivered reminders should be queryable");
        assert!(
            delivered.is_empty(),
            "opted-out actor should not receive rush reminders"
        );
    }

    #[test]
    fn rush_reminder_policy_throttles_repeated_scheduling() {
        let now_epoch_day = 300;
        let state = build_state_with_rush_reminder_runtime(now_epoch_day, true);
        let entries = state
            .menu_supply_policy
            .employee_discovery_snapshot(
                &BTreeSet::from([vendor_id("ven-discoverytst-a1")]),
                taipei_moment(now_epoch_day, 600),
            )
            .expect("discovery snapshot should be queryable");
        let subscriber_actor_ids = HashSet::from([actor_id(LOAD_GATE_EMPLOYEE_ACTOR_ID)]);

        let first_schedule = state
            .rush_reminder_workflow
            .schedule_from_discovery(
                true,
                &subscriber_actor_ids,
                &entries,
                taipei_moment(now_epoch_day, 600),
            )
            .expect("first reminder schedule should succeed");
        assert!(first_schedule.scheduled_count > 0);

        state
            .rush_reminder_workflow
            .dispatch_pending(
                true,
                state.rush_reminder_delivery_gateway.as_ref(),
                taipei_moment(now_epoch_day, 600),
            )
            .expect("first reminder dispatch should succeed");

        let second_schedule = state
            .rush_reminder_workflow
            .schedule_from_discovery(
                true,
                &subscriber_actor_ids,
                &entries,
                taipei_moment(now_epoch_day, 620),
            )
            .expect("second reminder schedule should succeed");
        assert_eq!(
            second_schedule.scheduled_count, 0,
            "throttled schedules must not enqueue duplicate reminders"
        );
        assert!(
            second_schedule.throttled_count > 0,
            "throttling should account for repeated reminder candidates"
        );
    }

    #[test]
    fn rush_reminder_delivery_failures_do_not_block_ordering() {
        let now_epoch_day = current_taipei_business_moment()
            .expect("current time should resolve for rush reminder failure test")
            .epoch_day();
        let mut state = build_state_with_rush_reminder_runtime(now_epoch_day, true);
        state.rush_reminder_delivery_gateway = Arc::new(FailingRushReminderDeliveryGateway);
        let query = EmployeeMenuDiscoveryQuery {
            plant_id: Some("fab-a".to_owned()),
            view: Some(MenuDiscoveryViewQuery::Week),
            menu_date: Some(epoch_day_to_iso_date(now_epoch_day)),
            ..EmployeeMenuDiscoveryQuery::default()
        };

        handle_list_employee_menus_at(&state, query, taipei_moment(now_epoch_day, 600))
            .expect("discovery should remain available when reminder delivery fails");
        let failure_count = state
            .rush_reminder_workflow
            .delivery_failures()
            .expect("reminder failures should be queryable")
            .len();
        assert!(
            failure_count > 0,
            "failing delivery gateway should record reminder delivery failures"
        );

        let create_request = EmployeeOrderCreateRequestPayload {
            plant_id: "fab-a".to_owned(),
            delivery_date: epoch_day_to_iso_date(now_epoch_day.saturating_add(3)),
            line_items: vec![OrderLineItemRequestPayload {
                menu_item_id: "menu-discoverytsta2".to_owned(),
                quantity: 1,
                special_requests: vec![],
            }],
            employee_note: None,
        };
        let created =
            handle_create_employee_order(&state, create_request).expect("order should be created");
        assert_eq!(created.status, "PENDING");
    }

    #[test]
    fn rush_reminder_delivery_is_isolated_from_order_transaction_path() {
        let now_epoch_day = current_taipei_business_moment()
            .expect("current time should resolve for rush reminder isolation test")
            .epoch_day();
        let mut state = build_state_with_rush_reminder_runtime(now_epoch_day, true);
        state.rush_reminder_delivery_gateway = Arc::new(FailingRushReminderDeliveryGateway);

        let create_request = EmployeeOrderCreateRequestPayload {
            plant_id: "fab-a".to_owned(),
            delivery_date: epoch_day_to_iso_date(now_epoch_day.saturating_add(2)),
            line_items: vec![OrderLineItemRequestPayload {
                menu_item_id: "menu-discoverytsta1".to_owned(),
                quantity: 1,
                special_requests: vec![],
            }],
            employee_note: None,
        };
        handle_create_employee_order(&state, create_request)
            .expect("order creation should remain independent from reminder delivery");

        let delivery_failures = state
            .rush_reminder_workflow
            .delivery_failures()
            .expect("reminder delivery failures should be queryable");
        assert!(
            delivery_failures.is_empty(),
            "ordering should not trigger reminder delivery attempts"
        );
    }

    #[test]
    fn discovery_rejects_missing_plant_id_query_parameter() {
        let now_epoch_day = 300;
        let state = build_state(now_epoch_day);
        let query = EmployeeMenuDiscoveryQuery {
            view: Some(MenuDiscoveryViewQuery::Week),
            menu_date: Some(epoch_day_to_iso_date(now_epoch_day)),
            ..EmployeeMenuDiscoveryQuery::default()
        };

        let error = handle_list_employee_menus(&state, query)
            .expect_err("missing plantId must fail without legacy fallback");
        assert_eq!(error.0, StatusCode::BAD_REQUEST);
        assert_eq!(error.1.code, "INVALID_MENU_DISCOVERY_QUERY");
    }

    #[test]
    fn pickup_verification_accepts_valid_totp_and_marks_order_fulfilled() {
        let now_epoch_day = current_taipei_business_moment()
            .expect("current time should resolve for test")
            .epoch_day();
        let state = build_state(now_epoch_day);
        let create_request = EmployeeOrderCreateRequestPayload {
            plant_id: "fab-a".to_owned(),
            delivery_date: epoch_day_to_iso_date(now_epoch_day.saturating_add(2)),
            line_items: vec![OrderLineItemRequestPayload {
                menu_item_id: "menu-discoverytsta1".to_owned(),
                quantity: 1,
                special_requests: vec![],
            }],
            employee_note: None,
        };
        let created_order = handle_create_employee_order(&state, create_request)
            .expect("create order should succeed for pickup verification");
        let parsed_order_id = parse_contract_order_id(&created_order.order_id)
            .expect("created order id should match contract format");
        let current_step =
            PickupTotpVerifier::current_taipei_step().expect("current step should resolve");
        let verification_code = state
            .pickup_totp_verifier
            .generate_qr_payload(&parsed_order_id, current_step);

        let response = handle_verify_order_pickup(
            &state,
            created_order.order_id.clone(),
            PickupVerificationRequest { verification_code },
            "req-pickup-success",
        )
        .expect("valid TOTP payload should verify successfully");

        assert_eq!(response.order_id, created_order.order_id);
        assert!(response.verified);
        let updated_snapshot = load_order_snapshot_or_not_found(&state, &parsed_order_id)
            .expect("fulfilled order should remain queryable");
        assert_eq!(updated_snapshot.state(), OrderLifecycleState::Fulfilled);
    }

    #[test]
    fn pickup_verification_rejects_expired_totp_code() {
        let now_epoch_day = current_taipei_business_moment()
            .expect("current time should resolve for test")
            .epoch_day();
        let state = build_state(now_epoch_day);
        let create_request = EmployeeOrderCreateRequestPayload {
            plant_id: "fab-a".to_owned(),
            delivery_date: epoch_day_to_iso_date(now_epoch_day.saturating_add(2)),
            line_items: vec![OrderLineItemRequestPayload {
                menu_item_id: "menu-discoverytsta1".to_owned(),
                quantity: 1,
                special_requests: vec![],
            }],
            employee_note: None,
        };
        let created_order = handle_create_employee_order(&state, create_request)
            .expect("create order should succeed for pickup verification");
        let parsed_order_id = parse_contract_order_id(&created_order.order_id)
            .expect("created order id should match contract format");
        let current_step =
            PickupTotpVerifier::current_taipei_step().expect("current step should resolve");
        let expired_step = current_step.saturating_sub(2);
        let verification_code = state
            .pickup_totp_verifier
            .generate_qr_payload(&parsed_order_id, expired_step);

        let error = handle_verify_order_pickup(
            &state,
            created_order.order_id.clone(),
            PickupVerificationRequest { verification_code },
            "req-pickup-expired",
        )
        .expect_err("expired TOTP payload should be rejected");
        assert_eq!(error.0, StatusCode::BAD_REQUEST);
        assert_eq!(error.1.code, "PICKUP_VERIFICATION_EXPIRED");
    }

    #[test]
    fn pickup_verification_rejects_replay_after_successful_claim() {
        let now_epoch_day = current_taipei_business_moment()
            .expect("current time should resolve for test")
            .epoch_day();
        let state = build_state(now_epoch_day);
        let create_request = EmployeeOrderCreateRequestPayload {
            plant_id: "fab-a".to_owned(),
            delivery_date: epoch_day_to_iso_date(now_epoch_day.saturating_add(2)),
            line_items: vec![OrderLineItemRequestPayload {
                menu_item_id: "menu-discoverytsta1".to_owned(),
                quantity: 1,
                special_requests: vec![],
            }],
            employee_note: None,
        };
        let created_order = handle_create_employee_order(&state, create_request)
            .expect("create order should succeed for pickup verification");
        let parsed_order_id = parse_contract_order_id(&created_order.order_id)
            .expect("created order id should match contract format");
        let current_step =
            PickupTotpVerifier::current_taipei_step().expect("current step should resolve");
        let verification_code = state
            .pickup_totp_verifier
            .generate_qr_payload(&parsed_order_id, current_step);

        handle_verify_order_pickup(
            &state,
            created_order.order_id.clone(),
            PickupVerificationRequest {
                verification_code: verification_code.clone(),
            },
            "req-pickup-replay-first",
        )
        .expect("first pickup verification should succeed");

        let replay_error = handle_verify_order_pickup(
            &state,
            created_order.order_id.clone(),
            PickupVerificationRequest { verification_code },
            "req-pickup-replay-second",
        )
        .expect_err("second pickup verification should be rejected as replay");
        assert_eq!(replay_error.0, StatusCode::CONFLICT);
        assert_eq!(replay_error.1.code, "PICKUP_VERIFICATION_REPLAYED");
    }

    #[test]
    fn employee_payroll_ledger_handler_reflects_append_only_adjustments() {
        let now_epoch_day = current_taipei_business_moment()
            .expect("current time should resolve for test")
            .epoch_day();
        let state = build_state(now_epoch_day);
        let create_request = EmployeeOrderCreateRequestPayload {
            plant_id: "fab-a".to_owned(),
            delivery_date: epoch_day_to_iso_date(now_epoch_day.saturating_add(2)),
            line_items: vec![OrderLineItemRequestPayload {
                menu_item_id: "menu-discoverytsta1".to_owned(),
                quantity: 1,
                special_requests: vec![],
            }],
            employee_note: None,
        };
        let created_order =
            handle_create_employee_order(&state, create_request).expect("order should be created");

        let first_ledger =
            handle_get_employee_order_payroll_ledger(&state, created_order.order_id.clone())
                .expect("initial payroll ledger should be queryable");
        assert_eq!(first_ledger.net_amount_minor, 12000);
        assert_eq!(first_ledger.ledger_entries.len(), 1);
        assert_eq!(first_ledger.ledger_entries[0].kind, "DEDUCTION");
        assert!(first_ledger.disputes.is_empty());

        handle_update_employee_order(
            &state,
            created_order.order_id.clone(),
            UpdateOrderRequest {
                operation: "CANCEL".to_owned(),
                line_items: None,
                cancel_reason: Some("cancelled by employee".to_owned()),
            },
        )
        .expect("order cancel should succeed");

        let updated_ledger =
            handle_get_employee_order_payroll_ledger(&state, created_order.order_id.clone())
                .expect("updated payroll ledger should be queryable");
        assert_eq!(updated_ledger.net_amount_minor, 0);
        assert_eq!(updated_ledger.ledger_entries.len(), 2);
        assert_eq!(updated_ledger.ledger_entries[1].kind, "ADJUSTMENT_CREDIT");
    }

    #[test]
    fn dispute_workflow_and_exchange_handlers_expose_traceable_payroll_lifecycle() {
        let now_epoch_day = current_taipei_business_moment()
            .expect("current time should resolve for test")
            .epoch_day();
        let state = build_state(now_epoch_day);
        let payroll = payroll_operator();
        let create_request = EmployeeOrderCreateRequestPayload {
            plant_id: "fab-a".to_owned(),
            delivery_date: epoch_day_to_iso_date(now_epoch_day.saturating_add(2)),
            line_items: vec![OrderLineItemRequestPayload {
                menu_item_id: "menu-discoverytsta1".to_owned(),
                quantity: 1,
                special_requests: vec![],
            }],
            employee_note: None,
        };
        let created_order =
            handle_create_employee_order(&state, create_request).expect("order should be created");

        let opened_dispute = handle_create_employee_order_dispute(
            &state,
            created_order.order_id.clone(),
            EmployeePayrollDisputeCreateRequest {
                reason: "charged despite inventory issue".to_owned(),
            },
        )
        .expect("dispute should be opened");
        assert_eq!(opened_dispute.status, "OPEN");
        assert_eq!(opened_dispute.trace.len(), 1);

        let assigned_dispute = handle_update_admin_payroll_dispute(
            &state,
            opened_dispute.dispute_id.clone(),
            AdminPayrollDisputePatchRequest {
                operation: "ASSIGN_OWNER".to_owned(),
                owner_actor_id: Some("payroll-owner-alpha".to_owned()),
                note: Some("triaged".to_owned()),
                refund_amount_minor: None,
            },
        )
        .expect("owner assignment should succeed");
        assert_eq!(assigned_dispute.status, "IN_REVIEW");
        assert_eq!(assigned_dispute.owner_actor_id, "payroll-owner-alpha");

        let resolved_dispute = handle_update_admin_payroll_dispute(
            &state,
            opened_dispute.dispute_id.clone(),
            AdminPayrollDisputePatchRequest {
                operation: "RESOLVE_REFUND".to_owned(),
                owner_actor_id: None,
                note: Some("approved partial refund".to_owned()),
                refund_amount_minor: Some(6000),
            },
        )
        .expect("refund resolution should succeed");
        assert_eq!(resolved_dispute.status, "RESOLVED_REFUND_APPROVED");
        assert!(resolved_dispute.trace.len() >= 3);

        let pay_period = created_order.delivery_date[..7].to_owned();
        let export_page = handle_export_payroll_deductions(
            &state,
            &payroll,
            PayrollExportQuery {
                pay_period: Some(pay_period),
                cycle_key: Some("cycle-1970-04-primary".to_owned()),
                page: Some(1),
                page_size: Some(20),
                sort_by: Some(PayrollSortFieldQuery::DeliveryDate),
                sort_order: Some(SortOrderQuery::Asc),
            },
        )
        .expect("payroll deductions export should succeed");
        assert_eq!(export_page.exchange_batch.exchange_path, "SFTP_BATCH");
        assert_eq!(export_page.exchange_batch.hr_api_sync_status, "NOT_SYNCED");

        let synced = handle_sync_payroll_hr_api_adjunct(
            &state,
            &payroll,
            export_page.exchange_batch.batch_id.clone(),
            PayrollHrApiSyncRequest {
                outcome: PayrollHrApiSyncOutcomePayload::Succeeded,
                note: None,
            },
        )
        .expect("hr api adjunct sync should succeed");
        assert_eq!(synced.exchange_batch.hr_api_sync_status, "SUCCEEDED");
        assert!(synced.exchange_batch.hr_api_synced_at.is_some());
    }

    #[test]
    fn parse_contract_anomaly_rule_id_enforces_openapi_pattern() {
        let parsed = parse_contract_anomaly_rule_id("rule-expiry-risk")
            .expect("contract-conformant anomaly rule id should parse");
        assert_eq!(parsed.as_str(), "rule-expiry-risk");

        let mut invalid_cases = vec![
            "".to_owned(),
            "rule-".to_owned(),
            "rule-ab".to_owned(),
            "RULE-expiry-risk".to_owned(),
            "rule-expiry_risk".to_owned(),
            "legacy-rule-expiry-risk".to_owned(),
            " rule-expiry-risk".to_owned(),
            "rule-expiry-risk ".to_owned(),
        ];
        invalid_cases.push(format!("rule-{}", "a".repeat(65)));

        for candidate in invalid_cases {
            assert!(
                parse_contract_anomaly_rule_id(&candidate).is_err(),
                "expected `{candidate}` to be rejected by contract rule id parser"
            );
        }
    }

    #[test]
    fn parse_contract_anomaly_alert_id_enforces_openapi_pattern() {
        let parsed = parse_contract_anomaly_alert_id("alt-0123456789abcdef")
            .expect("contract-conformant anomaly alert id should parse");
        assert_eq!(parsed.as_str(), "alt-0123456789abcdef");

        let invalid_cases = vec![
            "".to_owned(),
            "alt-".to_owned(),
            "alt-0123456789abcde".to_owned(),
            "alt-0123456789abcdef0".to_owned(),
            "alt-0123456789abcdeg".to_owned(),
            "ALT-0123456789abcdef".to_owned(),
            " alt-0123456789abcdef".to_owned(),
            "alt-0123456789abcdef ".to_owned(),
        ];
        for candidate in invalid_cases {
            assert!(
                parse_contract_anomaly_alert_id(&candidate).is_err(),
                "expected `{candidate}` to be rejected by contract alert id parser"
            );
        }
    }

    #[test]
    fn anomaly_evaluation_rejects_out_of_contract_metric_ranges() {
        let now_epoch_day = current_taipei_business_moment()
            .expect("current time should resolve for test")
            .epoch_day();
        let state = build_state(now_epoch_day);
        let committee = committee_admin();

        let invalid_cases = vec![
            (
                AnomalyAlertEvaluationRequest {
                    vendor_id: "ven-discoverytst-a1".to_owned(),
                    observed_at_epoch_day: Some(now_epoch_day),
                    observed_at_minute_of_day: Some(600),
                    days_until_expiry: Some(-0.1),
                    on_time_rate: None,
                    satisfaction_score: None,
                    complaint_count: None,
                    default_owner_actor_id: None,
                },
                "daysUntilExpiry must be greater than or equal to 0",
            ),
            (
                AnomalyAlertEvaluationRequest {
                    vendor_id: "ven-discoverytst-a1".to_owned(),
                    observed_at_epoch_day: Some(now_epoch_day),
                    observed_at_minute_of_day: Some(600),
                    days_until_expiry: None,
                    on_time_rate: Some(1.1),
                    satisfaction_score: None,
                    complaint_count: None,
                    default_owner_actor_id: None,
                },
                "onTimeRate must be less than or equal to 1",
            ),
            (
                AnomalyAlertEvaluationRequest {
                    vendor_id: "ven-discoverytst-a1".to_owned(),
                    observed_at_epoch_day: Some(now_epoch_day),
                    observed_at_minute_of_day: Some(600),
                    days_until_expiry: None,
                    on_time_rate: None,
                    satisfaction_score: Some(5.1),
                    complaint_count: None,
                    default_owner_actor_id: None,
                },
                "satisfactionScore must be less than or equal to 5",
            ),
            (
                AnomalyAlertEvaluationRequest {
                    vendor_id: "ven-discoverytst-a1".to_owned(),
                    observed_at_epoch_day: Some(now_epoch_day),
                    observed_at_minute_of_day: Some(600),
                    days_until_expiry: None,
                    on_time_rate: None,
                    satisfaction_score: None,
                    complaint_count: Some(-1.0),
                    default_owner_actor_id: None,
                },
                "complaintCount must be greater than or equal to 0",
            ),
            (
                AnomalyAlertEvaluationRequest {
                    vendor_id: "ven-discoverytst-a1".to_owned(),
                    observed_at_epoch_day: Some(now_epoch_day),
                    observed_at_minute_of_day: Some(600),
                    days_until_expiry: None,
                    on_time_rate: Some(f64::NAN),
                    satisfaction_score: None,
                    complaint_count: None,
                    default_owner_actor_id: None,
                },
                "onTimeRate must be a finite number",
            ),
        ];

        for (request, expected_message) in invalid_cases {
            let error = handle_evaluate_anomaly_alerts(&state, &committee, request)
                .expect_err("out-of-contract anomaly metrics should be rejected");
            assert_eq!(error.0, StatusCode::BAD_REQUEST);
            assert!(
                error.1.message.contains(expected_message),
                "expected error message to contain `{expected_message}`, got `{}`",
                error.1.message
            );
        }
    }

    #[test]
    fn close_anomaly_alert_rejects_blank_evidence_without_persisting_closed_status() {
        let now_epoch_day = current_taipei_business_moment()
            .expect("current time should resolve for test")
            .epoch_day();
        let state = build_state(now_epoch_day);
        let committee = committee_admin();

        let evaluation = handle_evaluate_anomaly_alerts(
            &state,
            &committee,
            AnomalyAlertEvaluationRequest {
                vendor_id: "ven-discoverytst-a1".to_owned(),
                observed_at_epoch_day: Some(now_epoch_day),
                observed_at_minute_of_day: Some(600),
                days_until_expiry: None,
                on_time_rate: Some(0.80),
                satisfaction_score: None,
                complaint_count: None,
                default_owner_actor_id: None,
            },
        )
        .expect("anomaly evaluation should succeed");
        let initial_alert = evaluation
            .triggered_alerts
            .iter()
            .find(|alert| alert.rule_kind == "ON_TIME_DEGRADATION")
            .expect("on-time degradation should trigger an alert");

        let close_error = handle_update_admin_anomaly_alert(
            &state,
            &committee,
            initial_alert.alert_id.clone(),
            AdminAnomalyAlertPatchRequest {
                operation: "CLOSE".to_owned(),
                owner_actor_id: None,
                note: Some("attempted close".to_owned()),
                closure_note: Some("closure evidence pending".to_owned()),
                closure_evidence_refs: Some(vec!["   ".to_owned()]),
                ticket_reference: None,
            },
        )
        .expect_err("blank closure evidence must be rejected");
        assert_eq!(close_error.0, StatusCode::BAD_REQUEST);
        assert!(
            close_error.1.message.contains("closureEvidenceRefs"),
            "expected close error to reference closureEvidenceRefs, got `{}`",
            close_error.1.message
        );

        let open_listing = handle_list_anomaly_alerts(
            &state,
            &committee,
            AnomalyAlertQueryRequest {
                vendor_id: Some("ven-discoverytst-a1".to_owned()),
                owner_actor_id: None,
                status: Some("OPEN".to_owned()),
                escalated_only: None,
                sla_status: None,
                as_of_epoch_day: Some(now_epoch_day),
                as_of_minute_of_day: Some(1439),
            },
        )
        .expect("open anomaly alerts should be queryable");
        assert!(
            open_listing
                .items
                .iter()
                .any(|item| item.alert_id == initial_alert.alert_id),
            "failed close must not persist CLOSED status"
        );

        let close_audit_events = handle_query_audit_investigations(
            &state,
            &committee,
            AuditInvestigationQuery {
                actor_id: None,
                action: Some("CLOSE_ANOMALY_ALERT".to_owned()),
                entity_type: Some("ANOMALY_ALERT".to_owned()),
                entity_id: Some(initial_alert.alert_id.clone()),
                occurred_from_epoch_day: None,
                occurred_to_epoch_day: None,
                correlation_id: None,
            },
        )
        .expect("close anomaly audit events should be queryable");
        assert!(
            close_audit_events.items.is_empty(),
            "failed close must not append close audit evidence"
        );
    }

    #[test]
    fn anomaly_alert_workflow_handlers_track_owner_and_closure_auditability() {
        let now_epoch_day = current_taipei_business_moment()
            .expect("current time should resolve for test")
            .epoch_day();
        let state = build_state(now_epoch_day);
        let committee = committee_admin();

        let evaluation = handle_evaluate_anomaly_alerts(
            &state,
            &committee,
            AnomalyAlertEvaluationRequest {
                vendor_id: "ven-discoverytst-a1".to_owned(),
                observed_at_epoch_day: Some(now_epoch_day),
                observed_at_minute_of_day: Some(600),
                days_until_expiry: None,
                on_time_rate: Some(0.80),
                satisfaction_score: None,
                complaint_count: None,
                default_owner_actor_id: None,
            },
        )
        .expect("anomaly evaluation should succeed");
        let initial_alert = evaluation
            .triggered_alerts
            .iter()
            .find(|alert| alert.rule_kind == "ON_TIME_DEGRADATION")
            .expect("on-time degradation should trigger an alert");
        assert_eq!(initial_alert.status, "OPEN");
        assert_eq!(
            initial_alert.owner_actor_id,
            LOAD_GATE_ANOMALY_ALERT_OWNER_ACTOR_ID
        );

        let assigned_alert = handle_update_admin_anomaly_alert(
            &state,
            &committee,
            initial_alert.alert_id.clone(),
            AdminAnomalyAlertPatchRequest {
                operation: "ASSIGN_OWNER".to_owned(),
                owner_actor_id: Some("committee-owner-alpha".to_owned()),
                note: Some("triaged by governance committee".to_owned()),
                closure_note: None,
                closure_evidence_refs: None,
                ticket_reference: None,
            },
        )
        .expect("anomaly alert owner assignment should succeed");
        assert_eq!(assigned_alert.owner_actor_id, "committee-owner-alpha");

        let in_progress_alert = handle_update_admin_anomaly_alert(
            &state,
            &committee,
            initial_alert.alert_id.clone(),
            AdminAnomalyAlertPatchRequest {
                operation: "START_REMEDIATION".to_owned(),
                owner_actor_id: None,
                note: Some("remediation started".to_owned()),
                closure_note: None,
                closure_evidence_refs: None,
                ticket_reference: None,
            },
        )
        .expect("anomaly alert remediation transition should succeed");
        assert_eq!(in_progress_alert.status, "REMEDIATION_IN_PROGRESS");

        let closed_alert = handle_update_admin_anomaly_alert(
            &state,
            &committee,
            initial_alert.alert_id.clone(),
            AdminAnomalyAlertPatchRequest {
                operation: "CLOSE".to_owned(),
                owner_actor_id: None,
                note: Some("closure approved".to_owned()),
                closure_note: Some("mitigated with vendor retraining and monitoring".to_owned()),
                closure_evidence_refs: Some(vec![
                    "runbook://anomaly/on-time-degradation".to_owned(),
                    "evidence://vendor/ven-discoverytst-a1/2026-04-16".to_owned(),
                ]),
                ticket_reference: Some("jira://OPS-42".to_owned()),
            },
        )
        .expect("anomaly alert close should succeed");
        assert_eq!(closed_alert.status, "CLOSED");
        assert_eq!(
            closed_alert.ticket_reference.as_deref(),
            Some("jira://OPS-42")
        );
        assert_eq!(closed_alert.closure_evidence_refs.len(), 2);

        let closed_listing = handle_list_anomaly_alerts(
            &state,
            &committee,
            AnomalyAlertQueryRequest {
                vendor_id: Some("ven-discoverytst-a1".to_owned()),
                owner_actor_id: Some("committee-owner-alpha".to_owned()),
                status: Some("CLOSED".to_owned()),
                escalated_only: None,
                sla_status: None,
                as_of_epoch_day: Some(now_epoch_day),
                as_of_minute_of_day: Some(1439),
            },
        )
        .expect("closed anomaly alerts should be queryable");
        assert_eq!(closed_listing.items.len(), 1);
        assert_eq!(closed_listing.items[0].alert_id, initial_alert.alert_id);

        let investigations = handle_query_audit_investigations(
            &state,
            &committee,
            AuditInvestigationQuery {
                actor_id: None,
                action: Some("CLOSE_ANOMALY_ALERT".to_owned()),
                entity_type: Some("ANOMALY_ALERT".to_owned()),
                entity_id: Some(initial_alert.alert_id.clone()),
                occurred_from_epoch_day: None,
                occurred_to_epoch_day: None,
                correlation_id: None,
            },
        )
        .expect("anomaly close event should be auditable");
        assert_eq!(investigations.items.len(), 1);
        assert_eq!(investigations.items[0].action, "CLOSE_ANOMALY_ALERT");
        assert_eq!(investigations.items[0].entity_type, "ANOMALY_ALERT");
    }

    #[test]
    fn payroll_export_handler_can_emit_terminated_employee_exception_status() {
        let now_epoch_day = current_taipei_business_moment()
            .expect("current time should resolve for test")
            .epoch_day();
        let state = build_state_with_terminated_load_gate_employee(now_epoch_day);
        let payroll = payroll_operator();
        let create_request = EmployeeOrderCreateRequestPayload {
            plant_id: "fab-a".to_owned(),
            delivery_date: epoch_day_to_iso_date(now_epoch_day.saturating_add(2)),
            line_items: vec![OrderLineItemRequestPayload {
                menu_item_id: "menu-discoverytsta1".to_owned(),
                quantity: 1,
                special_requests: vec![],
            }],
            employee_note: None,
        };
        let created_order =
            handle_create_employee_order(&state, create_request).expect("order should be created");
        let pay_period = created_order.delivery_date[..7].to_owned();

        let export_page = handle_export_payroll_deductions(
            &state,
            &payroll,
            PayrollExportQuery {
                pay_period: Some(pay_period),
                cycle_key: Some("cycle-terminated-runtime-drill".to_owned()),
                page: Some(1),
                page_size: Some(20),
                sort_by: Some(PayrollSortFieldQuery::DeliveryDate),
                sort_order: Some(SortOrderQuery::Asc),
            },
        )
        .expect("payroll deductions export should succeed");

        let exported = export_page
            .items
            .iter()
            .find(|item| {
                state
                    .payroll_export_field_encryptor
                    .decrypt_field(&item.order_id_ciphertext)
                    .expect("encrypted order id should decrypt")
                    == created_order.order_id
            })
            .expect("created order should exist in exported deductions");
        assert_ne!(
            exported.order_id_ciphertext, created_order.order_id,
            "order id must not be exposed in plaintext"
        );
        assert_eq!(exported.status, "EMPLOYEE_TERMINATED");
    }

    #[test]
    fn payroll_export_payload_encrypts_sensitive_record_fields() {
        let now_epoch_day = current_taipei_business_moment()
            .expect("current time should resolve for test")
            .epoch_day();
        let state = build_state(now_epoch_day);
        let payroll = payroll_operator();
        let create_request = EmployeeOrderCreateRequestPayload {
            plant_id: "fab-a".to_owned(),
            delivery_date: epoch_day_to_iso_date(now_epoch_day.saturating_add(2)),
            line_items: vec![OrderLineItemRequestPayload {
                menu_item_id: "menu-discoverytsta1".to_owned(),
                quantity: 1,
                special_requests: vec![],
            }],
            employee_note: None,
        };
        let created_order =
            handle_create_employee_order(&state, create_request).expect("order should be created");
        let pay_period = created_order.delivery_date[..7].to_owned();

        let export_page = handle_export_payroll_deductions(
            &state,
            &payroll,
            PayrollExportQuery {
                pay_period: Some(pay_period),
                cycle_key: Some("cycle-encryption-evidence-runtime".to_owned()),
                page: Some(1),
                page_size: Some(20),
                sort_by: Some(PayrollSortFieldQuery::DeliveryDate),
                sort_order: Some(SortOrderQuery::Asc),
            },
        )
        .expect("payroll deductions export should succeed");

        let exported = export_page
            .items
            .first()
            .expect("at least one payroll deduction record should exist");
        assert!(
            exported.employee_actor_ciphertext.contains(':'),
            "employee actor must be envelope ciphertext"
        );
        assert!(
            exported.order_id_ciphertext.contains(':'),
            "order id must be envelope ciphertext"
        );
        assert!(
            exported.amount_ciphertext.contains(':'),
            "amount must be envelope ciphertext"
        );

        let decrypted_employee = state
            .payroll_export_field_encryptor
            .decrypt_field(&exported.employee_actor_ciphertext)
            .expect("employee actor ciphertext should decrypt");
        let decrypted_order_id = state
            .payroll_export_field_encryptor
            .decrypt_field(&exported.order_id_ciphertext)
            .expect("order id ciphertext should decrypt");
        let decrypted_amount = state
            .payroll_export_field_encryptor
            .decrypt_field(&exported.amount_ciphertext)
            .expect("amount ciphertext should decrypt");
        let decrypted_amount: serde_json::Value =
            serde_json::from_str(&decrypted_amount).expect("amount payload should deserialize");

        assert_eq!(decrypted_employee, LOAD_GATE_EMPLOYEE_ACTOR_ID);
        assert_eq!(decrypted_order_id, created_order.order_id);
        assert_eq!(decrypted_amount["currency"], "TWD");
        assert_eq!(decrypted_amount["amountMinor"], 12000);
    }

    #[test]
    fn monthly_settlement_close_handler_uses_previous_taipei_cycle_defaults() {
        let now_epoch_day = current_taipei_business_moment()
            .expect("current time should resolve for test")
            .epoch_day();
        let state = build_state(now_epoch_day);
        let payroll = payroll_operator();

        let closed = handle_close_payroll_monthly_settlement(
            &state,
            &payroll,
            PayrollMonthlySettlementCloseRequest::default(),
        )
        .expect("monthly close should succeed");

        let expected_pay_period = previous_pay_period_for_epoch_day(now_epoch_day);
        assert_eq!(closed.exchange_batch.pay_period, expected_pay_period);
        assert_eq!(
            closed.exchange_batch.cycle_key,
            format!("monthly-{}", expected_pay_period)
        );
        assert_eq!(closed.exchange_batch.exchange_path, "SFTP_BATCH");
        assert_eq!(
            closed
                .exchange_batch
                .reconciliation
                .required_exception_classes,
            vec![
                "DISPUTED".to_owned(),
                "DEDUCTION_FAILED".to_owned(),
                "EMPLOYEE_TERMINATED".to_owned(),
                "REFUNDED".to_owned()
            ]
        );
    }

    #[test]
    fn payroll_settlement_and_export_reject_page_size_above_openapi_limit() {
        let now_epoch_day = current_taipei_business_moment()
            .expect("current time should resolve for test")
            .epoch_day();
        let state = build_state(now_epoch_day);
        let payroll = payroll_operator();

        let export_error = handle_export_payroll_deductions(
            &state,
            &payroll,
            PayrollExportQuery {
                pay_period: Some("1970-04".to_owned()),
                cycle_key: Some("cycle-page-size-guard".to_owned()),
                page: Some(1),
                page_size: Some(201),
                sort_by: Some(PayrollSortFieldQuery::DeliveryDate),
                sort_order: Some(SortOrderQuery::Asc),
            },
        )
        .expect_err("integration export should reject pageSize above OpenAPI max");
        assert_eq!(export_error.0, StatusCode::BAD_REQUEST);

        let close_error = handle_close_payroll_monthly_settlement(
            &state,
            &payroll,
            PayrollMonthlySettlementCloseRequest {
                page_size: Some(201),
                ..PayrollMonthlySettlementCloseRequest::default()
            },
        )
        .expect_err("monthly close should reject pageSize above OpenAPI max");
        assert_eq!(close_error.0, StatusCode::BAD_REQUEST);
    }

    #[test]
    fn settlement_cycle_lock_handlers_require_reason_and_toggle_lock_state() {
        let now_epoch_day = current_taipei_business_moment()
            .expect("current time should resolve for test")
            .epoch_day();
        let state = build_state(now_epoch_day);
        let committee = committee_admin();
        let payroll = payroll_operator();

        let closed = handle_close_payroll_monthly_settlement(
            &state,
            &payroll,
            PayrollMonthlySettlementCloseRequest::default(),
        )
        .expect("monthly close should succeed");
        let cycle_key = closed.exchange_batch.cycle_key.clone();

        let unlock_error = handle_unlock_payroll_settlement_cycle(
            &state,
            &committee,
            cycle_key.clone(),
            PayrollSettlementCycleLockRequest::default(),
        )
        .expect_err("unlock without reason should fail");
        assert_eq!(unlock_error.0, StatusCode::BAD_REQUEST);

        let unlocked = handle_unlock_payroll_settlement_cycle(
            &state,
            &committee,
            cycle_key.clone(),
            PayrollSettlementCycleLockRequest {
                reason: Some("authorized recompute for corrected totals".to_owned()),
            },
        )
        .expect("unlock with reason should succeed");
        assert_eq!(unlocked.settlement_cycle.lock_state, "UNLOCKED");

        let lock_error = handle_lock_payroll_settlement_cycle(
            &state,
            &committee,
            cycle_key.clone(),
            PayrollSettlementCycleLockRequest::default(),
        )
        .expect_err("lock without reason should fail");
        assert_eq!(lock_error.0, StatusCode::BAD_REQUEST);

        let locked = handle_lock_payroll_settlement_cycle(
            &state,
            &committee,
            cycle_key,
            PayrollSettlementCycleLockRequest {
                reason: Some("manual governance relock".to_owned()),
            },
        )
        .expect("lock with reason should succeed");
        assert_eq!(locked.settlement_cycle.lock_state, "LOCKED");
    }

    #[test]
    fn mcp_and_http_verification_paths_share_validation_error_codes() {
        let now_epoch_day = current_taipei_business_moment()
            .expect("current time should resolve for test")
            .epoch_day();
        let state = build_state(now_epoch_day);
        let create_request = EmployeeOrderCreateRequestPayload {
            plant_id: "fab-a".to_owned(),
            delivery_date: epoch_day_to_iso_date(now_epoch_day.saturating_add(2)),
            line_items: vec![OrderLineItemRequestPayload {
                menu_item_id: "menu-discoverytsta1".to_owned(),
                quantity: 1,
                special_requests: vec![],
            }],
            employee_note: None,
        };
        let created_order = handle_create_employee_order(&state, create_request)
            .expect("create order should succeed for verification parity");

        let http_error = handle_verify_order_pickup(
            &state,
            created_order.order_id.clone(),
            PickupVerificationRequest {
                verification_code: "   ".to_owned(),
            },
            "http-verify-parity",
        )
        .expect_err("http pickup verification should reject empty verificationCode");

        let service_account = oauth_service_account_actor(
            "svc-verify-parity",
            Role::Employee,
            PlantScope::restricted(vec![plant_id("fab-a")]).expect("scope should be valid"),
        );
        let grant = McpServiceAccountGrant::new(
            service_account.actor_id().clone(),
            service_account,
            [MCP_TOOL_VERIFICATION_VERIFY_PICKUP_TOTP],
        )
        .expect("verification grant should be valid");
        let mcp_error = invoke_mcp_write_for_test(
            &state,
            &grant,
            MCP_TOOL_VERIFICATION_VERIFY_PICKUP_TOTP,
            serde_json::json!({
                "orderId": created_order.order_id,
                "request": {
                    "verificationCode": "   "
                }
            }),
        )
        .expect_err("mcp pickup verification should reject empty verificationCode");

        assert_eq!(http_error.0, mcp_error.0);
        assert_eq!(http_error.1.code, mcp_error.1.code);
    }

    #[test]
    fn mcp_and_http_compliance_review_paths_share_validation_error_codes() {
        let now_epoch_day = current_taipei_business_moment()
            .expect("current time should resolve for test")
            .epoch_day();
        let state = build_state(now_epoch_day);
        let committee = committee_admin();

        let http_error = handle_review_vendor_application(
            &state,
            &committee,
            "ven-discoverytst-a1".to_owned(),
            VendorApplicationReviewRequest {
                decision: "INVALID".to_owned(),
                comment: "short".to_owned(),
                decided_on_epoch_day: now_epoch_day,
            },
        )
        .expect_err("http compliance review should reject unsupported decision");

        let service_account = oauth_service_account_actor(
            "svc-compliance-parity",
            Role::CommitteeAdmin,
            PlantScope::all(),
        );
        let grant = McpServiceAccountGrant::new(
            service_account.actor_id().clone(),
            service_account,
            [MCP_TOOL_COMPLIANCE_REVIEW_VENDOR_APPLICATION],
        )
        .expect("compliance review grant should be valid");
        let mcp_error = invoke_mcp_write_for_test(
            &state,
            &grant,
            MCP_TOOL_COMPLIANCE_REVIEW_VENDOR_APPLICATION,
            serde_json::json!({
                "vendorId": "ven-discoverytst-a1",
                "decision": "INVALID",
                "comment": "short",
                "decidedOnEpochDay": now_epoch_day
            }),
        )
        .expect_err("mcp compliance review should reject unsupported decision");

        assert_eq!(http_error.0, mcp_error.0);
        assert_eq!(http_error.1.code, mcp_error.1.code);
    }

    #[test]
    fn mcp_and_http_settlement_paths_share_validation_error_codes() {
        let now_epoch_day = current_taipei_business_moment()
            .expect("current time should resolve for test")
            .epoch_day();
        let state = build_state(now_epoch_day);
        let payroll = payroll_operator();
        let http_query = PayrollExportQuery {
            pay_period: None,
            cycle_key: Some("cycle-missing-pay-period".to_owned()),
            page: Some(1),
            page_size: Some(20),
            sort_by: Some(PayrollSortFieldQuery::DeliveryDate),
            sort_order: Some(SortOrderQuery::Asc),
        };

        let http_error = handle_export_payroll_deductions(&state, &payroll, http_query)
            .expect_err("http settlement export should require payPeriod");

        let service_account = oauth_service_account_actor(
            "svc-settlement-parity",
            Role::PayrollOperator,
            PlantScope::all(),
        );
        let grant = McpServiceAccountGrant::new(
            service_account.actor_id().clone(),
            service_account,
            [MCP_TOOL_SETTLEMENT_EXPORT_PAYROLL_DEDUCTIONS],
        )
        .expect("settlement export grant should be valid");
        let mcp_error = invoke_mcp_write_for_test(
            &state,
            &grant,
            MCP_TOOL_SETTLEMENT_EXPORT_PAYROLL_DEDUCTIONS,
            serde_json::json!({
                "payPeriod": null,
                "cycleKey": "cycle-missing-pay-period",
                "page": 1,
                "pageSize": 20,
                "sortBy": "deliveryDate",
                "sortOrder": "asc"
            }),
        )
        .expect_err("mcp settlement export should require payPeriod");

        assert_eq!(http_error.0, mcp_error.0);
        assert_eq!(http_error.1.code, mcp_error.1.code);
    }

    #[test]
    fn mcp_and_http_anomaly_paths_share_validation_error_codes() {
        let now_epoch_day = current_taipei_business_moment()
            .expect("current time should resolve for test")
            .epoch_day();
        let state = build_state(now_epoch_day);
        let committee = committee_admin();
        let alert_id = "alt-0000000000000001".to_owned();

        let http_error = handle_update_admin_anomaly_alert(
            &state,
            &committee,
            alert_id.clone(),
            AdminAnomalyAlertPatchRequest {
                operation: "INVALID".to_owned(),
                owner_actor_id: None,
                note: None,
                closure_note: None,
                closure_evidence_refs: None,
                ticket_reference: None,
            },
        )
        .expect_err("http anomaly patch should reject unsupported operation");

        let service_account = oauth_service_account_actor(
            "svc-anomaly-parity",
            Role::CommitteeAdmin,
            PlantScope::all(),
        );
        let grant = McpServiceAccountGrant::new(
            service_account.actor_id().clone(),
            service_account,
            [MCP_TOOL_ANOMALY_UPDATE_ALERT_STATUS],
        )
        .expect("anomaly update grant should be valid");
        let mcp_error = invoke_mcp_write_for_test(
            &state,
            &grant,
            MCP_TOOL_ANOMALY_UPDATE_ALERT_STATUS,
            serde_json::json!({
                "alertId": alert_id,
                "request": {
                    "operation": "INVALID"
                }
            }),
        )
        .expect_err("mcp anomaly patch should reject unsupported operation");

        assert_eq!(http_error.0, mcp_error.0);
        assert_eq!(http_error.1.code, mcp_error.1.code);
    }

    #[test]
    fn mcp_and_http_anomaly_read_paths_share_committee_authorization() {
        let now_epoch_day = current_taipei_business_moment()
            .expect("current time should resolve for test")
            .epoch_day();
        let state = build_state(now_epoch_day);
        let non_committee = payroll_operator();
        let query = AnomalyAlertQueryRequest {
            vendor_id: Some("ven-discoverytst-a1".to_owned()),
            owner_actor_id: None,
            status: Some("OPEN".to_owned()),
            escalated_only: None,
            sla_status: None,
            as_of_epoch_day: Some(now_epoch_day),
            as_of_minute_of_day: Some(1439),
        };
        let http_error = handle_list_anomaly_alerts(&state, &non_committee, query)
            .expect_err("HTTP anomaly list should require committee role");

        let service_account = oauth_service_account_actor(
            "svc-anomaly-read-parity",
            Role::PayrollOperator,
            PlantScope::all(),
        );
        let grant = McpServiceAccountGrant::new(
            service_account.actor_id().clone(),
            service_account,
            [MCP_TOOL_ANOMALY_LIST_ALERTS],
        )
        .expect("anomaly list grant should be valid");
        let mcp_error = invoke_mcp_read_tool(
            &state,
            &grant,
            MCP_TOOL_ANOMALY_LIST_ALERTS,
            serde_json::json!({
                "vendorId": "ven-discoverytst-a1",
                "status": "OPEN",
                "asOfEpochDay": now_epoch_day,
                "asOfMinuteOfDay": 1439
            }),
            "mcp-anomaly-read-parity",
        )
        .expect_err("MCP anomaly list should require committee role parity");

        assert_eq!(http_error.0, mcp_error.0);
        assert_eq!(http_error.1.code, mcp_error.1.code);
    }

    #[test]
    fn mcp_bridge_write_authorization_emits_auditable_bridge_metadata() {
        let now_epoch_day = current_taipei_business_moment()
            .expect("current time should resolve for test")
            .epoch_day();
        let state = build_state(now_epoch_day);
        let service_account = oauth_service_account_actor(
            "svc-bridge-audit-parity",
            Role::CommitteeAdmin,
            PlantScope::all(),
        );
        let grant = McpServiceAccountGrant::new(
            service_account.actor_id().clone(),
            service_account,
            [MCP_TOOL_ANOMALY_UPSERT_RULE],
        )
        .expect("bridge audit grant should be valid");
        let bridge = McpShortLivedKeyBridge::new(
            "bridge-key-audit-test",
            10_000,
            10_300,
            10_100,
            "emergency governance exception",
        )
        .expect("bridge should be valid");
        let auth_gateway = McpAuthorizationGateway::new(AccessController::with_default_policy());

        invoke_mcp_write_tool(
            &state,
            &auth_gateway,
            &grant,
            MCP_TOOL_ANOMALY_UPSERT_RULE,
            serde_json::json!({
                "ruleId": "rule-bridgeaudit01",
                "request": {
                    "kind": "EXPIRY_RISK",
                    "displayName": "Bridge Audit Rule",
                    "description": "exercise bridge-key audit metadata persistence",
                    "governanceIssueId": "issue-bridge-audit",
                    "enabled": true,
                    "thresholdValue": 7.5,
                    "thresholdComparator": "LTE",
                    "evaluationWindowDays": 7,
                    "slaMinutes": 30,
                    "severity": "CRITICAL"
                }
            }),
            Some(&bridge),
            10_150,
            "mcp-bridge-audit-request",
        )
        .expect("mcp anomaly upsert with bridge authorization should succeed");

        let audit_events = state
            .audit_trail
            .investigation_query(
                &committee_admin(),
                &AuditInvestigationFilter::default()
                    .with_entity(
                        AuditEntityType::AuditTrail,
                        format!("mcp-write-authz:{MCP_TOOL_ANOMALY_UPSERT_RULE}"),
                    )
                    .expect("mcp authorization audit filter should be valid"),
            )
            .expect("mcp authorization audit events should be queryable");
        let event = audit_events
            .iter()
            .find(|event| event.reason().contains("mcp-write-authz"))
            .expect("mcp authorization audit evidence should exist");

        assert_eq!(
            event.audit_identity().authentication_source(),
            AuthenticationSource::OAuthServiceAccount
        );
        assert_eq!(
            event.audit_identity().operation_id(),
            "manageVendorComplianceLifecycle"
        );
        assert!(event
            .reason()
            .contains("model=OAUTH_SERVICE_ACCOUNT_WITH_BRIDGE_KEY"));
        assert!(event.reason().contains("bridgeKeyId=bridge-key-audit-test"));
        assert!(event
            .reason()
            .contains("bridgeReason=emergency governance exception"));
    }
}
