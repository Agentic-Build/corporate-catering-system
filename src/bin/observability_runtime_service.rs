use std::cmp::Ordering as CmpOrdering;
use std::collections::{BTreeMap, HashSet};
use std::net::SocketAddr;
use std::path::PathBuf;
use std::sync::atomic::{AtomicU64, Ordering as AtomicOrdering};
use std::sync::Arc;
use std::time::{SystemTime, UNIX_EPOCH};

use aes_gcm::aead::{Aead, KeyInit};
use aes_gcm::{Aes256Gcm, Nonce};
use axum::extract::{Path, Query, State};
use axum::http::{header::AUTHORIZATION, HeaderMap, StatusCode};
use axum::routing::{get, patch, post};
use axum::{Json, Router};
use base64::prelude::{Engine as _, BASE64_STANDARD};
use corporate_catering_system::audit::{
    AuditAction, AuditCorrelationId, AuditEntityType, AuditInvestigationFilter,
    AuditRetentionPolicy, AuditSnapshotEncryptionKey, AuditTimestamp, AuditTrailError,
    ImmutableAuditEvidence, ImmutableAuditTrail, ResponsibilityAttribution,
};
use corporate_catering_system::health::{evaluate_probe, HealthProbeKind, HealthState};
use corporate_catering_system::identity::{
    ActorId, AuthenticatedActorContext, AuthenticationSource, EmploymentStatus, PlantId,
    PlantScope, Role,
};
use corporate_catering_system::menu_supply_window::{
    EmployeeMenuDiscoveryEntry, MenuHealthTag, MenuImageUrl, MenuItemId, MenuSupplyPolicy,
    MenuSupplyWindowError, Money, OrderId, OrderLifecycleState, OrderLineItemRequest,
    OrderMutation, OrderRetentionPolicy, OrderSnapshot, SpecialRequest, VendorMenuItem,
    VendorMenuItemDraft,
};
use corporate_catering_system::observability::{
    initialize_telemetry_runtime_from_env, TelemetryService,
};
use corporate_catering_system::payroll::{
    OrderPayrollView, PayrollDeductionRecord, PayrollDisputeId, PayrollDisputeRecord,
    PayrollDisputeTraceEvent, PayrollExchangeBatch, PayrollExchangeBatchId, PayrollExportPage,
    PayrollHrApiSyncOutcome, PayrollLedgerError, PayrollLedgerService, PayrollLedgerSourceKind,
    PayrollLedgerSourceRef, PayrollReconciliationMetadata, PayrollRetentionPolicy,
    PayrollSettlementLockReceipt, PayrollSortField as PayrollSortFieldDomain,
    SortOrder as PayrollSortOrderDomain,
};
use corporate_catering_system::pickup_totp::{
    PickupTotpVerificationError, PickupTotpVerifier, VerifiedTotp,
};
use corporate_catering_system::transport::http::{
    HttpAuditInvestigationExecutionGateway, HttpEmployeeDiscoveryExecutionGateway,
    HttpOrderExecutionError, HttpOrderingExecutionGateway, HttpVendorMenuExecutionGateway,
};
use corporate_catering_system::vendor_compliance::{
    ComplianceDate, ComplianceDocumentTemplate, DocumentTemplateId, HistoryRetentionPolicy,
    VendorCategory, VendorComplianceLifecycle, VendorDocumentSubmission, VendorId,
    VendorReviewDecision,
};
use corporate_catering_system::vendor_delivery_mapping::{
    DeliveryMappingId, DeliveryRuleEffect, ServiceWindow, TaipeiBusinessMoment,
    VendorPlantDeliveryMapping, VendorPlantDeliveryPolicy,
};
use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};
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
const PRELAUNCH_TERMINATED_EMPLOYEE_ACTOR_IDS_ENV: &str = "PRELAUNCH_TERMINATED_EMPLOYEE_ACTOR_IDS";
const PRELAUNCH_AUDIT_TRAIL_ENCRYPTION_KEY_ENV: &str = "PRELAUNCH_AUDIT_TRAIL_ENCRYPTION_KEY_HEX";
const PRELAUNCH_PAYROLL_EXPORT_ENCRYPTION_KEY_ENV: &str =
    "PRELAUNCH_PAYROLL_EXPORT_ENCRYPTION_KEY_HEX";
const AUTHORIZATION_BEARER_PREFIX: &str = "Bearer ";
const PAYROLL_FIELD_ENVELOPE_VERSION: &str = "v1";
const PAYROLL_FIELD_NONCE_BYTES: usize = 12;

const ALL_AUDIT_ACTIONS: [AuditAction; 30] = [
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
];

const ALL_AUDIT_ENTITY_TYPES: [AuditEntityType; 13] = [
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
];

#[derive(Debug, Clone)]
struct AppState {
    next_order_sequence: Arc<AtomicU64>,
    plant_id: PlantId,
    terminated_employee_actor_ids: Arc<HashSet<ActorId>>,
    audit_trail: ImmutableAuditTrail,
    payroll_export_field_encryptor: PayrollExportFieldEncryptor,
    payroll_ledger_service: PayrollLedgerService,
    compliance_lifecycle: Arc<VendorComplianceLifecycle>,
    delivery_policy: Arc<VendorPlantDeliveryPolicy>,
    menu_supply_policy: MenuSupplyPolicy,
    pickup_totp_verifier: Arc<PickupTotpVerifier>,
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
struct AdminPayrollDisputePatchRequest {
    operation: String,
    owner_actor_id: Option<String>,
    note: Option<String>,
    refund_amount_minor: Option<u32>,
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
    recommendation_enabled: Option<bool>,
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

    let state = bootstrap_runtime_state(
        audit_trail.clone(),
        vendor_id,
        plant_id,
        delivery_epoch_day,
        menu_variant_count,
        payroll_retention_policy,
        order_retention_policy,
        payroll_export_field_encryptor,
        pickup_totp_verifier,
    )
    .map_err(|error| format!("failed to bootstrap runtime state: {error}"))?;
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
        state.payroll_ledger_service.clone(),
        committee_actor.clone(),
        payroll_purge_interval_seconds,
    );
    spawn_order_retention_purge_job(
        state.menu_supply_policy.clone(),
        committee_actor,
        order_purge_interval_seconds,
    );

    let app = Router::new()
        .route("/health/ready", get(ready_probe))
        .route("/health/live", get(live_probe))
        .route("/health/startup", get(startup_probe))
        .route("/api/v1/employee/menus", get(list_employee_menus))
        .route("/api/v1/employee/orders", post(create_employee_order))
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
        .with_state(state);

    let listener = tokio::net::TcpListener::bind(socket_addr).await?;
    tracing::info!(bind_addr = %socket_addr, "observability runtime service listening");
    axum::serve(listener, app).await?;
    Ok(())
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

fn seeded_menu_health_tags(index: u16) -> Vec<MenuHealthTag> {
    match index % 5 {
        0 => vec![MenuHealthTag::LowCalorie, MenuHealthTag::HighProtein],
        1 => vec![MenuHealthTag::HighProtein],
        2 => vec![MenuHealthTag::Vegetarian],
        3 => vec![MenuHealthTag::Vegan],
        _ => vec![MenuHealthTag::GlutenFree],
    }
}

fn bootstrap_runtime_state(
    audit_trail: ImmutableAuditTrail,
    vendor_id: VendorId,
    plant_id: PlantId,
    delivery_epoch_day: i32,
    menu_variant_count: u16,
    payroll_retention_policy: PayrollRetentionPolicy,
    order_retention_policy: OrderRetentionPolicy,
    payroll_export_field_encryptor: PayrollExportFieldEncryptor,
    pickup_totp_verifier: Arc<PickupTotpVerifier>,
) -> Result<AppState, String> {
    let committee_actor = load_gate_committee_admin_actor().map_err(|(_, error)| error.message)?;

    let vendor_actor = AuthenticatedActorContext::new(
        ActorId::parse("vendor-load-gate").map_err(|error| error.to_string())?,
        Role::VendorOperator,
        PlantScope::restricted(vec![plant_id.clone()]).map_err(|error| error.to_string())?,
        AuthenticationSource::VendorAccountMfa,
    )
    .map_err(|error| error.to_string())?;

    let mut compliance_lifecycle = VendorComplianceLifecycle::with_audit_trail(
        HistoryRetentionPolicy::default(),
        audit_trail.clone(),
    );
    let vendor_category = VendorCategory::parse("RESTAURANT").map_err(|error| error.to_string())?;
    let template_id =
        DocumentTemplateId::parse("tmpl-load-gate-license").map_err(|error| error.to_string())?;

    compliance_lifecycle
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

    compliance_lifecycle
        .register_vendor_application(
            &vendor_actor,
            vendor_id.clone(),
            "Load Gate Vendor",
            vendor_category,
            submitted_on,
        )
        .map_err(|error| error.to_string())?;

    compliance_lifecycle
        .submit_document(
            &vendor_actor,
            &vendor_id,
            &template_id,
            VendorDocumentSubmission::new(
                "s3://evidence/docs/load-gate-license.pdf",
                submitted_on,
                ComplianceDate::from_epoch_day(delivery_epoch_day.saturating_add(300)),
            )
            .map_err(|error| error.to_string())?,
        )
        .map_err(|error| error.to_string())?;

    compliance_lifecycle
        .review_application(
            &committee_actor,
            &vendor_id,
            VendorReviewDecision::Approved,
            "Prelaunch load-gate vendor is approved.",
            approved_on,
        )
        .map_err(|error| error.to_string())?;

    let mut delivery_policy = VendorPlantDeliveryPolicy::with_audit_trail(audit_trail.clone());
    let mapping_window_start = TaipeiBusinessMoment::new(delivery_epoch_day.saturating_sub(30), 0)
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

    let menu_supply_policy = MenuSupplyPolicy::with_audit_trail_and_retention(
        Default::default(),
        audit_trail.clone(),
        order_retention_policy,
    );
    let terminated_employee_actor_ids =
        parse_terminated_employee_actor_ids_from_env().map_err(|error| {
            format!("{PRELAUNCH_TERMINATED_EMPLOYEE_ACTOR_IDS_ENV} is invalid: {error}")
        })?;
    let payroll_ledger_service =
        PayrollLedgerService::new(payroll_retention_policy, audit_trail.clone());
    let vendor_menu_gateway = HttpVendorMenuExecutionGateway::new(&menu_supply_policy);

    for index in 1..=menu_variant_count {
        let menu_item_id =
            MenuItemId::parse(format!("menu-{index}")).map_err(|error| error.to_string())?;
        let delivery_epoch_day = delivery_epoch_day.saturating_add(i32::from((index - 1) % 7));
        let image_url = MenuImageUrl::parse(format!(
            "https://cdn.example.com/menu/load-gate-{index}.jpg"
        ))
        .map_err(|error| error.to_string())?;
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

    Ok(AppState {
        next_order_sequence: Arc::new(AtomicU64::new(1)),
        plant_id,
        terminated_employee_actor_ids: Arc::new(terminated_employee_actor_ids),
        audit_trail,
        payroll_export_field_encryptor,
        payroll_ledger_service,
        compliance_lifecycle: Arc::new(compliance_lifecycle),
        delivery_policy: Arc::new(delivery_policy),
        menu_supply_policy,
        pickup_totp_verifier,
    })
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

    let discovery_gateway = HttpEmployeeDiscoveryExecutionGateway::new(
        state.compliance_lifecycle.as_ref(),
        state.delivery_policy.as_ref(),
        &state.menu_supply_policy,
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
        recommendation_requested: query.recommendation_enabled.unwrap_or(false),
        recommendation_applied: false,
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
    let employee_actor = load_gate_employee_actor_for_plant(state, &state.plant_id)?;

    let ordering_gateway = HttpOrderingExecutionGateway::new(
        state.compliance_lifecycle.as_ref(),
        state.delivery_policy.as_ref(),
        &state.menu_supply_policy,
    );

    ordering_gateway
        .execute_create_employee_order(
            &employee_actor,
            order_id.clone(),
            &request_vendor_id,
            &state.plant_id,
            delivery_epoch_day,
            line_items,
            requested_at,
        )
        .map_err(map_http_order_execution_error)?;

    let snapshot = load_order_snapshot_or_policy_error(state, &order_id)?;
    sync_payroll_ledger_from_order_snapshot(
        state,
        &employee_actor,
        "createEmployeeOrder",
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
    let mutation = parse_order_mutation(request)?;
    let current_snapshot = load_order_snapshot_or_not_found(state, &order_id)?;
    let employee_actor = load_gate_employee_actor_for_plant(state, current_snapshot.plant_id())?;
    let requested_at = current_taipei_business_moment().map_err(|error| {
        domain_error(
            StatusCode::INTERNAL_SERVER_ERROR,
            "TIME_RESOLUTION_FAILED",
            error,
        )
    })?;

    let ordering_gateway = HttpOrderingExecutionGateway::new(
        state.compliance_lifecycle.as_ref(),
        state.delivery_policy.as_ref(),
        &state.menu_supply_policy,
    );

    ordering_gateway
        .execute_update_employee_order(
            &employee_actor,
            &order_id,
            current_snapshot.vendor_id(),
            &state.plant_id,
            mutation,
            requested_at,
        )
        .map_err(map_http_order_execution_error)?;

    let updated_snapshot = load_order_snapshot_or_not_found(state, &order_id)?;
    sync_payroll_ledger_from_order_snapshot(
        state,
        &employee_actor,
        "updateEmployeeOrder",
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
    let view = state
        .payroll_ledger_service
        .employee_order_view(&employee_actor, &order_id)
        .map_err(map_payroll_ledger_error)?;

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
    let dispute = state
        .payroll_ledger_service
        .open_dispute(
            &employee_actor,
            &order_id,
            &default_owner_actor_id,
            request.reason,
            occurred_at,
        )
        .map_err(map_payroll_ledger_error)?;

    Ok(to_payroll_dispute_payload(&dispute))
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
            state
                .payroll_ledger_service
                .assign_dispute_owner(
                    &payroll_actor,
                    &dispute_id,
                    &owner_actor_id,
                    occurred_at,
                    note,
                )
                .map_err(map_payroll_ledger_error)?
        }
        "RESOLVE_REFUND" => {
            let note = parse_required_patch_note(request.note, "note")?;
            state
                .payroll_ledger_service
                .resolve_dispute_refund(
                    &payroll_actor,
                    &dispute_id,
                    occurred_at,
                    note,
                    request.refund_amount_minor,
                )
                .map_err(map_payroll_ledger_error)?
        }
        "RESOLVE_REJECTED" => {
            let note = parse_required_patch_note(request.note, "note")?;
            state
                .payroll_ledger_service
                .resolve_dispute_rejected(&payroll_actor, &dispute_id, occurred_at, note)
                .map_err(map_payroll_ledger_error)?
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
    let report = state
        .payroll_ledger_service
        .purge_expired_data(committee_actor, as_of)
        .map_err(map_payroll_ledger_error)?;

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
    let report = state
        .menu_supply_policy
        .purge_expired_orders(committee_actor, as_of)
        .map_err(map_order_retention_purge_error)?;

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
    let export_page = state
        .payroll_ledger_service
        .export_sftp_batch(
            payroll_actor,
            &pay_period,
            &cycle_key,
            page,
            page_size,
            sort_by,
            sort_order,
            occurred_at,
        )
        .map_err(map_payroll_ledger_error)?;

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
    let export_page = state
        .payroll_ledger_service
        .close_monthly_settlement(
            payroll_actor,
            request.cycle_key.as_deref(),
            page,
            page_size,
            sort_by,
            sort_order,
            occurred_at,
        )
        .map_err(map_payroll_ledger_error)?;

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
    let receipt = state
        .payroll_ledger_service
        .unlock_cycle_for_recompute(committee_actor, &cycle_key, reason, occurred_at)
        .map_err(map_payroll_ledger_error)?;

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
    let receipt = state
        .payroll_ledger_service
        .lock_cycle(committee_actor, &cycle_key, reason, occurred_at)
        .map_err(map_payroll_ledger_error)?;

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
    let batch = state
        .payroll_ledger_service
        .sync_hr_api_adjunct(payroll_actor, &batch_id, outcome, request.note, occurred_at)
        .map_err(map_payroll_ledger_error)?;

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

fn require_corporate_actor_for_role(
    headers: &HeaderMap,
    required_role: Role,
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
    AuthenticatedActorContext::new(
        actor_id,
        role,
        PlantScope::all(),
        AuthenticationSource::CorporateSso,
    )
    .map_err(|error| {
        domain_error(
            StatusCode::UNAUTHORIZED,
            "UNAUTHORIZED",
            format!("bearer actor context is invalid: {error}"),
        )
    })
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

fn generate_contract_order_id(state: &AppState) -> Result<OrderId, (StatusCode, ErrorPayload)> {
    let sequence = state
        .next_order_sequence
        .fetch_add(1, AtomicOrdering::Relaxed);
    OrderId::parse(format!("ord-{sequence:016x}")).map_err(|error| {
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
    let mut resolved_vendor_id: Option<VendorId> = None;
    for line_item in line_items {
        let menu_item = state
            .menu_supply_policy
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

fn load_order_snapshot_or_policy_error(
    state: &AppState,
    order_id: &OrderId,
) -> Result<OrderSnapshot, (StatusCode, ErrorPayload)> {
    state
        .menu_supply_policy
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
                StatusCode::CONFLICT,
                "ORDER_POLICY_VIOLATION",
                format!(
                    "order `{}` is missing after successful mutation",
                    order_id.as_str()
                ),
            )
        })
}

fn load_order_snapshot_or_not_found(
    state: &AppState,
    order_id: &OrderId,
) -> Result<OrderSnapshot, (StatusCode, ErrorPayload)> {
    state
        .menu_supply_policy
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
}

fn build_employee_order_payload(
    state: &AppState,
    snapshot: &OrderSnapshot,
) -> Result<EmployeeOrderPayload, (StatusCode, ErrorPayload)> {
    let mut line_items = Vec::with_capacity(snapshot.line_items().len());
    let mut total_minor: u64 = 0;
    let mut order_currency: Option<String> = None;

    for (menu_item_id, quantity) in snapshot.line_items() {
        let menu_item = state
            .menu_supply_policy
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

fn compute_order_total_for_payroll(
    state: &AppState,
    snapshot: &OrderSnapshot,
) -> Result<(String, u32), (StatusCode, ErrorPayload)> {
    let mut total_minor: u64 = 0;
    let mut currency: Option<String> = None;
    for (menu_item_id, quantity) in snapshot.line_items() {
        let menu_item = state
            .menu_supply_policy
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
    let employee_employment_status =
        if actor.role() == Role::Employee && actor.actor_id() == snapshot.employee_actor_id() {
            actor.employment_status()
        } else {
            EmploymentStatus::Active
        };
    state
        .payroll_ledger_service
        .reconcile_order_charge(
            actor,
            operation_id,
            snapshot.order_id(),
            snapshot.employee_actor_id(),
            employee_employment_status,
            snapshot.delivery_epoch_day(),
            &currency,
            target_amount_minor,
            AuditTimestamp::from_taipei_business_moment(
                occurred_at.epoch_day(),
                occurred_at.minute_of_day(),
            )
            .map_err(|error| {
                domain_error(
                    StatusCode::INTERNAL_SERVER_ERROR,
                    "PAYROLL_LEDGER_INTERNAL_ERROR",
                    error.to_string(),
                )
            })?,
            source_event,
        )
        .map_err(|error| {
            domain_error(
                StatusCode::INTERNAL_SERVER_ERROR,
                "PAYROLL_LEDGER_INTERNAL_ERROR",
                error.to_string(),
            )
        })?;
    Ok(())
}

fn taipei_moment_to_iso_datetime(moment: TaipeiBusinessMoment) -> String {
    let (year, month, day) = civil_from_days(i64::from(moment.epoch_day()));
    let hour = moment.minute_of_day() / 60;
    let minute = moment.minute_of_day() % 60;
    format!("{year:04}-{month:02}-{day:02}T{hour:02}:{minute:02}:00+08:00")
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
    payroll_ledger_service: PayrollLedgerService,
    committee_actor: AuthenticatedActorContext,
    interval_seconds: u64,
) {
    tokio::spawn(async move {
        run_payroll_retention_purge_once(&payroll_ledger_service, &committee_actor);
        let mut interval = time::interval(std::time::Duration::from_secs(interval_seconds));
        interval.set_missed_tick_behavior(MissedTickBehavior::Skip);
        loop {
            interval.tick().await;
            run_payroll_retention_purge_once(&payroll_ledger_service, &committee_actor);
        }
    });
}

fn run_payroll_retention_purge_once(
    payroll_ledger_service: &PayrollLedgerService,
    committee_actor: &AuthenticatedActorContext,
) {
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
    match payroll_ledger_service.purge_expired_data(committee_actor, as_of) {
        Ok(report) => tracing::info!(
            purged_ledger_entries = report.purged_ledger_entries,
            purged_disputes = report.purged_disputes,
            purged_exchange_batches = report.purged_exchange_batches,
            as_of_epoch_day = as_of.epoch_day(),
            as_of_minute_of_day = as_of.minute_of_day(),
            "payroll retention purge job completed"
        ),
        Err(error) => tracing::error!(error = %error, "payroll retention purge job failed"),
    }
}

fn spawn_order_retention_purge_job(
    menu_supply_policy: MenuSupplyPolicy,
    committee_actor: AuthenticatedActorContext,
    interval_seconds: u64,
) {
    tokio::spawn(async move {
        run_order_retention_purge_once(&menu_supply_policy, &committee_actor);
        let mut interval = time::interval(std::time::Duration::from_secs(interval_seconds));
        interval.set_missed_tick_behavior(MissedTickBehavior::Skip);
        loop {
            interval.tick().await;
            run_order_retention_purge_once(&menu_supply_policy, &committee_actor);
        }
    });
}

fn run_order_retention_purge_once(
    menu_supply_policy: &MenuSupplyPolicy,
    committee_actor: &AuthenticatedActorContext,
) {
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
    match menu_supply_policy.purge_expired_orders(committee_actor, as_of) {
        Ok(report) => tracing::info!(
            purged_orders = report.purged_orders,
            as_of_epoch_day = as_of.epoch_day(),
            as_of_minute_of_day = as_of.minute_of_day(),
            "order retention purge job completed"
        ),
        Err(error) => tracing::error!(error = %error, "order retention purge job failed"),
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
    let employee_actor = load_gate_employee_actor_for_plant(state, snapshot.plant_id())?;

    state
        .menu_supply_policy
        .update_order(
            &employee_actor,
            &order_id,
            OrderMutation::MarkFulfilled,
            requested_at,
        )
        .map_err(|error| map_pickup_claim_update_error(&order_id, request_id, error))?;

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

    fn payroll_export_field_encryptor() -> PayrollExportFieldEncryptor {
        PayrollExportFieldEncryptor::parse_hex(
            "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff",
        )
        .expect("test payroll export encryption key should parse")
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
                        "s3://evidence/docs/discovery-license.pdf",
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
                            MenuImageUrl::parse("https://cdn.example.com/menu/visible-bento.jpg")
                                .expect("image should be valid"),
                        ),
                        Money::new("TWD", 12000).expect("money should be valid"),
                        5,
                        now_epoch_day.saturating_add(1),
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
                            MenuImageUrl::parse("https://cdn.example.com/menu/visible-salad.jpg")
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
                            MenuImageUrl::parse("https://cdn.example.com/menu/hidden-bento.jpg")
                                .expect("image should be valid"),
                        ),
                        Money::new("TWD", 11000).expect("money should be valid"),
                        9,
                        now_epoch_day.saturating_add(1),
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
                now_epoch_day.saturating_add(1),
                vec![
                    OrderLineItemRequest::new(menu_item_id("menu-discoverytsta1"), 2, vec![])
                        .expect("line item should be valid"),
                ],
                taipei_moment(now_epoch_day, 600),
            )
            .expect("order should consume inventory");

        AppState {
            next_order_sequence: Arc::new(AtomicU64::new(1)),
            plant_id: plant,
            terminated_employee_actor_ids: Arc::new(HashSet::new()),
            audit_trail,
            payroll_export_field_encryptor: payroll_export_field_encryptor(),
            payroll_ledger_service,
            compliance_lifecycle: Arc::new(compliance_lifecycle),
            delivery_policy: Arc::new(delivery_policy),
            menu_supply_policy,
            pickup_totp_verifier: Arc::new(
                PickupTotpVerifier::from_secret("unit-test-pickup-totp-secret".as_bytes())
                    .expect("test pickup verifier should be valid"),
            ),
        }
    }

    fn build_state_with_terminated_load_gate_employee(now_epoch_day: i32) -> AppState {
        let mut state = build_state(now_epoch_day);
        state.terminated_employee_actor_ids =
            Arc::new(HashSet::from([actor_id(LOAD_GATE_EMPLOYEE_ACTOR_ID)]));
        state
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
            delivery_date: epoch_day_to_iso_date(now_epoch_day.saturating_add(1)),
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
            delivery_date: epoch_day_to_iso_date(now_epoch_day.saturating_add(1)),
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
            next_order_sequence: Arc::new(AtomicU64::new(1)),
            plant_id: plant_id("fab-a"),
            terminated_employee_actor_ids: Arc::new(HashSet::new()),
            audit_trail: audit_trail.clone(),
            payroll_export_field_encryptor: payroll_export_field_encryptor(),
            payroll_ledger_service: PayrollLedgerService::new(
                PayrollRetentionPolicy::default(),
                audit_trail.clone(),
            ),
            compliance_lifecycle: Arc::new(VendorComplianceLifecycle::with_audit_trail(
                HistoryRetentionPolicy::default(),
                audit_trail.clone(),
            )),
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
            delivery_date: epoch_day_to_iso_date(now_epoch_day.saturating_add(1)),
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
            epoch_day_to_iso_date(now_epoch_day + 1)
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
            recommendation_enabled: Some(false),
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
    fn recommendation_flag_does_not_change_core_discovery_behavior() {
        let now_epoch_day = 300;
        let state = build_state(now_epoch_day);
        let query = EmployeeMenuDiscoveryQuery {
            plant_id: Some("fab-a".to_owned()),
            view: Some(MenuDiscoveryViewQuery::Week),
            menu_date: Some(epoch_day_to_iso_date(now_epoch_day)),
            recommendation_enabled: Some(true),
            ..EmployeeMenuDiscoveryQuery::default()
        };

        let response_a =
            handle_list_employee_menus_at(&state, query, taipei_moment(now_epoch_day, 600))
                .expect("discovery request should succeed");
        let query = EmployeeMenuDiscoveryQuery {
            plant_id: Some("fab-a".to_owned()),
            view: Some(MenuDiscoveryViewQuery::Week),
            menu_date: Some(epoch_day_to_iso_date(now_epoch_day)),
            recommendation_enabled: Some(true),
            ..EmployeeMenuDiscoveryQuery::default()
        };
        let response_b =
            handle_list_employee_menus_at(&state, query, taipei_moment(now_epoch_day, 600))
                .expect("discovery request should succeed");

        assert!(response_a.recommendation_requested);
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
            delivery_date: epoch_day_to_iso_date(now_epoch_day.saturating_add(1)),
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
            delivery_date: epoch_day_to_iso_date(now_epoch_day.saturating_add(1)),
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
            delivery_date: epoch_day_to_iso_date(now_epoch_day.saturating_add(1)),
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
            delivery_date: epoch_day_to_iso_date(now_epoch_day.saturating_add(1)),
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
            delivery_date: epoch_day_to_iso_date(now_epoch_day.saturating_add(1)),
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
    fn payroll_export_handler_can_emit_terminated_employee_exception_status() {
        let now_epoch_day = current_taipei_business_moment()
            .expect("current time should resolve for test")
            .epoch_day();
        let state = build_state_with_terminated_load_gate_employee(now_epoch_day);
        let payroll = payroll_operator();
        let create_request = EmployeeOrderCreateRequestPayload {
            plant_id: "fab-a".to_owned(),
            delivery_date: epoch_day_to_iso_date(now_epoch_day.saturating_add(1)),
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
            delivery_date: epoch_day_to_iso_date(now_epoch_day.saturating_add(1)),
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
}
