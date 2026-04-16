use std::collections::{BTreeMap, BTreeSet};
use std::fmt;
use std::sync::{Arc, Mutex};

use sha2::{Digest, Sha256};

use crate::audit::{
    AuditAction, AuditCorrelationId, AuditEntityRef, AuditEntityType, AuditEvidenceWrite,
    AuditIdentityLink, AuditTimestamp, AuditTrailError, ImmutableAuditTrail,
};
use crate::identity::{ActorId, AuthenticatedActorContext, EmploymentStatus, Role};
use crate::menu_supply_window::{Money, OrderId};

const MAX_DISPUTE_REASON_LENGTH: usize = 280;
const MAX_CYCLE_KEY_LENGTH: usize = 64;
const OPEN_PAYROLL_DISPUTE_OPERATION_ID: &str = "openPayrollDispute";
const ASSIGN_PAYROLL_DISPUTE_OWNER_OPERATION_ID: &str = "assignPayrollDisputeOwner";
const RESOLVE_PAYROLL_DISPUTE_OPERATION_ID: &str = "resolvePayrollDispute";
const EXPORT_PAYROLL_SFTP_BATCH_OPERATION_ID: &str = "exportPayrollSftpBatch";
const LOCK_PAYROLL_SETTLEMENT_CYCLE_OPERATION_ID: &str = "lockPayrollSettlementCycle";
const UNLOCK_PAYROLL_SETTLEMENT_CYCLE_OPERATION_ID: &str = "unlockPayrollSettlementCycle";
const SYNC_PAYROLL_HR_API_OPERATION_ID: &str = "syncPayrollHrApiAdjunct";
const PURGE_PAYROLL_DATA_OPERATION_ID: &str = "purgePayrollData";
const PAYROLL_EXCHANGE_CORRELATION_PREFIX: &str = "payroll-exchange";
const PAYROLL_SETTLEMENT_CORRELATION_PREFIX: &str = "payroll-settlement";

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct PayrollRetentionPolicy {
    ledger_retention_days: u16,
    dispute_retention_days: u16,
    exchange_retention_days: u16,
}

impl PayrollRetentionPolicy {
    pub fn new(
        ledger_retention_days: u16,
        dispute_retention_days: u16,
        exchange_retention_days: u16,
    ) -> Result<Self, PayrollLedgerError> {
        if ledger_retention_days == 0 || dispute_retention_days == 0 || exchange_retention_days == 0
        {
            return Err(PayrollLedgerError::InvalidRetentionPolicy);
        }
        Ok(Self {
            ledger_retention_days,
            dispute_retention_days,
            exchange_retention_days,
        })
    }

    pub const fn ledger_retention_days(self) -> u16 {
        self.ledger_retention_days
    }

    pub const fn dispute_retention_days(self) -> u16 {
        self.dispute_retention_days
    }

    pub const fn exchange_retention_days(self) -> u16 {
        self.exchange_retention_days
    }
}

impl Default for PayrollRetentionPolicy {
    fn default() -> Self {
        Self {
            ledger_retention_days: 365 * 2,
            dispute_retention_days: 365,
            exchange_retention_days: 365,
        }
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum PayrollLedgerEntryKind {
    Deduction,
    AdjustmentDebit,
    AdjustmentCredit,
    Refund,
}

impl PayrollLedgerEntryKind {
    pub const fn as_str(self) -> &'static str {
        match self {
            Self::Deduction => "DEDUCTION",
            Self::AdjustmentDebit => "ADJUSTMENT_DEBIT",
            Self::AdjustmentCredit => "ADJUSTMENT_CREDIT",
            Self::Refund => "REFUND",
        }
    }

    const fn signum(self) -> i64 {
        match self {
            Self::Deduction | Self::AdjustmentDebit => 1,
            Self::AdjustmentCredit | Self::Refund => -1,
        }
    }
}

impl fmt::Display for PayrollLedgerEntryKind {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(self.as_str())
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum PayrollLedgerSourceKind {
    OrderMutation,
    DisputeWorkflow,
    SftpBatchExport,
    HrApiSyncAdjunct,
}

impl PayrollLedgerSourceKind {
    pub const fn as_str(self) -> &'static str {
        match self {
            Self::OrderMutation => "ORDER_MUTATION",
            Self::DisputeWorkflow => "DISPUTE_WORKFLOW",
            Self::SftpBatchExport => "SFTP_BATCH_EXPORT",
            Self::HrApiSyncAdjunct => "HR_API_SYNC_ADJUNCT",
        }
    }
}

impl fmt::Display for PayrollLedgerSourceKind {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(self.as_str())
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PayrollLedgerSourceRef {
    kind: PayrollLedgerSourceKind,
    event_reference: String,
}

impl PayrollLedgerSourceRef {
    pub fn new(
        kind: PayrollLedgerSourceKind,
        event_reference: impl Into<String>,
    ) -> Result<Self, PayrollLedgerError> {
        let event_reference = event_reference.into();
        if event_reference.trim().is_empty() {
            return Err(PayrollLedgerError::InvalidSourceEventReference);
        }
        Ok(Self {
            kind,
            event_reference,
        })
    }

    pub const fn kind(&self) -> PayrollLedgerSourceKind {
        self.kind
    }

    pub fn event_reference(&self) -> &str {
        &self.event_reference
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PayrollLedgerEntry {
    entry_id: u64,
    order_id: OrderId,
    employee_actor_id: ActorId,
    delivery_epoch_day: i32,
    amount: Money,
    kind: PayrollLedgerEntryKind,
    occurred_at: AuditTimestamp,
    source_event: PayrollLedgerSourceRef,
}

impl PayrollLedgerEntry {
    pub fn entry_id(&self) -> u64 {
        self.entry_id
    }

    pub fn order_id(&self) -> &OrderId {
        &self.order_id
    }

    pub fn employee_actor_id(&self) -> &ActorId {
        &self.employee_actor_id
    }

    pub const fn delivery_epoch_day(&self) -> i32 {
        self.delivery_epoch_day
    }

    pub fn amount(&self) -> &Money {
        &self.amount
    }

    pub const fn kind(&self) -> PayrollLedgerEntryKind {
        self.kind
    }

    pub const fn occurred_at(&self) -> AuditTimestamp {
        self.occurred_at
    }

    pub fn source_event(&self) -> &PayrollLedgerSourceRef {
        &self.source_event
    }

    pub fn signed_amount_minor(&self) -> i64 {
        i64::from(self.amount.amount_minor()) * self.kind.signum()
    }
}

#[derive(Debug, Clone, PartialEq, Eq, PartialOrd, Ord, Hash)]
pub struct PayrollDisputeId(String);

impl PayrollDisputeId {
    pub fn parse(value: impl Into<String>) -> Result<Self, PayrollLedgerError> {
        let value = value.into();
        if value.trim().is_empty() {
            return Err(PayrollLedgerError::InvalidDisputeId);
        }
        Ok(Self(value))
    }

    pub fn as_str(&self) -> &str {
        &self.0
    }
}

impl fmt::Display for PayrollDisputeId {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(self.as_str())
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum PayrollDisputeStatus {
    Open,
    InReview,
    ResolvedRefundApproved,
    ResolvedRejected,
}

impl PayrollDisputeStatus {
    pub const fn as_str(self) -> &'static str {
        match self {
            Self::Open => "OPEN",
            Self::InReview => "IN_REVIEW",
            Self::ResolvedRefundApproved => "RESOLVED_REFUND_APPROVED",
            Self::ResolvedRejected => "RESOLVED_REJECTED",
        }
    }

    const fn is_resolved(self) -> bool {
        matches!(self, Self::ResolvedRefundApproved | Self::ResolvedRejected)
    }
}

impl fmt::Display for PayrollDisputeStatus {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(self.as_str())
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum PayrollDisputeTraceEventType {
    Opened,
    OwnerAssigned,
    ResolvedRefundApproved,
    ResolvedRejected,
}

impl PayrollDisputeTraceEventType {
    pub const fn as_str(self) -> &'static str {
        match self {
            Self::Opened => "OPENED",
            Self::OwnerAssigned => "OWNER_ASSIGNED",
            Self::ResolvedRefundApproved => "RESOLVED_REFUND_APPROVED",
            Self::ResolvedRejected => "RESOLVED_REJECTED",
        }
    }
}

impl fmt::Display for PayrollDisputeTraceEventType {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(self.as_str())
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PayrollDisputeTraceEvent {
    occurred_at: AuditTimestamp,
    actor_id: ActorId,
    event_type: PayrollDisputeTraceEventType,
    status: PayrollDisputeStatus,
    owner_actor_id: ActorId,
    note: Option<String>,
    source_event: PayrollLedgerSourceRef,
    refund_ledger_entry_id: Option<u64>,
}

impl PayrollDisputeTraceEvent {
    pub const fn occurred_at(&self) -> AuditTimestamp {
        self.occurred_at
    }

    pub fn actor_id(&self) -> &ActorId {
        &self.actor_id
    }

    pub const fn event_type(&self) -> PayrollDisputeTraceEventType {
        self.event_type
    }

    pub const fn status(&self) -> PayrollDisputeStatus {
        self.status
    }

    pub fn owner_actor_id(&self) -> &ActorId {
        &self.owner_actor_id
    }

    pub fn note(&self) -> Option<&str> {
        self.note.as_deref()
    }

    pub fn source_event(&self) -> &PayrollLedgerSourceRef {
        &self.source_event
    }

    pub const fn refund_ledger_entry_id(&self) -> Option<u64> {
        self.refund_ledger_entry_id
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PayrollDisputeRecord {
    dispute_id: PayrollDisputeId,
    order_id: OrderId,
    employee_actor_id: ActorId,
    owner_actor_id: ActorId,
    status: PayrollDisputeStatus,
    opened_at: AuditTimestamp,
    updated_at: AuditTimestamp,
    trace: Vec<PayrollDisputeTraceEvent>,
}

impl PayrollDisputeRecord {
    pub fn dispute_id(&self) -> &PayrollDisputeId {
        &self.dispute_id
    }

    pub fn order_id(&self) -> &OrderId {
        &self.order_id
    }

    pub fn employee_actor_id(&self) -> &ActorId {
        &self.employee_actor_id
    }

    pub fn owner_actor_id(&self) -> &ActorId {
        &self.owner_actor_id
    }

    pub const fn status(&self) -> PayrollDisputeStatus {
        self.status
    }

    pub const fn opened_at(&self) -> AuditTimestamp {
        self.opened_at
    }

    pub const fn updated_at(&self) -> AuditTimestamp {
        self.updated_at
    }

    pub fn trace(&self) -> &[PayrollDisputeTraceEvent] {
        &self.trace
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct OrderPayrollView {
    order_id: OrderId,
    employee_actor_id: ActorId,
    delivery_epoch_day: i32,
    currency: String,
    net_amount_minor: i64,
    ledger_entries: Vec<PayrollLedgerEntry>,
    disputes: Vec<PayrollDisputeRecord>,
}

impl OrderPayrollView {
    pub fn order_id(&self) -> &OrderId {
        &self.order_id
    }

    pub fn employee_actor_id(&self) -> &ActorId {
        &self.employee_actor_id
    }

    pub const fn delivery_epoch_day(&self) -> i32 {
        self.delivery_epoch_day
    }

    pub fn currency(&self) -> &str {
        &self.currency
    }

    pub const fn net_amount_minor(&self) -> i64 {
        self.net_amount_minor
    }

    pub fn ledger_entries(&self) -> &[PayrollLedgerEntry] {
        &self.ledger_entries
    }

    pub fn disputes(&self) -> &[PayrollDisputeRecord] {
        &self.disputes
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum PayrollSortField {
    EmployeeActorId,
    AmountMinor,
    DeliveryDate,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum SortOrder {
    Asc,
    Desc,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum PayrollDeductionStatus {
    Ready,
    Locked,
    Refunded,
    Disputed,
    DeductionFailed,
    EmployeeTerminated,
}

impl PayrollDeductionStatus {
    pub const fn as_str(self) -> &'static str {
        match self {
            Self::Ready => "READY",
            Self::Locked => "LOCKED",
            Self::Refunded => "REFUNDED",
            Self::Disputed => "DISPUTED",
            Self::DeductionFailed => "DEDUCTION_FAILED",
            Self::EmployeeTerminated => "EMPLOYEE_TERMINATED",
        }
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord)]
pub enum PayrollExceptionClass {
    Disputed,
    DeductionFailed,
    EmployeeTerminated,
    Refunded,
}

impl PayrollExceptionClass {
    pub const fn as_str(self) -> &'static str {
        match self {
            Self::Disputed => "DISPUTED",
            Self::DeductionFailed => "DEDUCTION_FAILED",
            Self::EmployeeTerminated => "EMPLOYEE_TERMINATED",
            Self::Refunded => "REFUNDED",
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PayrollReconciliationMetadata {
    total_records: usize,
    total_amount_minor: u64,
    total_source_entries: usize,
    ready_records: usize,
    locked_records: usize,
    refunded_records: usize,
    disputed_records: usize,
    deduction_failed_records: usize,
    employee_terminated_records: usize,
    required_exception_classes: Vec<PayrollExceptionClass>,
    present_exception_classes: Vec<PayrollExceptionClass>,
}

impl PayrollReconciliationMetadata {
    pub const fn total_records(&self) -> usize {
        self.total_records
    }

    pub const fn total_amount_minor(&self) -> u64 {
        self.total_amount_minor
    }

    pub const fn total_source_entries(&self) -> usize {
        self.total_source_entries
    }

    pub const fn ready_records(&self) -> usize {
        self.ready_records
    }

    pub const fn locked_records(&self) -> usize {
        self.locked_records
    }

    pub const fn refunded_records(&self) -> usize {
        self.refunded_records
    }

    pub const fn disputed_records(&self) -> usize {
        self.disputed_records
    }

    pub const fn deduction_failed_records(&self) -> usize {
        self.deduction_failed_records
    }

    pub const fn employee_terminated_records(&self) -> usize {
        self.employee_terminated_records
    }

    pub fn required_exception_classes(&self) -> &[PayrollExceptionClass] {
        &self.required_exception_classes
    }

    pub fn present_exception_classes(&self) -> &[PayrollExceptionClass] {
        &self.present_exception_classes
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum PayrollSettlementLockState {
    Locked,
    Unlocked,
}

impl PayrollSettlementLockState {
    pub const fn as_str(self) -> &'static str {
        match self {
            Self::Locked => "LOCKED",
            Self::Unlocked => "UNLOCKED",
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PayrollDeductionRecord {
    employee_actor_id: ActorId,
    order_id: OrderId,
    delivery_epoch_day: i32,
    amount: Money,
    pay_period: String,
    status: PayrollDeductionStatus,
    dispute_status: Option<PayrollDisputeStatus>,
    source_entry_ids: Vec<u64>,
}

impl PayrollDeductionRecord {
    pub fn employee_actor_id(&self) -> &ActorId {
        &self.employee_actor_id
    }

    pub fn order_id(&self) -> &OrderId {
        &self.order_id
    }

    pub const fn delivery_epoch_day(&self) -> i32 {
        self.delivery_epoch_day
    }

    pub fn amount(&self) -> &Money {
        &self.amount
    }

    pub fn pay_period(&self) -> &str {
        &self.pay_period
    }

    pub const fn status(&self) -> PayrollDeductionStatus {
        self.status
    }

    pub const fn dispute_status(&self) -> Option<PayrollDisputeStatus> {
        self.dispute_status
    }

    pub fn source_entry_ids(&self) -> &[u64] {
        &self.source_entry_ids
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum HrApiSyncStatus {
    NotSynced,
    Succeeded,
    Failed,
}

impl HrApiSyncStatus {
    pub const fn as_str(self) -> &'static str {
        match self {
            Self::NotSynced => "NOT_SYNCED",
            Self::Succeeded => "SUCCEEDED",
            Self::Failed => "FAILED",
        }
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum PayrollHrApiSyncOutcome {
    Succeeded,
    Failed,
}

#[derive(Debug, Clone, PartialEq, Eq, PartialOrd, Ord, Hash)]
pub struct PayrollExchangeBatchId(String);

impl PayrollExchangeBatchId {
    pub fn parse(value: impl Into<String>) -> Result<Self, PayrollLedgerError> {
        let value = value.into();
        if value.trim().is_empty() {
            return Err(PayrollLedgerError::InvalidExchangeBatchId);
        }
        Ok(Self(value))
    }

    pub fn as_str(&self) -> &str {
        &self.0
    }
}

impl fmt::Display for PayrollExchangeBatchId {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(self.as_str())
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PayrollSettlementLockReceipt {
    cycle_key: String,
    pay_period: String,
    lock_state: PayrollSettlementLockState,
    batch_id: PayrollExchangeBatchId,
    snapshot_checksum: String,
    reason: String,
    changed_at: AuditTimestamp,
    actor_id: ActorId,
}

impl PayrollSettlementLockReceipt {
    pub fn cycle_key(&self) -> &str {
        &self.cycle_key
    }

    pub fn pay_period(&self) -> &str {
        &self.pay_period
    }

    pub const fn lock_state(&self) -> PayrollSettlementLockState {
        self.lock_state
    }

    pub fn batch_id(&self) -> &PayrollExchangeBatchId {
        &self.batch_id
    }

    pub fn snapshot_checksum(&self) -> &str {
        &self.snapshot_checksum
    }

    pub fn reason(&self) -> &str {
        &self.reason
    }

    pub const fn changed_at(&self) -> AuditTimestamp {
        self.changed_at
    }

    pub fn actor_id(&self) -> &ActorId {
        &self.actor_id
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct HrApiSyncReceipt {
    synced_at: AuditTimestamp,
    actor_id: ActorId,
    status: HrApiSyncStatus,
}

impl HrApiSyncReceipt {
    pub const fn synced_at(&self) -> AuditTimestamp {
        self.synced_at
    }

    pub fn actor_id(&self) -> &ActorId {
        &self.actor_id
    }

    pub const fn status(&self) -> HrApiSyncStatus {
        self.status
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PayrollExchangeBatch {
    batch_id: PayrollExchangeBatchId,
    pay_period: String,
    cycle_key: String,
    generated_at: AuditTimestamp,
    cycle_start_epoch_day: i32,
    cycle_end_epoch_day: i32,
    order_ids: Vec<OrderId>,
    snapshot_checksum: String,
    reconciliation: PayrollReconciliationMetadata,
    snapshot_records: Vec<PayrollDeductionRecord>,
    hr_api_sync_receipt: Option<HrApiSyncReceipt>,
}

impl PayrollExchangeBatch {
    pub fn batch_id(&self) -> &PayrollExchangeBatchId {
        &self.batch_id
    }

    pub fn pay_period(&self) -> &str {
        &self.pay_period
    }

    pub fn cycle_key(&self) -> &str {
        &self.cycle_key
    }

    pub const fn generated_at(&self) -> AuditTimestamp {
        self.generated_at
    }

    pub const fn cycle_start_epoch_day(&self) -> i32 {
        self.cycle_start_epoch_day
    }

    pub const fn cycle_end_epoch_day(&self) -> i32 {
        self.cycle_end_epoch_day
    }

    pub fn order_ids(&self) -> &[OrderId] {
        &self.order_ids
    }

    pub fn snapshot_checksum(&self) -> &str {
        &self.snapshot_checksum
    }

    pub fn reconciliation(&self) -> &PayrollReconciliationMetadata {
        &self.reconciliation
    }

    pub fn hr_api_sync_receipt(&self) -> Option<&HrApiSyncReceipt> {
        self.hr_api_sync_receipt.as_ref()
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PayrollExportPage {
    items: Vec<PayrollDeductionRecord>,
    total_items: usize,
    page: usize,
    page_size: usize,
    batch: PayrollExchangeBatch,
}

impl PayrollExportPage {
    pub fn items(&self) -> &[PayrollDeductionRecord] {
        &self.items
    }

    pub const fn total_items(&self) -> usize {
        self.total_items
    }

    pub const fn page(&self) -> usize {
        self.page
    }

    pub const fn page_size(&self) -> usize {
        self.page_size
    }

    pub fn batch(&self) -> &PayrollExchangeBatch {
        &self.batch
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
pub struct PayrollPurgeReport {
    pub purged_ledger_entries: usize,
    pub purged_disputes: usize,
    pub purged_exchange_batches: usize,
}

#[derive(Debug, Clone)]
pub struct PayrollLedgerService {
    state: Arc<Mutex<PayrollLedgerState>>,
    audit_trail: ImmutableAuditTrail,
}

impl PayrollLedgerService {
    pub fn new(retention_policy: PayrollRetentionPolicy, audit_trail: ImmutableAuditTrail) -> Self {
        Self {
            state: Arc::new(Mutex::new(PayrollLedgerState {
                retention_policy,
                ..PayrollLedgerState::default()
            })),
            audit_trail,
        }
    }

    pub fn retention_policy(&self) -> Result<PayrollRetentionPolicy, PayrollLedgerError> {
        let state = lock_state(&self.state)?;
        Ok(state.retention_policy)
    }

    #[allow(clippy::too_many_arguments)]
    pub fn reconcile_order_charge(
        &self,
        actor: &AuthenticatedActorContext,
        operation_id: &str,
        order_id: &OrderId,
        employee_actor_id: &ActorId,
        employee_employment_status: EmploymentStatus,
        delivery_epoch_day: i32,
        currency: &str,
        target_amount_minor: u32,
        occurred_at: AuditTimestamp,
        source_event: PayrollLedgerSourceRef,
    ) -> Result<Option<PayrollLedgerEntry>, PayrollLedgerError> {
        if operation_id.trim().is_empty() {
            return Err(PayrollLedgerError::InvalidOperationId);
        }

        let mut state = lock_state(&self.state)?;
        let previous_state = state.clone();

        upsert_order_registry(
            &mut state,
            order_id,
            employee_actor_id,
            employee_employment_status,
            delivery_epoch_day,
            currency,
        )?;

        let current_net = current_order_net_amount_minor_locked(&state, order_id);
        let target = i64::from(target_amount_minor);
        let delta = target - current_net;
        if delta == 0 {
            return Ok(None);
        }

        let kind = if current_net == 0 && delta > 0 {
            PayrollLedgerEntryKind::Deduction
        } else if delta > 0 {
            PayrollLedgerEntryKind::AdjustmentDebit
        } else {
            PayrollLedgerEntryKind::AdjustmentCredit
        };
        let delta_minor =
            u32::try_from(delta.abs()).map_err(|_| PayrollLedgerError::AmountOutOfRange {
                amount_minor: delta.abs(),
            })?;

        let entry = append_ledger_entry_locked(
            &mut state,
            order_id,
            employee_actor_id,
            delivery_epoch_day,
            currency,
            delta_minor,
            kind,
            occurred_at,
            source_event,
        )?;

        if let Err(error) = self.append_audit_event(
            actor,
            operation_id,
            AuditAction::AppendPayrollLedgerEntry,
            AuditEntityType::PayrollLedgerEntry,
            format!("{}:{}", order_id.as_str(), entry.entry_id()),
            format!(
                "reconcile payroll ledger entry from {}:{} for order {}",
                entry.source_event().kind().as_str(),
                entry.source_event().event_reference(),
                order_id.as_str()
            ),
            AuditCorrelationId::for_order(order_id.as_str())
                .map_err(PayrollLedgerError::AuditTrail)?,
            occurred_at,
        ) {
            *state = previous_state;
            return Err(error);
        }

        Ok(Some(entry))
    }

    pub fn employee_order_view(
        &self,
        actor: &AuthenticatedActorContext,
        order_id: &OrderId,
    ) -> Result<OrderPayrollView, PayrollLedgerError> {
        let state = lock_state(&self.state)?;
        let metadata = state
            .order_registry
            .get(order_id)
            .ok_or_else(|| PayrollLedgerError::OrderNotRegistered(order_id.clone()))?;

        if actor.role() == Role::Employee && actor.actor_id() != &metadata.employee_actor_id {
            return Err(PayrollLedgerError::NotOrderOwner {
                actor_id: actor.actor_id().clone(),
                order_id: order_id.clone(),
            });
        }

        let ledger_entries = state
            .ledger_entries_by_order
            .get(order_id)
            .cloned()
            .unwrap_or_default();
        let net_amount_minor = ledger_entries
            .iter()
            .map(PayrollLedgerEntry::signed_amount_minor)
            .sum::<i64>();

        let disputes = state
            .dispute_ids_by_order
            .get(order_id)
            .map(|ids| {
                ids.iter()
                    .filter_map(|dispute_id| state.disputes.get(dispute_id).cloned())
                    .collect::<Vec<_>>()
            })
            .unwrap_or_default();

        Ok(OrderPayrollView {
            order_id: order_id.clone(),
            employee_actor_id: metadata.employee_actor_id.clone(),
            delivery_epoch_day: metadata.delivery_epoch_day,
            currency: metadata.currency.clone(),
            net_amount_minor,
            ledger_entries,
            disputes,
        })
    }

    pub fn open_dispute(
        &self,
        actor: &AuthenticatedActorContext,
        order_id: &OrderId,
        default_owner_actor_id: &ActorId,
        reason: impl Into<String>,
        occurred_at: AuditTimestamp,
    ) -> Result<PayrollDisputeRecord, PayrollLedgerError> {
        ensure_role(actor, Role::Employee)?;

        let reason = normalize_dispute_reason(reason.into())?;
        let source_event = PayrollLedgerSourceRef::new(
            PayrollLedgerSourceKind::DisputeWorkflow,
            format!("order:{}:employee_open", order_id.as_str()),
        )?;

        let mut state = lock_state(&self.state)?;
        let previous_state = state.clone();
        let metadata = state
            .order_registry
            .get(order_id)
            .ok_or_else(|| PayrollLedgerError::OrderNotRegistered(order_id.clone()))?
            .clone();

        if actor.actor_id() != &metadata.employee_actor_id {
            return Err(PayrollLedgerError::NotOrderOwner {
                actor_id: actor.actor_id().clone(),
                order_id: order_id.clone(),
            });
        }

        state.next_dispute_sequence = state
            .next_dispute_sequence
            .checked_add(1)
            .ok_or(PayrollLedgerError::DisputeSequenceOverflow)?;
        let dispute_id =
            PayrollDisputeId::parse(format!("dsp-{:016x}", state.next_dispute_sequence))?;

        let trace_event = PayrollDisputeTraceEvent {
            occurred_at,
            actor_id: actor.actor_id().clone(),
            event_type: PayrollDisputeTraceEventType::Opened,
            status: PayrollDisputeStatus::Open,
            owner_actor_id: default_owner_actor_id.clone(),
            note: Some(reason.clone()),
            source_event,
            refund_ledger_entry_id: None,
        };
        let dispute = PayrollDisputeRecord {
            dispute_id: dispute_id.clone(),
            order_id: order_id.clone(),
            employee_actor_id: metadata.employee_actor_id,
            owner_actor_id: default_owner_actor_id.clone(),
            status: PayrollDisputeStatus::Open,
            opened_at: occurred_at,
            updated_at: occurred_at,
            trace: vec![trace_event],
        };

        state.disputes.insert(dispute_id.clone(), dispute.clone());
        state
            .dispute_ids_by_order
            .entry(order_id.clone())
            .or_default()
            .push(dispute_id.clone());

        if let Err(error) = self.append_audit_event(
            actor,
            OPEN_PAYROLL_DISPUTE_OPERATION_ID,
            AuditAction::OpenPayrollDispute,
            AuditEntityType::PayrollDispute,
            dispute_id.as_str().to_owned(),
            format!(
                "open payroll dispute for order {} reason={reason}",
                order_id.as_str()
            ),
            AuditCorrelationId::for_order(order_id.as_str())
                .map_err(PayrollLedgerError::AuditTrail)?,
            occurred_at,
        ) {
            *state = previous_state;
            return Err(error);
        }

        Ok(dispute)
    }

    pub fn assign_dispute_owner(
        &self,
        actor: &AuthenticatedActorContext,
        dispute_id: &PayrollDisputeId,
        owner_actor_id: &ActorId,
        occurred_at: AuditTimestamp,
        note: Option<String>,
    ) -> Result<PayrollDisputeRecord, PayrollLedgerError> {
        ensure_role(actor, Role::PayrollOperator)?;
        let source_event = PayrollLedgerSourceRef::new(
            PayrollLedgerSourceKind::DisputeWorkflow,
            format!("dispute:{}:assign_owner", dispute_id.as_str()),
        )?;
        let assignment_note = note
            .as_deref()
            .map(str::trim)
            .filter(|value| !value.is_empty())
            .map(str::to_owned);

        let mut state = lock_state(&self.state)?;
        let previous_state = state.clone();
        let dispute = state
            .disputes
            .get_mut(dispute_id)
            .ok_or_else(|| PayrollLedgerError::DisputeNotFound(dispute_id.clone()))?;
        if dispute.status.is_resolved() {
            return Err(PayrollLedgerError::InvalidDisputeTransition {
                dispute_id: dispute_id.clone(),
                status: dispute.status,
                operation: "ASSIGN_OWNER",
            });
        }

        dispute.owner_actor_id = owner_actor_id.clone();
        dispute.status = PayrollDisputeStatus::InReview;
        dispute.updated_at = occurred_at;
        dispute.trace.push(PayrollDisputeTraceEvent {
            occurred_at,
            actor_id: actor.actor_id().clone(),
            event_type: PayrollDisputeTraceEventType::OwnerAssigned,
            status: dispute.status,
            owner_actor_id: dispute.owner_actor_id.clone(),
            note: assignment_note.clone(),
            source_event,
            refund_ledger_entry_id: None,
        });

        if let Err(error) = self.append_audit_event(
            actor,
            ASSIGN_PAYROLL_DISPUTE_OWNER_OPERATION_ID,
            AuditAction::AssignPayrollDisputeOwner,
            AuditEntityType::PayrollDispute,
            dispute_id.as_str().to_owned(),
            format!(
                "assign payroll dispute owner={}{}",
                owner_actor_id.as_str(),
                assignment_note
                    .as_ref()
                    .map(|value| format!(" note={value}"))
                    .unwrap_or_default()
            ),
            AuditCorrelationId::for_order(dispute.order_id().as_str())
                .map_err(PayrollLedgerError::AuditTrail)?,
            occurred_at,
        ) {
            *state = previous_state;
            return Err(error);
        }

        Ok(dispute.clone())
    }

    pub fn resolve_dispute_refund(
        &self,
        actor: &AuthenticatedActorContext,
        dispute_id: &PayrollDisputeId,
        occurred_at: AuditTimestamp,
        note: impl Into<String>,
        refund_amount_minor: Option<u32>,
    ) -> Result<PayrollDisputeRecord, PayrollLedgerError> {
        ensure_role(actor, Role::PayrollOperator)?;
        let note = normalize_dispute_reason(note.into())?;

        let mut state = lock_state(&self.state)?;
        let previous_state = state.clone();

        let (order_id, employee_actor_id, delivery_epoch_day, currency, current_status) = {
            let dispute = state
                .disputes
                .get(dispute_id)
                .ok_or_else(|| PayrollLedgerError::DisputeNotFound(dispute_id.clone()))?;
            let metadata = state
                .order_registry
                .get(dispute.order_id())
                .ok_or_else(|| {
                    PayrollLedgerError::OrderNotRegistered(dispute.order_id().clone())
                })?;
            (
                dispute.order_id().clone(),
                metadata.employee_actor_id.clone(),
                metadata.delivery_epoch_day,
                metadata.currency.clone(),
                dispute.status,
            )
        };

        if current_status.is_resolved() {
            return Err(PayrollLedgerError::InvalidDisputeTransition {
                dispute_id: dispute_id.clone(),
                status: current_status,
                operation: "RESOLVE_REFUND",
            });
        }

        let current_net = current_order_net_amount_minor_locked(&state, &order_id);
        if current_net <= 0 {
            return Err(PayrollLedgerError::NoOutstandingPayrollAmount {
                order_id,
                current_net_amount_minor: current_net,
            });
        }
        let outstanding =
            u32::try_from(current_net).map_err(|_| PayrollLedgerError::AmountOutOfRange {
                amount_minor: current_net,
            })?;
        let refund_minor = refund_amount_minor.unwrap_or(outstanding);
        if refund_minor == 0 || refund_minor > outstanding {
            return Err(PayrollLedgerError::RefundAmountOutOfRange {
                requested_minor: refund_minor,
                outstanding_minor: outstanding,
            });
        }

        let source_event = PayrollLedgerSourceRef::new(
            PayrollLedgerSourceKind::DisputeWorkflow,
            format!("dispute:{}:resolve_refund", dispute_id.as_str()),
        )?;

        let refund_entry = append_ledger_entry_locked(
            &mut state,
            &order_id,
            &employee_actor_id,
            delivery_epoch_day,
            &currency,
            refund_minor,
            PayrollLedgerEntryKind::Refund,
            occurred_at,
            source_event.clone(),
        )?;

        let dispute = state
            .disputes
            .get_mut(dispute_id)
            .ok_or_else(|| PayrollLedgerError::DisputeNotFound(dispute_id.clone()))?;
        dispute.status = PayrollDisputeStatus::ResolvedRefundApproved;
        dispute.updated_at = occurred_at;
        dispute.trace.push(PayrollDisputeTraceEvent {
            occurred_at,
            actor_id: actor.actor_id().clone(),
            event_type: PayrollDisputeTraceEventType::ResolvedRefundApproved,
            status: dispute.status,
            owner_actor_id: dispute.owner_actor_id.clone(),
            note: Some(note.clone()),
            source_event,
            refund_ledger_entry_id: Some(refund_entry.entry_id()),
        });

        if let Err(error) = self.append_audit_event(
            actor,
            RESOLVE_PAYROLL_DISPUTE_OPERATION_ID,
            AuditAction::ResolvePayrollDispute,
            AuditEntityType::PayrollDispute,
            dispute_id.as_str().to_owned(),
            format!(
                "resolve payroll dispute with refundAmountMinor={} note={note}",
                refund_minor
            ),
            AuditCorrelationId::for_order(dispute.order_id().as_str())
                .map_err(PayrollLedgerError::AuditTrail)?,
            occurred_at,
        ) {
            *state = previous_state;
            return Err(error);
        }

        Ok(dispute.clone())
    }

    pub fn resolve_dispute_rejected(
        &self,
        actor: &AuthenticatedActorContext,
        dispute_id: &PayrollDisputeId,
        occurred_at: AuditTimestamp,
        note: impl Into<String>,
    ) -> Result<PayrollDisputeRecord, PayrollLedgerError> {
        ensure_role(actor, Role::PayrollOperator)?;
        let note = normalize_dispute_reason(note.into())?;
        let source_event = PayrollLedgerSourceRef::new(
            PayrollLedgerSourceKind::DisputeWorkflow,
            format!("dispute:{}:resolve_reject", dispute_id.as_str()),
        )?;

        let mut state = lock_state(&self.state)?;
        let previous_state = state.clone();
        let dispute = state
            .disputes
            .get_mut(dispute_id)
            .ok_or_else(|| PayrollLedgerError::DisputeNotFound(dispute_id.clone()))?;
        if dispute.status.is_resolved() {
            return Err(PayrollLedgerError::InvalidDisputeTransition {
                dispute_id: dispute_id.clone(),
                status: dispute.status,
                operation: "RESOLVE_REJECTED",
            });
        }

        dispute.status = PayrollDisputeStatus::ResolvedRejected;
        dispute.updated_at = occurred_at;
        dispute.trace.push(PayrollDisputeTraceEvent {
            occurred_at,
            actor_id: actor.actor_id().clone(),
            event_type: PayrollDisputeTraceEventType::ResolvedRejected,
            status: dispute.status,
            owner_actor_id: dispute.owner_actor_id.clone(),
            note: Some(note.clone()),
            source_event,
            refund_ledger_entry_id: None,
        });

        if let Err(error) = self.append_audit_event(
            actor,
            RESOLVE_PAYROLL_DISPUTE_OPERATION_ID,
            AuditAction::ResolvePayrollDispute,
            AuditEntityType::PayrollDispute,
            dispute_id.as_str().to_owned(),
            format!("resolve payroll dispute as rejected note={note}"),
            AuditCorrelationId::for_order(dispute.order_id().as_str())
                .map_err(PayrollLedgerError::AuditTrail)?,
            occurred_at,
        ) {
            *state = previous_state;
            return Err(error);
        }

        Ok(dispute.clone())
    }

    #[allow(clippy::too_many_arguments)]
    pub fn export_sftp_batch(
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
        ensure_role(actor, Role::PayrollOperator)?;
        validate_pay_period(pay_period)?;
        let cycle_key = normalize_cycle_key(cycle_key)?;
        if page == 0 || page_size == 0 || page_size > 500 {
            return Err(PayrollLedgerError::InvalidPagination { page, page_size });
        }
        let (cycle_start_epoch_day, cycle_end_epoch_day) = pay_period_bounds(pay_period)?;

        let mut state = lock_state(&self.state)?;
        let previous_state = state.clone();

        if let Some(existing_batch_id) = state.exchange_batch_ids_by_cycle.get(&cycle_key).cloned()
        {
            let batch = state
                .exchange_batches
                .get(&existing_batch_id)
                .ok_or_else(|| {
                    PayrollLedgerError::ExchangeBatchNotFound(existing_batch_id.clone())
                })?
                .clone();
            if batch.pay_period() != pay_period {
                return Err(PayrollLedgerError::CycleKeyPayPeriodConflict {
                    cycle_key,
                    expected_pay_period: batch.pay_period().to_owned(),
                    actual_pay_period: pay_period.to_owned(),
                });
            }
            let lock_state = cycle_lock_state_for(&state, &cycle_key)?;
            if lock_state == PayrollSettlementLockState::Locked {
                let (paged_records, total_items) = paginate_records(
                    batch.snapshot_records.clone(),
                    page,
                    page_size,
                    sort_by,
                    sort_order,
                );
                return Ok(PayrollExportPage {
                    items: paged_records,
                    total_items,
                    page,
                    page_size,
                    batch,
                });
            }
        }

        let records = build_deduction_records_locked(&state, pay_period)?;
        let order_ids = records
            .iter()
            .map(|record| record.order_id.clone())
            .collect::<Vec<_>>();
        let reconciliation = compute_reconciliation_metadata(&records);
        let snapshot_checksum = compute_cycle_snapshot_checksum(pay_period, &cycle_key, &records);
        let locked_orders = state
            .locked_orders_by_pay_period
            .entry(pay_period.to_owned())
            .or_default();
        for order_id in &order_ids {
            locked_orders.insert(order_id.clone());
        }

        state.next_exchange_batch_sequence = state
            .next_exchange_batch_sequence
            .checked_add(1)
            .ok_or(PayrollLedgerError::ExchangeBatchSequenceOverflow)?;
        let batch_id = PayrollExchangeBatchId::parse(format!(
            "sftp-{}-{:016x}",
            pay_period.replace('-', ""),
            state.next_exchange_batch_sequence
        ))?;
        let batch = PayrollExchangeBatch {
            batch_id: batch_id.clone(),
            pay_period: pay_period.to_owned(),
            cycle_key: cycle_key.clone(),
            generated_at: occurred_at,
            cycle_start_epoch_day,
            cycle_end_epoch_day,
            order_ids,
            snapshot_checksum,
            reconciliation,
            snapshot_records: records,
            hr_api_sync_receipt: None,
        };
        state
            .exchange_batches
            .insert(batch_id.clone(), batch.clone());
        state
            .exchange_batch_ids_by_cycle
            .insert(cycle_key.clone(), batch_id.clone());
        state
            .cycle_lock_state_by_cycle
            .insert(cycle_key.clone(), PayrollSettlementLockState::Locked);

        if let Err(error) = self.append_audit_event(
            actor,
            EXPORT_PAYROLL_SFTP_BATCH_OPERATION_ID,
            AuditAction::ExportPayrollSftpBatch,
            AuditEntityType::PayrollExchangeBatch,
            batch_id.as_str().to_owned(),
            format!("export SFTP payroll batch for payPeriod={pay_period} cycleKey={cycle_key}"),
            batch_correlation_id(&batch_id).map_err(PayrollLedgerError::AuditTrail)?,
            occurred_at,
        ) {
            *state = previous_state;
            return Err(error);
        }

        let (paged_records, total_items) = paginate_records(
            batch.snapshot_records.clone(),
            page,
            page_size,
            sort_by,
            sort_order,
        );

        Ok(PayrollExportPage {
            items: paged_records,
            total_items,
            page,
            page_size,
            batch,
        })
    }

    pub fn close_monthly_settlement(
        &self,
        actor: &AuthenticatedActorContext,
        cycle_key: Option<&str>,
        page: usize,
        page_size: usize,
        sort_by: PayrollSortField,
        sort_order: SortOrder,
        occurred_at: AuditTimestamp,
    ) -> Result<PayrollExportPage, PayrollLedgerError> {
        let pay_period = previous_pay_period_for_epoch_day(occurred_at.epoch_day());
        let cycle_key = cycle_key
            .map(normalize_cycle_key)
            .transpose()?
            .unwrap_or_else(|| default_monthly_cycle_key(&pay_period));
        self.export_sftp_batch(
            actor,
            &pay_period,
            &cycle_key,
            page,
            page_size,
            sort_by,
            sort_order,
            occurred_at,
        )
    }

    pub fn lock_cycle(
        &self,
        actor: &AuthenticatedActorContext,
        cycle_key: &str,
        reason: impl Into<String>,
        occurred_at: AuditTimestamp,
    ) -> Result<PayrollSettlementLockReceipt, PayrollLedgerError> {
        ensure_role(actor, Role::CommitteeAdmin)?;
        let cycle_key = normalize_cycle_key(cycle_key)?;
        let reason = normalize_settlement_reason(reason.into())?;

        let mut state = lock_state(&self.state)?;
        let previous_state = state.clone();

        let batch_id = state
            .exchange_batch_ids_by_cycle
            .get(&cycle_key)
            .cloned()
            .ok_or_else(|| PayrollLedgerError::SettlementCycleNotFound {
                cycle_key: cycle_key.clone(),
            })?;
        let batch = state
            .exchange_batches
            .get(&batch_id)
            .ok_or_else(|| PayrollLedgerError::ExchangeBatchNotFound(batch_id.clone()))?
            .clone();
        let current_lock_state = cycle_lock_state_for(&state, &cycle_key)?;
        if current_lock_state == PayrollSettlementLockState::Locked {
            return Err(PayrollLedgerError::SettlementCycleAlreadyLocked { cycle_key });
        }
        state
            .cycle_lock_state_by_cycle
            .insert(cycle_key.clone(), PayrollSettlementLockState::Locked);

        if let Err(error) = self.append_audit_event(
            actor,
            LOCK_PAYROLL_SETTLEMENT_CYCLE_OPERATION_ID,
            AuditAction::LockPayrollSettlementCycle,
            AuditEntityType::Settlement,
            cycle_key.clone(),
            format!(
                "lock payroll settlement cycleKey={cycle_key} payPeriod={} reason={reason}",
                batch.pay_period()
            ),
            settlement_correlation_id(&cycle_key).map_err(PayrollLedgerError::AuditTrail)?,
            occurred_at,
        ) {
            *state = previous_state;
            return Err(error);
        }

        Ok(PayrollSettlementLockReceipt {
            cycle_key,
            pay_period: batch.pay_period().to_owned(),
            lock_state: PayrollSettlementLockState::Locked,
            batch_id,
            snapshot_checksum: batch.snapshot_checksum().to_owned(),
            reason,
            changed_at: occurred_at,
            actor_id: actor.actor_id().clone(),
        })
    }

    pub fn unlock_cycle_for_recompute(
        &self,
        actor: &AuthenticatedActorContext,
        cycle_key: &str,
        reason: impl Into<String>,
        occurred_at: AuditTimestamp,
    ) -> Result<PayrollSettlementLockReceipt, PayrollLedgerError> {
        ensure_role(actor, Role::CommitteeAdmin)?;
        let cycle_key = normalize_cycle_key(cycle_key)?;
        let reason = normalize_settlement_reason(reason.into())?;

        let mut state = lock_state(&self.state)?;
        let previous_state = state.clone();

        let batch_id = state
            .exchange_batch_ids_by_cycle
            .get(&cycle_key)
            .cloned()
            .ok_or_else(|| PayrollLedgerError::SettlementCycleNotFound {
                cycle_key: cycle_key.clone(),
            })?;
        let batch = state
            .exchange_batches
            .get(&batch_id)
            .ok_or_else(|| PayrollLedgerError::ExchangeBatchNotFound(batch_id.clone()))?
            .clone();
        let current_lock_state = cycle_lock_state_for(&state, &cycle_key)?;
        if current_lock_state == PayrollSettlementLockState::Unlocked {
            return Err(PayrollLedgerError::SettlementCycleAlreadyUnlocked { cycle_key });
        }
        state
            .cycle_lock_state_by_cycle
            .insert(cycle_key.clone(), PayrollSettlementLockState::Unlocked);

        if let Err(error) = self.append_audit_event(
            actor,
            UNLOCK_PAYROLL_SETTLEMENT_CYCLE_OPERATION_ID,
            AuditAction::UnlockPayrollSettlementCycle,
            AuditEntityType::Settlement,
            cycle_key.clone(),
            format!(
                "unlock payroll settlement cycleKey={cycle_key} payPeriod={} reason={reason}",
                batch.pay_period()
            ),
            settlement_correlation_id(&cycle_key).map_err(PayrollLedgerError::AuditTrail)?,
            occurred_at,
        ) {
            *state = previous_state;
            return Err(error);
        }

        Ok(PayrollSettlementLockReceipt {
            cycle_key,
            pay_period: batch.pay_period().to_owned(),
            lock_state: PayrollSettlementLockState::Unlocked,
            batch_id,
            snapshot_checksum: batch.snapshot_checksum().to_owned(),
            reason,
            changed_at: occurred_at,
            actor_id: actor.actor_id().clone(),
        })
    }

    pub fn sync_hr_api_adjunct(
        &self,
        actor: &AuthenticatedActorContext,
        batch_id: &PayrollExchangeBatchId,
        outcome: PayrollHrApiSyncOutcome,
        note: Option<String>,
        occurred_at: AuditTimestamp,
    ) -> Result<PayrollExchangeBatch, PayrollLedgerError> {
        ensure_role(actor, Role::PayrollOperator)?;
        let note = match note {
            Some(value) => Some(normalize_dispute_reason(value)?),
            None => None,
        };

        let mut state = lock_state(&self.state)?;
        let previous_state = state.clone();
        let mut failed_order_ids = Vec::new();
        {
            let batch = state
                .exchange_batches
                .get_mut(batch_id)
                .ok_or_else(|| PayrollLedgerError::ExchangeBatchNotFound(batch_id.clone()))?;

            if batch.hr_api_sync_receipt.is_none() {
                let status = match outcome {
                    PayrollHrApiSyncOutcome::Succeeded => HrApiSyncStatus::Succeeded,
                    PayrollHrApiSyncOutcome::Failed => HrApiSyncStatus::Failed,
                };
                if status == HrApiSyncStatus::Failed {
                    failed_order_ids = batch.order_ids().to_vec();
                }
                batch.hr_api_sync_receipt = Some(HrApiSyncReceipt {
                    synced_at: occurred_at,
                    actor_id: actor.actor_id().clone(),
                    status,
                });
            }
        }
        for order_id in failed_order_ids {
            state.failed_deduction_orders.insert(order_id);
        }
        let batch = state
            .exchange_batches
            .get(batch_id)
            .ok_or_else(|| PayrollLedgerError::ExchangeBatchNotFound(batch_id.clone()))?
            .clone();
        let receipt_status = batch
            .hr_api_sync_receipt()
            .map(HrApiSyncReceipt::status)
            .unwrap_or(HrApiSyncStatus::NotSynced);
        if let Err(error) = self.append_audit_event(
            actor,
            SYNC_PAYROLL_HR_API_OPERATION_ID,
            AuditAction::SyncPayrollHrApiAdjunct,
            AuditEntityType::PayrollExchangeBatch,
            batch_id.as_str().to_owned(),
            format!(
                "sync HR API adjunct status={}{}",
                receipt_status.as_str(),
                note.as_ref()
                    .map(|value| format!(" note={value}"))
                    .unwrap_or_default()
            ),
            batch_correlation_id(batch_id).map_err(PayrollLedgerError::AuditTrail)?,
            occurred_at,
        ) {
            *state = previous_state;
            return Err(error);
        }

        Ok(batch)
    }

    pub fn purge_expired_data(
        &self,
        actor: &AuthenticatedActorContext,
        as_of: AuditTimestamp,
    ) -> Result<PayrollPurgeReport, PayrollLedgerError> {
        ensure_role(actor, Role::CommitteeAdmin)?;

        let mut state = lock_state(&self.state)?;
        let previous_state = state.clone();

        let ledger_retention_days = i32::from(state.retention_policy.ledger_retention_days());
        let dispute_retention_days = i32::from(state.retention_policy.dispute_retention_days());
        let exchange_retention_days = i32::from(state.retention_policy.exchange_retention_days());

        let mut purged_ledger_entries = 0usize;
        for entries in state.ledger_entries_by_order.values_mut() {
            let before = entries.len();
            entries.retain(|entry| as_of.days_since(entry.occurred_at()) <= ledger_retention_days);
            purged_ledger_entries += before.saturating_sub(entries.len());
        }
        state
            .ledger_entries_by_order
            .retain(|_, entries| !entries.is_empty());

        let mut purged_disputes = 0usize;
        state.disputes.retain(|_, dispute| {
            let keep = as_of.days_since(dispute.updated_at()) <= dispute_retention_days;
            if !keep {
                purged_disputes += 1;
            }
            keep
        });
        let active_dispute_ids = state.disputes.keys().cloned().collect::<BTreeSet<_>>();
        state.dispute_ids_by_order.retain(|_, dispute_ids| {
            dispute_ids.retain(|dispute_id| active_dispute_ids.contains(dispute_id));
            !dispute_ids.is_empty()
        });

        let mut purged_exchange_batches = 0usize;
        state.exchange_batches.retain(|_, batch| {
            let keep = as_of.days_since(batch.generated_at()) <= exchange_retention_days;
            if !keep {
                purged_exchange_batches += 1;
            }
            keep
        });
        let active_exchange_batch_ids = state
            .exchange_batches
            .keys()
            .cloned()
            .collect::<BTreeSet<_>>();
        state
            .exchange_batch_ids_by_cycle
            .retain(|_, batch_id| active_exchange_batch_ids.contains(batch_id));
        let active_cycle_keys = state
            .exchange_batch_ids_by_cycle
            .keys()
            .cloned()
            .collect::<BTreeSet<_>>();
        state
            .cycle_lock_state_by_cycle
            .retain(|cycle_key, _| active_cycle_keys.contains(cycle_key));

        let ledger_order_ids = state
            .ledger_entries_by_order
            .keys()
            .cloned()
            .collect::<BTreeSet<_>>();
        let dispute_order_ids = state
            .dispute_ids_by_order
            .keys()
            .cloned()
            .collect::<BTreeSet<_>>();
        state.order_registry.retain(|order_id, _| {
            ledger_order_ids.contains(order_id) || dispute_order_ids.contains(order_id)
        });

        let registered_order_ids = state
            .order_registry
            .keys()
            .cloned()
            .collect::<BTreeSet<_>>();
        for locked_orders in state.locked_orders_by_pay_period.values_mut() {
            locked_orders.retain(|order_id| registered_order_ids.contains(order_id));
        }
        state
            .locked_orders_by_pay_period
            .retain(|_, order_ids| !order_ids.is_empty());
        state
            .failed_deduction_orders
            .retain(|order_id| registered_order_ids.contains(order_id));

        if let Err(error) = self.append_audit_event(
            actor,
            PURGE_PAYROLL_DATA_OPERATION_ID,
            AuditAction::PurgePayrollData,
            AuditEntityType::PayrollDataRetention,
            format!("payroll-retention:{}", as_of.epoch_day()),
            format!(
                "purge payroll data asOfEpochDay={} ledgerRetentionDays={} disputeRetentionDays={} exchangeRetentionDays={}",
                as_of.epoch_day(),
                state.retention_policy.ledger_retention_days(),
                state.retention_policy.dispute_retention_days(),
                state.retention_policy.exchange_retention_days()
            ),
            AuditCorrelationId::parse(format!("payroll-retention:{}", as_of.epoch_day()))
                .map_err(PayrollLedgerError::AuditTrail)?,
            as_of,
        ) {
            *state = previous_state;
            return Err(error);
        }

        Ok(PayrollPurgeReport {
            purged_ledger_entries,
            purged_disputes,
            purged_exchange_batches,
        })
    }

    fn append_audit_event(
        &self,
        actor: &AuthenticatedActorContext,
        operation_id: &str,
        action: AuditAction,
        entity_type: AuditEntityType,
        entity_id: String,
        reason: String,
        correlation_id: AuditCorrelationId,
        occurred_at: AuditTimestamp,
    ) -> Result<(), PayrollLedgerError> {
        let write = AuditEvidenceWrite::new_with_reason(
            occurred_at,
            AuditIdentityLink::from_actor(actor, operation_id),
            action,
            AuditEntityRef::new(entity_type, entity_id).map_err(PayrollLedgerError::AuditTrail)?,
            reason,
            correlation_id,
        )
        .map_err(PayrollLedgerError::AuditTrail)?;
        self.audit_trail
            .append(write)
            .map_err(PayrollLedgerError::AuditTrail)?;
        Ok(())
    }
}

#[derive(Debug, Clone, Default)]
struct PayrollLedgerState {
    retention_policy: PayrollRetentionPolicy,
    next_entry_id: u64,
    next_dispute_sequence: u64,
    next_exchange_batch_sequence: u64,
    order_registry: BTreeMap<OrderId, OrderPayrollMetadata>,
    ledger_entries_by_order: BTreeMap<OrderId, Vec<PayrollLedgerEntry>>,
    disputes: BTreeMap<PayrollDisputeId, PayrollDisputeRecord>,
    dispute_ids_by_order: BTreeMap<OrderId, Vec<PayrollDisputeId>>,
    locked_orders_by_pay_period: BTreeMap<String, BTreeSet<OrderId>>,
    failed_deduction_orders: BTreeSet<OrderId>,
    exchange_batch_ids_by_cycle: BTreeMap<String, PayrollExchangeBatchId>,
    cycle_lock_state_by_cycle: BTreeMap<String, PayrollSettlementLockState>,
    exchange_batches: BTreeMap<PayrollExchangeBatchId, PayrollExchangeBatch>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
struct OrderPayrollMetadata {
    employee_actor_id: ActorId,
    employee_employment_status: EmploymentStatus,
    delivery_epoch_day: i32,
    currency: String,
}

fn ensure_role(
    actor: &AuthenticatedActorContext,
    required_role: Role,
) -> Result<(), PayrollLedgerError> {
    if actor.role() != required_role {
        return Err(PayrollLedgerError::UnauthorizedRole {
            expected: required_role,
            actual: actor.role(),
        });
    }
    Ok(())
}

fn normalize_dispute_reason(value: String) -> Result<String, PayrollLedgerError> {
    let trimmed = value.trim();
    if trimmed.is_empty() {
        return Err(PayrollLedgerError::InvalidDisputeReason(
            "reason must not be empty".to_owned(),
        ));
    }
    if trimmed.chars().count() > MAX_DISPUTE_REASON_LENGTH {
        return Err(PayrollLedgerError::InvalidDisputeReason(format!(
            "reason must be at most {MAX_DISPUTE_REASON_LENGTH} characters"
        )));
    }
    Ok(trimmed.to_owned())
}

fn normalize_settlement_reason(value: String) -> Result<String, PayrollLedgerError> {
    let trimmed = value.trim();
    if trimmed.is_empty() {
        return Err(PayrollLedgerError::InvalidSettlementReason(
            "reason must not be empty".to_owned(),
        ));
    }
    if trimmed.chars().count() > MAX_DISPUTE_REASON_LENGTH {
        return Err(PayrollLedgerError::InvalidSettlementReason(format!(
            "reason must be at most {MAX_DISPUTE_REASON_LENGTH} characters"
        )));
    }
    Ok(trimmed.to_owned())
}

fn lock_state(
    state: &Arc<Mutex<PayrollLedgerState>>,
) -> Result<std::sync::MutexGuard<'_, PayrollLedgerState>, PayrollLedgerError> {
    state.lock().map_err(|_| PayrollLedgerError::StatePoisoned)
}

fn upsert_order_registry(
    state: &mut PayrollLedgerState,
    order_id: &OrderId,
    employee_actor_id: &ActorId,
    employee_employment_status: EmploymentStatus,
    delivery_epoch_day: i32,
    currency: &str,
) -> Result<(), PayrollLedgerError> {
    let candidate = OrderPayrollMetadata {
        employee_actor_id: employee_actor_id.clone(),
        employee_employment_status,
        delivery_epoch_day,
        currency: currency.to_owned(),
    };
    if let Some(existing) = state.order_registry.get_mut(order_id) {
        if existing.employee_actor_id != candidate.employee_actor_id {
            return Err(PayrollLedgerError::OrderOwnerMismatch {
                order_id: order_id.clone(),
                existing_owner: existing.employee_actor_id.clone(),
                attempted_owner: candidate.employee_actor_id,
            });
        }
        if existing.currency != candidate.currency {
            return Err(PayrollLedgerError::OrderCurrencyMismatch {
                order_id: order_id.clone(),
                existing_currency: existing.currency.clone(),
                attempted_currency: candidate.currency,
            });
        }
        if existing.delivery_epoch_day != candidate.delivery_epoch_day {
            return Err(PayrollLedgerError::OrderDeliveryDateMismatch {
                order_id: order_id.clone(),
                existing_delivery_epoch_day: existing.delivery_epoch_day,
                attempted_delivery_epoch_day: candidate.delivery_epoch_day,
            });
        }
        if candidate.employee_employment_status == EmploymentStatus::Terminated {
            existing.employee_employment_status = EmploymentStatus::Terminated;
        }
        return Ok(());
    }

    state.order_registry.insert(order_id.clone(), candidate);
    Ok(())
}

#[allow(clippy::too_many_arguments)]
fn append_ledger_entry_locked(
    state: &mut PayrollLedgerState,
    order_id: &OrderId,
    employee_actor_id: &ActorId,
    delivery_epoch_day: i32,
    currency: &str,
    amount_minor: u32,
    kind: PayrollLedgerEntryKind,
    occurred_at: AuditTimestamp,
    source_event: PayrollLedgerSourceRef,
) -> Result<PayrollLedgerEntry, PayrollLedgerError> {
    state.next_entry_id = state
        .next_entry_id
        .checked_add(1)
        .ok_or(PayrollLedgerError::LedgerSequenceOverflow)?;
    let entry = PayrollLedgerEntry {
        entry_id: state.next_entry_id,
        order_id: order_id.clone(),
        employee_actor_id: employee_actor_id.clone(),
        delivery_epoch_day,
        amount: Money::new(currency.to_owned(), amount_minor)
            .map_err(|error| PayrollLedgerError::InvalidMoney(error.to_string()))?,
        kind,
        occurred_at,
        source_event,
    };
    state
        .ledger_entries_by_order
        .entry(order_id.clone())
        .or_default()
        .push(entry.clone());
    Ok(entry)
}

fn current_order_net_amount_minor_locked(state: &PayrollLedgerState, order_id: &OrderId) -> i64 {
    state
        .ledger_entries_by_order
        .get(order_id)
        .map(|entries| {
            entries
                .iter()
                .map(PayrollLedgerEntry::signed_amount_minor)
                .sum::<i64>()
        })
        .unwrap_or(0)
}

fn build_deduction_records_locked(
    state: &PayrollLedgerState,
    pay_period: &str,
) -> Result<Vec<PayrollDeductionRecord>, PayrollLedgerError> {
    let mut records = Vec::new();

    for (order_id, metadata) in &state.order_registry {
        if format_pay_period(metadata.delivery_epoch_day) != pay_period {
            continue;
        }

        let entries = state
            .ledger_entries_by_order
            .get(order_id)
            .cloned()
            .unwrap_or_default();
        if entries.is_empty() {
            continue;
        }

        let net = entries
            .iter()
            .map(PayrollLedgerEntry::signed_amount_minor)
            .sum::<i64>();

        let amount_minor = if net <= 0 {
            0
        } else {
            u32::try_from(net)
                .map_err(|_| PayrollLedgerError::AmountOutOfRange { amount_minor: net })?
        };
        let amount = Money::new(metadata.currency.clone(), amount_minor)
            .map_err(|error| PayrollLedgerError::InvalidMoney(error.to_string()))?;

        let dispute_status = state.dispute_ids_by_order.get(order_id).and_then(|ids| {
            ids.iter()
                .filter_map(|id| state.disputes.get(id))
                .max_by_key(|dispute| dispute.updated_at())
                .map(|dispute| dispute.status())
        });

        let status = if net <= 0 {
            PayrollDeductionStatus::Refunded
        } else if metadata.employee_employment_status == EmploymentStatus::Terminated {
            PayrollDeductionStatus::EmployeeTerminated
        } else if state.failed_deduction_orders.contains(order_id) {
            PayrollDeductionStatus::DeductionFailed
        } else if matches!(
            dispute_status,
            Some(PayrollDisputeStatus::Open | PayrollDisputeStatus::InReview)
        ) {
            PayrollDeductionStatus::Disputed
        } else if state
            .locked_orders_by_pay_period
            .get(pay_period)
            .is_some_and(|locked| locked.contains(order_id))
        {
            PayrollDeductionStatus::Locked
        } else {
            PayrollDeductionStatus::Ready
        };

        records.push(PayrollDeductionRecord {
            employee_actor_id: metadata.employee_actor_id.clone(),
            order_id: order_id.clone(),
            delivery_epoch_day: metadata.delivery_epoch_day,
            amount,
            pay_period: pay_period.to_owned(),
            status,
            dispute_status,
            source_entry_ids: entries.iter().map(PayrollLedgerEntry::entry_id).collect(),
        });
    }

    Ok(records)
}

fn required_exception_classes() -> Vec<PayrollExceptionClass> {
    vec![
        PayrollExceptionClass::Disputed,
        PayrollExceptionClass::DeductionFailed,
        PayrollExceptionClass::EmployeeTerminated,
        PayrollExceptionClass::Refunded,
    ]
}

fn compute_reconciliation_metadata(
    records: &[PayrollDeductionRecord],
) -> PayrollReconciliationMetadata {
    let mut total_amount_minor = 0u64;
    let mut total_source_entries = 0usize;
    let mut ready_records = 0usize;
    let mut locked_records = 0usize;
    let mut refunded_records = 0usize;
    let mut disputed_records = 0usize;
    let mut deduction_failed_records = 0usize;
    let mut employee_terminated_records = 0usize;
    let mut present_exception_classes = BTreeSet::new();

    for record in records {
        total_amount_minor =
            total_amount_minor.saturating_add(u64::from(record.amount().amount_minor()));
        total_source_entries = total_source_entries.saturating_add(record.source_entry_ids().len());
        match record.status() {
            PayrollDeductionStatus::Ready => {
                ready_records = ready_records.saturating_add(1);
            }
            PayrollDeductionStatus::Locked => {
                locked_records = locked_records.saturating_add(1);
            }
            PayrollDeductionStatus::Refunded => {
                refunded_records = refunded_records.saturating_add(1);
                present_exception_classes.insert(PayrollExceptionClass::Refunded);
            }
            PayrollDeductionStatus::Disputed => {
                disputed_records = disputed_records.saturating_add(1);
                present_exception_classes.insert(PayrollExceptionClass::Disputed);
            }
            PayrollDeductionStatus::DeductionFailed => {
                deduction_failed_records = deduction_failed_records.saturating_add(1);
                present_exception_classes.insert(PayrollExceptionClass::DeductionFailed);
            }
            PayrollDeductionStatus::EmployeeTerminated => {
                employee_terminated_records = employee_terminated_records.saturating_add(1);
                present_exception_classes.insert(PayrollExceptionClass::EmployeeTerminated);
            }
        }
    }

    PayrollReconciliationMetadata {
        total_records: records.len(),
        total_amount_minor,
        total_source_entries,
        ready_records,
        locked_records,
        refunded_records,
        disputed_records,
        deduction_failed_records,
        employee_terminated_records,
        required_exception_classes: required_exception_classes(),
        present_exception_classes: present_exception_classes.into_iter().collect(),
    }
}

fn sort_deduction_records(
    records: &mut [PayrollDeductionRecord],
    sort_by: PayrollSortField,
    sort_order: SortOrder,
) {
    records.sort_by(|left, right| {
        let ordering = match sort_by {
            PayrollSortField::EmployeeActorId => left
                .employee_actor_id()
                .as_str()
                .cmp(right.employee_actor_id().as_str()),
            PayrollSortField::AmountMinor => left
                .amount()
                .amount_minor()
                .cmp(&right.amount().amount_minor()),
            PayrollSortField::DeliveryDate => {
                left.delivery_epoch_day().cmp(&right.delivery_epoch_day())
            }
        }
        .then_with(|| left.order_id().cmp(right.order_id()));

        match sort_order {
            SortOrder::Asc => ordering,
            SortOrder::Desc => ordering.reverse(),
        }
    });
}

fn paginate_records(
    mut records: Vec<PayrollDeductionRecord>,
    page: usize,
    page_size: usize,
    sort_by: PayrollSortField,
    sort_order: SortOrder,
) -> (Vec<PayrollDeductionRecord>, usize) {
    sort_deduction_records(&mut records, sort_by, sort_order);
    let total_items = records.len();
    let start = page.saturating_sub(1).saturating_mul(page_size);
    let end = start.saturating_add(page_size).min(total_items);
    let paged_records = if start >= total_items {
        Vec::new()
    } else {
        records[start..end].to_vec()
    };
    (paged_records, total_items)
}

fn compute_cycle_snapshot_checksum(
    pay_period: &str,
    cycle_key: &str,
    records: &[PayrollDeductionRecord],
) -> String {
    let mut canonical = records.to_vec();
    canonical.sort_by(|left, right| {
        left.order_id().cmp(right.order_id()).then_with(|| {
            left.employee_actor_id()
                .as_str()
                .cmp(right.employee_actor_id().as_str())
        })
    });
    let mut hasher = Sha256::new();
    hasher.update(pay_period.as_bytes());
    hasher.update([b'\n']);
    hasher.update(cycle_key.as_bytes());
    hasher.update([b'\n']);
    for record in canonical {
        hasher.update(record.order_id().as_str().as_bytes());
        hasher.update([b'|']);
        hasher.update(record.employee_actor_id().as_str().as_bytes());
        hasher.update([b'|']);
        hasher.update(record.delivery_epoch_day().to_string().as_bytes());
        hasher.update([b'|']);
        hasher.update(record.amount().currency().as_bytes());
        hasher.update([b'|']);
        hasher.update(record.amount().amount_minor().to_string().as_bytes());
        hasher.update([b'|']);
        hasher.update(record.status().as_str().as_bytes());
        hasher.update([b'|']);
        hasher.update(
            record
                .dispute_status()
                .map(PayrollDisputeStatus::as_str)
                .unwrap_or("NONE")
                .as_bytes(),
        );
        hasher.update([b'|']);
        for entry_id in record.source_entry_ids() {
            hasher.update(entry_id.to_string().as_bytes());
            hasher.update([b',']);
        }
        hasher.update([b'\n']);
    }
    hasher
        .finalize()
        .iter()
        .map(|byte| format!("{byte:02x}"))
        .collect()
}

fn validate_pay_period(pay_period: &str) -> Result<(), PayrollLedgerError> {
    parse_pay_period_parts(pay_period).map(|_| ())
}

fn parse_pay_period_parts(pay_period: &str) -> Result<(i32, u32), PayrollLedgerError> {
    let trimmed = pay_period.trim();
    let mut parts = trimmed.split('-');
    let Some(year_part) = parts.next() else {
        return Err(PayrollLedgerError::InvalidPayPeriod(pay_period.to_owned()));
    };
    let Some(month_part) = parts.next() else {
        return Err(PayrollLedgerError::InvalidPayPeriod(pay_period.to_owned()));
    };
    if parts.next().is_some() {
        return Err(PayrollLedgerError::InvalidPayPeriod(pay_period.to_owned()));
    }

    if year_part.len() != 4 || !year_part.chars().all(|ch| ch.is_ascii_digit()) {
        return Err(PayrollLedgerError::InvalidPayPeriod(pay_period.to_owned()));
    }
    if month_part.len() != 2 || !month_part.chars().all(|ch| ch.is_ascii_digit()) {
        return Err(PayrollLedgerError::InvalidPayPeriod(pay_period.to_owned()));
    }
    let month = month_part
        .parse::<u32>()
        .map_err(|_| PayrollLedgerError::InvalidPayPeriod(pay_period.to_owned()))?;
    if !(1..=12).contains(&month) {
        return Err(PayrollLedgerError::InvalidPayPeriod(pay_period.to_owned()));
    }
    let year = year_part
        .parse::<i32>()
        .map_err(|_| PayrollLedgerError::InvalidPayPeriod(pay_period.to_owned()))?;
    Ok((year, month))
}

fn pay_period_bounds(pay_period: &str) -> Result<(i32, i32), PayrollLedgerError> {
    let (year, month) = parse_pay_period_parts(pay_period)?;
    let cycle_start_epoch_day = i32::try_from(days_from_civil(year, month, 1))
        .map_err(|_| PayrollLedgerError::InvalidPayPeriod(pay_period.to_owned()))?;
    let (next_year, next_month) = if month == 12 {
        (year.saturating_add(1), 1)
    } else {
        (year, month + 1)
    };
    let next_month_start_epoch_day = i32::try_from(days_from_civil(next_year, next_month, 1))
        .map_err(|_| PayrollLedgerError::InvalidPayPeriod(pay_period.to_owned()))?;
    let cycle_end_epoch_day = next_month_start_epoch_day
        .checked_sub(1)
        .ok_or_else(|| PayrollLedgerError::InvalidPayPeriod(pay_period.to_owned()))?;
    Ok((cycle_start_epoch_day, cycle_end_epoch_day))
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

fn default_monthly_cycle_key(pay_period: &str) -> String {
    format!("monthly-{pay_period}")
}

fn normalize_cycle_key(value: &str) -> Result<String, PayrollLedgerError> {
    let trimmed = value.trim();
    if trimmed.is_empty() {
        return Err(PayrollLedgerError::InvalidCycleKey(
            "cycle key must not be empty".to_owned(),
        ));
    }
    if trimmed.len() > MAX_CYCLE_KEY_LENGTH {
        return Err(PayrollLedgerError::InvalidCycleKey(format!(
            "cycle key must be at most {MAX_CYCLE_KEY_LENGTH} characters"
        )));
    }
    if !trimmed
        .chars()
        .all(|character| character.is_ascii_alphanumeric() || matches!(character, '-' | '_' | '.'))
    {
        return Err(PayrollLedgerError::InvalidCycleKey(
            "cycle key may contain only ASCII letters, digits, '-', '_' or '.'".to_owned(),
        ));
    }
    Ok(trimmed.to_owned())
}

fn batch_correlation_id(
    batch_id: &PayrollExchangeBatchId,
) -> Result<AuditCorrelationId, AuditTrailError> {
    AuditCorrelationId::parse(format!(
        "{PAYROLL_EXCHANGE_CORRELATION_PREFIX}:{}",
        batch_id.as_str()
    ))
}

fn settlement_correlation_id(cycle_key: &str) -> Result<AuditCorrelationId, AuditTrailError> {
    AuditCorrelationId::parse(format!(
        "{PAYROLL_SETTLEMENT_CORRELATION_PREFIX}:{cycle_key}"
    ))
}

fn cycle_lock_state_for(
    state: &PayrollLedgerState,
    cycle_key: &str,
) -> Result<PayrollSettlementLockState, PayrollLedgerError> {
    state
        .cycle_lock_state_by_cycle
        .get(cycle_key)
        .copied()
        .ok_or_else(|| PayrollLedgerError::SettlementCycleLockStateMissing {
            cycle_key: cycle_key.to_owned(),
        })
}

fn format_pay_period(epoch_day: i32) -> String {
    let (year, month, _) = civil_from_days(i64::from(epoch_day));
    format!("{year:04}-{month:02}")
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

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum PayrollLedgerError {
    InvalidOperationId,
    InvalidRetentionPolicy,
    InvalidSourceEventReference,
    InvalidDisputeId,
    InvalidExchangeBatchId,
    InvalidDisputeReason(String),
    InvalidPayPeriod(String),
    InvalidCycleKey(String),
    InvalidSettlementReason(String),
    InvalidPagination {
        page: usize,
        page_size: usize,
    },
    InvalidMoney(String),
    UnauthorizedRole {
        expected: Role,
        actual: Role,
    },
    NotOrderOwner {
        actor_id: ActorId,
        order_id: OrderId,
    },
    OrderNotRegistered(OrderId),
    OrderOwnerMismatch {
        order_id: OrderId,
        existing_owner: ActorId,
        attempted_owner: ActorId,
    },
    OrderCurrencyMismatch {
        order_id: OrderId,
        existing_currency: String,
        attempted_currency: String,
    },
    OrderDeliveryDateMismatch {
        order_id: OrderId,
        existing_delivery_epoch_day: i32,
        attempted_delivery_epoch_day: i32,
    },
    DisputeNotFound(PayrollDisputeId),
    ExchangeBatchNotFound(PayrollExchangeBatchId),
    SettlementCycleNotFound {
        cycle_key: String,
    },
    CycleKeyPayPeriodConflict {
        cycle_key: String,
        expected_pay_period: String,
        actual_pay_period: String,
    },
    SettlementCycleAlreadyLocked {
        cycle_key: String,
    },
    SettlementCycleAlreadyUnlocked {
        cycle_key: String,
    },
    SettlementCycleLockStateMissing {
        cycle_key: String,
    },
    InvalidDisputeTransition {
        dispute_id: PayrollDisputeId,
        status: PayrollDisputeStatus,
        operation: &'static str,
    },
    NoOutstandingPayrollAmount {
        order_id: OrderId,
        current_net_amount_minor: i64,
    },
    RefundAmountOutOfRange {
        requested_minor: u32,
        outstanding_minor: u32,
    },
    AmountOutOfRange {
        amount_minor: i64,
    },
    LedgerSequenceOverflow,
    DisputeSequenceOverflow,
    ExchangeBatchSequenceOverflow,
    StatePoisoned,
    AuditTrail(AuditTrailError),
}

impl fmt::Display for PayrollLedgerError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::InvalidOperationId => f.write_str("operation id must not be empty"),
            Self::InvalidRetentionPolicy => f.write_str(
                "payroll retention policy requires positive ledger/dispute/exchange retention days",
            ),
            Self::InvalidSourceEventReference => {
                f.write_str("payroll source event reference must not be empty")
            }
            Self::InvalidDisputeId => f.write_str("payroll dispute id must not be empty"),
            Self::InvalidExchangeBatchId => {
                f.write_str("payroll exchange batch id must not be empty")
            }
            Self::InvalidDisputeReason(message) => write!(f, "invalid dispute reason: {message}"),
            Self::InvalidPayPeriod(pay_period) => write!(
                f,
                "invalid pay period `{pay_period}`: expected YYYY-MM with valid month"
            ),
            Self::InvalidCycleKey(message) => write!(f, "invalid payroll cycle key: {message}"),
            Self::InvalidSettlementReason(message) => {
                write!(f, "invalid settlement reason: {message}")
            }
            Self::InvalidPagination { page, page_size } => write!(
                f,
                "invalid pagination: page must be >= 1 and pageSize must be 1..=500, got page={page} pageSize={page_size}"
            ),
            Self::InvalidMoney(message) => write!(f, "invalid payroll money amount: {message}"),
            Self::UnauthorizedRole { expected, actual } => write!(
                f,
                "operation requires role {expected:?}, but actor has role {actual:?}"
            ),
            Self::NotOrderOwner { actor_id, order_id } => write!(
                f,
                "actor {actor_id} does not own payroll order {order_id}"
            ),
            Self::OrderNotRegistered(order_id) => {
                write!(f, "order {order_id} is missing from payroll ledger registry")
            }
            Self::OrderOwnerMismatch {
                order_id,
                existing_owner,
                attempted_owner,
            } => write!(
                f,
                "order {order_id} is owned by {existing_owner}, cannot reconcile with {attempted_owner}"
            ),
            Self::OrderCurrencyMismatch {
                order_id,
                existing_currency,
                attempted_currency,
            } => write!(
                f,
                "order {order_id} uses currency {existing_currency}, cannot reconcile with {attempted_currency}"
            ),
            Self::OrderDeliveryDateMismatch {
                order_id,
                existing_delivery_epoch_day,
                attempted_delivery_epoch_day,
            } => write!(
                f,
                "order {order_id} targets delivery day {existing_delivery_epoch_day}, cannot reconcile with {attempted_delivery_epoch_day}"
            ),
            Self::DisputeNotFound(dispute_id) => write!(f, "payroll dispute {dispute_id} not found"),
            Self::ExchangeBatchNotFound(batch_id) => {
                write!(f, "payroll exchange batch {batch_id} not found")
            }
            Self::SettlementCycleNotFound { cycle_key } => {
                write!(f, "payroll settlement cycle {cycle_key} not found")
            }
            Self::CycleKeyPayPeriodConflict {
                cycle_key,
                expected_pay_period,
                actual_pay_period,
            } => write!(
                f,
                "payroll cycle key {cycle_key} is bound to pay period {expected_pay_period}, cannot use with {actual_pay_period}"
            ),
            Self::SettlementCycleAlreadyLocked { cycle_key } => {
                write!(f, "payroll settlement cycle {cycle_key} is already locked")
            }
            Self::SettlementCycleAlreadyUnlocked { cycle_key } => {
                write!(f, "payroll settlement cycle {cycle_key} is already unlocked")
            }
            Self::SettlementCycleLockStateMissing { cycle_key } => write!(
                f,
                "payroll settlement cycle {cycle_key} is missing explicit lock state"
            ),
            Self::InvalidDisputeTransition {
                dispute_id,
                status,
                operation,
            } => write!(
                f,
                "payroll dispute {dispute_id} cannot perform operation {operation} from status {status}"
            ),
            Self::NoOutstandingPayrollAmount {
                order_id,
                current_net_amount_minor,
            } => write!(
                f,
                "order {order_id} has no outstanding payroll deduction to refund (net amount {current_net_amount_minor})"
            ),
            Self::RefundAmountOutOfRange {
                requested_minor,
                outstanding_minor,
            } => write!(
                f,
                "refund amount {requested_minor} is invalid for outstanding amount {outstanding_minor}"
            ),
            Self::AmountOutOfRange { amount_minor } => write!(
                f,
                "amount minor value {amount_minor} is outside supported u32 range"
            ),
            Self::LedgerSequenceOverflow => {
                f.write_str("payroll ledger sequence overflowed")
            }
            Self::DisputeSequenceOverflow => {
                f.write_str("payroll dispute sequence overflowed")
            }
            Self::ExchangeBatchSequenceOverflow => {
                f.write_str("payroll exchange batch sequence overflowed")
            }
            Self::StatePoisoned => {
                f.write_str("payroll ledger state is poisoned due to a previous panic")
            }
            Self::AuditTrail(error) => write!(f, "payroll audit trail write failed: {error}"),
        }
    }
}

impl std::error::Error for PayrollLedgerError {}

trait AuditTimestampDaysSinceExt {
    fn days_since(self, earlier: AuditTimestamp) -> i32;
}

impl AuditTimestampDaysSinceExt for AuditTimestamp {
    fn days_since(self, earlier: AuditTimestamp) -> i32 {
        self.epoch_day() - earlier.epoch_day()
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::identity::{AuthenticationSource, PlantId, PlantScope};

    fn actor_id(value: &str) -> ActorId {
        ActorId::parse(value).expect("actor id should be valid")
    }

    fn payroll_actor() -> AuthenticatedActorContext {
        AuthenticatedActorContext::new(
            actor_id("payroll-ledger-test"),
            Role::PayrollOperator,
            PlantScope::all(),
            AuthenticationSource::CorporateSso,
        )
        .expect("payroll actor should be valid")
    }

    fn employee_actor() -> AuthenticatedActorContext {
        AuthenticatedActorContext::new(
            actor_id("employee-ledger-test"),
            Role::Employee,
            PlantScope::restricted(vec![PlantId::parse("fab-a").expect("plant id should parse")])
                .expect("restricted scope should be valid"),
            AuthenticationSource::CorporateSso,
        )
        .expect("employee actor should be valid")
    }

    fn terminated_employee_actor() -> AuthenticatedActorContext {
        AuthenticatedActorContext::new_with_employment_status(
            actor_id("employee-ledger-terminated"),
            Role::Employee,
            PlantScope::restricted(vec![PlantId::parse("fab-a").expect("plant id should parse")])
                .expect("restricted scope should be valid"),
            AuthenticationSource::CorporateSso,
            EmploymentStatus::Terminated,
        )
        .expect("terminated employee actor should be valid")
    }

    fn committee_actor() -> AuthenticatedActorContext {
        AuthenticatedActorContext::new(
            actor_id("committee-ledger-test"),
            Role::CommitteeAdmin,
            PlantScope::all(),
            AuthenticationSource::CorporateSso,
        )
        .expect("committee actor should be valid")
    }

    fn order_id(value: &str) -> OrderId {
        OrderId::parse(value).expect("order id should parse")
    }

    fn audit_timestamp(epoch_day: i32, minute_of_day: u16) -> AuditTimestamp {
        AuditTimestamp::new(epoch_day, minute_of_day).expect("timestamp should be valid")
    }

    fn source_ref(label: &str) -> PayrollLedgerSourceRef {
        PayrollLedgerSourceRef::new(PayrollLedgerSourceKind::OrderMutation, label)
            .expect("source ref should be valid")
    }

    #[test]
    fn reconcile_is_append_only_and_tracks_adjustments() {
        let audit_trail = ImmutableAuditTrail::default();
        let service = PayrollLedgerService::new(PayrollRetentionPolicy::default(), audit_trail);
        let employee = employee_actor();
        let order = order_id("ord-payroll-ledger-001");
        let amount = Money::new("TWD", 12000).expect("money should be valid");

        let first = service
            .reconcile_order_charge(
                &employee,
                "createEmployeeOrder",
                &order,
                employee.actor_id(),
                EmploymentStatus::Active,
                55,
                amount.currency(),
                12000,
                audit_timestamp(50, 600),
                source_ref("order:create"),
            )
            .expect("first deduction should append")
            .expect("delta should produce entry");
        assert_eq!(first.kind(), PayrollLedgerEntryKind::Deduction);
        assert_eq!(first.amount().amount_minor(), 12000);

        let second = service
            .reconcile_order_charge(
                &employee,
                "updateEmployeeOrder",
                &order,
                employee.actor_id(),
                EmploymentStatus::Active,
                55,
                amount.currency(),
                9000,
                audit_timestamp(50, 610),
                source_ref("order:update"),
            )
            .expect("adjustment should append")
            .expect("delta should produce entry");
        assert_eq!(second.kind(), PayrollLedgerEntryKind::AdjustmentCredit);
        assert_eq!(second.amount().amount_minor(), 3000);

        let view = service
            .employee_order_view(&employee, &order)
            .expect("employee should view own order payroll");
        assert_eq!(view.ledger_entries().len(), 2);
        assert_eq!(view.net_amount_minor(), 9000);
        assert_eq!(
            view.ledger_entries()
                .iter()
                .map(|entry| entry.source_event().event_reference().to_owned())
                .collect::<Vec<_>>(),
            vec!["order:create".to_owned(), "order:update".to_owned()]
        );
    }

    #[test]
    fn dispute_workflow_has_owner_timestamps_and_resolution_trace() {
        let audit_trail = ImmutableAuditTrail::default();
        let service = PayrollLedgerService::new(PayrollRetentionPolicy::default(), audit_trail);
        let employee = employee_actor();
        let payroll = payroll_actor();
        let order = order_id("ord-payroll-ledger-002");
        let amount = Money::new("TWD", 10000).expect("money should be valid");

        service
            .reconcile_order_charge(
                &employee,
                "createEmployeeOrder",
                &order,
                employee.actor_id(),
                EmploymentStatus::Active,
                80,
                amount.currency(),
                10000,
                audit_timestamp(70, 500),
                source_ref("order:create"),
            )
            .expect("deduction should append");

        let default_owner = actor_id("payroll-queue-system");
        let dispute = service
            .open_dispute(
                &employee,
                &order,
                &default_owner,
                "charged after sold out",
                audit_timestamp(70, 520),
            )
            .expect("dispute should open");
        assert_eq!(dispute.owner_actor_id(), &default_owner);
        assert_eq!(dispute.status(), PayrollDisputeStatus::Open);

        let assigned_owner = actor_id("payroll-owner-a1");
        let dispute = service
            .assign_dispute_owner(
                &payroll,
                dispute.dispute_id(),
                &assigned_owner,
                audit_timestamp(70, 530),
                Some("triaged".to_owned()),
            )
            .expect("owner assignment should succeed");
        assert_eq!(dispute.owner_actor_id(), &assigned_owner);
        assert_eq!(dispute.status(), PayrollDisputeStatus::InReview);

        let resolved = service
            .resolve_dispute_refund(
                &payroll,
                dispute.dispute_id(),
                audit_timestamp(70, 540),
                "approved refund",
                Some(7000),
            )
            .expect("refund resolution should succeed");
        assert_eq!(
            resolved.status(),
            PayrollDisputeStatus::ResolvedRefundApproved
        );
        assert_eq!(resolved.trace().len(), 3);
        assert_eq!(
            resolved.trace()[2].event_type(),
            PayrollDisputeTraceEventType::ResolvedRefundApproved
        );
        assert!(resolved.trace()[2].refund_ledger_entry_id().is_some());

        let view = service
            .employee_order_view(&employee, &order)
            .expect("employee should view resolved dispute state");
        assert_eq!(view.net_amount_minor(), 3000);
        assert_eq!(view.disputes().len(), 1);
        assert_eq!(
            view.disputes()[0].status(),
            PayrollDisputeStatus::ResolvedRefundApproved
        );
    }

    #[test]
    fn sftp_exchange_is_primary_and_hr_api_sync_is_optional_adjunct() {
        let audit_trail = ImmutableAuditTrail::default();
        let service = PayrollLedgerService::new(PayrollRetentionPolicy::default(), audit_trail);
        let employee = employee_actor();
        let payroll = payroll_actor();
        let order = order_id("ord-payroll-ledger-003");
        let amount = Money::new("TWD", 9500).expect("money should be valid");

        service
            .reconcile_order_charge(
                &employee,
                "createEmployeeOrder",
                &order,
                employee.actor_id(),
                EmploymentStatus::Active,
                92,
                amount.currency(),
                9500,
                audit_timestamp(92, 600),
                source_ref("order:create"),
            )
            .expect("deduction should append");

        let export_page = service
            .export_sftp_batch(
                &payroll,
                "1970-04",
                "cycle-1970-04-primary",
                1,
                50,
                PayrollSortField::DeliveryDate,
                SortOrder::Asc,
                audit_timestamp(92, 620),
            )
            .expect("sftp export should succeed");
        assert_eq!(export_page.items().len(), 1);
        assert_eq!(export_page.batch().pay_period(), "1970-04");
        assert!(export_page.batch().hr_api_sync_receipt().is_none());

        let synced_batch = service
            .sync_hr_api_adjunct(
                &payroll,
                export_page.batch().batch_id(),
                PayrollHrApiSyncOutcome::Succeeded,
                None,
                audit_timestamp(92, 700),
            )
            .expect("optional HR API sync should succeed");
        let receipt = synced_batch
            .hr_api_sync_receipt()
            .expect("sync receipt should exist after adjunct sync");
        assert_eq!(receipt.status(), HrApiSyncStatus::Succeeded);
        assert_eq!(receipt.actor_id(), payroll.actor_id());
    }

    #[test]
    fn payroll_cycle_replay_is_idempotent_and_keeps_snapshot_checksum() {
        let audit_trail = ImmutableAuditTrail::default();
        let service =
            PayrollLedgerService::new(PayrollRetentionPolicy::default(), audit_trail.clone());
        let employee = employee_actor();
        let payroll = payroll_actor();
        let order = order_id("ord-payroll-ledger-005");

        service
            .reconcile_order_charge(
                &employee,
                "createEmployeeOrder",
                &order,
                employee.actor_id(),
                EmploymentStatus::Active,
                95,
                "TWD",
                8800,
                audit_timestamp(95, 500),
                source_ref("order:create"),
            )
            .expect("deduction should append");

        let first_export = service
            .export_sftp_batch(
                &payroll,
                "1970-04",
                "cycle-1970-04-finclose",
                1,
                50,
                PayrollSortField::DeliveryDate,
                SortOrder::Asc,
                audit_timestamp(95, 520),
            )
            .expect("first cycle export should succeed");
        let first_evidence_count = audit_trail
            .evidence_count()
            .expect("audit evidence count should resolve");

        let replay_export = service
            .export_sftp_batch(
                &payroll,
                "1970-04",
                "cycle-1970-04-finclose",
                1,
                50,
                PayrollSortField::DeliveryDate,
                SortOrder::Asc,
                audit_timestamp(95, 560),
            )
            .expect("replayed cycle export should be idempotent");
        let replay_evidence_count = audit_trail
            .evidence_count()
            .expect("audit evidence count should resolve");

        assert_eq!(
            first_export.batch().batch_id(),
            replay_export.batch().batch_id()
        );
        assert_eq!(
            first_export.batch().snapshot_checksum(),
            replay_export.batch().snapshot_checksum()
        );
        assert_eq!(first_export.items(), replay_export.items());
        assert_eq!(first_evidence_count, replay_evidence_count);
    }

    #[test]
    fn authorized_unlock_allows_recompute_for_the_same_cycle() {
        let audit_trail = ImmutableAuditTrail::default();
        let service =
            PayrollLedgerService::new(PayrollRetentionPolicy::default(), audit_trail.clone());
        let employee = employee_actor();
        let payroll = payroll_actor();
        let committee = committee_actor();
        let order = order_id("ord-payroll-ledger-unlock");

        service
            .reconcile_order_charge(
                &employee,
                "createEmployeeOrder",
                &order,
                employee.actor_id(),
                EmploymentStatus::Active,
                95,
                "TWD",
                8800,
                audit_timestamp(95, 500),
                source_ref("order:create"),
            )
            .expect("deduction should append");

        let first_export = service
            .export_sftp_batch(
                &payroll,
                "1970-04",
                "cycle-1970-04-unlock",
                1,
                50,
                PayrollSortField::DeliveryDate,
                SortOrder::Asc,
                audit_timestamp(95, 520),
            )
            .expect("first cycle export should succeed");

        let unauthorized_unlock = service
            .unlock_cycle_for_recompute(
                &payroll,
                "cycle-1970-04-unlock",
                "fix reconciliation drift",
                audit_timestamp(95, 521),
            )
            .expect_err("unlock should require committee role");
        assert!(matches!(
            unauthorized_unlock,
            PayrollLedgerError::UnauthorizedRole { .. }
        ));

        let invalid_unlock_reason = service
            .unlock_cycle_for_recompute(
                &committee,
                "cycle-1970-04-unlock",
                "   ",
                audit_timestamp(95, 522),
            )
            .expect_err("unlock reason should be required");
        assert!(matches!(
            invalid_unlock_reason,
            PayrollLedgerError::InvalidSettlementReason(_)
        ));

        let unlocked = service
            .unlock_cycle_for_recompute(
                &committee,
                "cycle-1970-04-unlock",
                "approved correction after ledger adjustment",
                audit_timestamp(95, 523),
            )
            .expect("committee unlock should succeed");
        assert_eq!(unlocked.lock_state(), PayrollSettlementLockState::Unlocked);
        assert_eq!(unlocked.batch_id(), first_export.batch().batch_id());

        service
            .reconcile_order_charge(
                &employee,
                "updateEmployeeOrder",
                &order,
                employee.actor_id(),
                EmploymentStatus::Active,
                95,
                "TWD",
                9100,
                audit_timestamp(95, 540),
                source_ref("order:update"),
            )
            .expect("adjustment should append");

        let recomputed = service
            .export_sftp_batch(
                &payroll,
                "1970-04",
                "cycle-1970-04-unlock",
                1,
                50,
                PayrollSortField::DeliveryDate,
                SortOrder::Asc,
                audit_timestamp(95, 550),
            )
            .expect("recomputed cycle should succeed after unlock");
        assert_ne!(
            recomputed.batch().batch_id(),
            first_export.batch().batch_id()
        );
        assert_ne!(
            recomputed.batch().snapshot_checksum(),
            first_export.batch().snapshot_checksum()
        );

        let replay_after_recompute = service
            .export_sftp_batch(
                &payroll,
                "1970-04",
                "cycle-1970-04-unlock",
                1,
                50,
                PayrollSortField::DeliveryDate,
                SortOrder::Asc,
                audit_timestamp(95, 560),
            )
            .expect("replay after recompute should stay idempotent");
        assert_eq!(
            replay_after_recompute.batch().batch_id(),
            recomputed.batch().batch_id()
        );
    }

    #[test]
    fn monthly_close_defaults_to_previous_taipei_cycle_and_emits_reconciliation_metadata() {
        let audit_trail = ImmutableAuditTrail::default();
        let service = PayrollLedgerService::new(PayrollRetentionPolicy::default(), audit_trail);
        let employee = employee_actor();
        let payroll = payroll_actor();
        let order = order_id("ord-payroll-ledger-monthly-close");

        service
            .reconcile_order_charge(
                &employee,
                "createEmployeeOrder",
                &order,
                employee.actor_id(),
                EmploymentStatus::Active,
                95,
                "TWD",
                6200,
                audit_timestamp(95, 500),
                source_ref("order:create"),
            )
            .expect("deduction should append");

        let close_page = service
            .close_monthly_settlement(
                &payroll,
                None,
                1,
                20,
                PayrollSortField::DeliveryDate,
                SortOrder::Asc,
                audit_timestamp(120, 10),
            )
            .expect("monthly close should succeed");

        assert_eq!(close_page.batch().pay_period(), "1970-04");
        assert_eq!(close_page.batch().cycle_key(), "monthly-1970-04");
        assert_eq!(close_page.batch().cycle_start_epoch_day(), 90);
        assert_eq!(close_page.batch().cycle_end_epoch_day(), 119);
        assert_eq!(close_page.batch().reconciliation().total_records(), 1);
        assert_eq!(
            close_page.batch().reconciliation().total_amount_minor(),
            6200
        );
        assert_eq!(
            close_page.batch().reconciliation().total_source_entries(),
            1
        );
        assert_eq!(close_page.batch().reconciliation().ready_records(), 1);
        assert_eq!(close_page.batch().reconciliation().locked_records(), 0);
        assert_eq!(close_page.batch().reconciliation().refunded_records(), 0);
        assert_eq!(close_page.batch().reconciliation().disputed_records(), 0);
        assert_eq!(
            close_page
                .batch()
                .reconciliation()
                .required_exception_classes()
                .iter()
                .map(|class| class.as_str())
                .collect::<Vec<_>>(),
            vec![
                "DISPUTED",
                "DEDUCTION_FAILED",
                "EMPLOYEE_TERMINATED",
                "REFUNDED"
            ]
        );
        assert!(close_page
            .batch()
            .reconciliation()
            .present_exception_classes()
            .is_empty());
    }

    #[test]
    fn lock_cycle_requires_existing_cycle_and_non_empty_reason() {
        let audit_trail = ImmutableAuditTrail::default();
        let service = PayrollLedgerService::new(PayrollRetentionPolicy::default(), audit_trail);
        let committee = committee_actor();

        let missing_cycle = service
            .lock_cycle(
                &committee,
                "cycle-missing",
                "close governance hold",
                audit_timestamp(120, 20),
            )
            .expect_err("missing cycle lock should fail");
        assert!(matches!(
            missing_cycle,
            PayrollLedgerError::SettlementCycleNotFound { .. }
        ));

        let invalid_reason = service
            .unlock_cycle_for_recompute(
                &committee,
                "cycle-missing",
                "   ",
                audit_timestamp(120, 21),
            )
            .expect_err("unlock reason should be non-empty");
        assert!(matches!(
            invalid_reason,
            PayrollLedgerError::InvalidSettlementReason(_)
        ));
    }

    #[test]
    fn settlement_cycle_requires_explicit_lock_state() {
        let audit_trail = ImmutableAuditTrail::default();
        let service = PayrollLedgerService::new(PayrollRetentionPolicy::default(), audit_trail);
        let employee = employee_actor();
        let payroll = payroll_actor();
        let order = order_id("ord-payroll-ledger-lock-state-invariant");

        service
            .reconcile_order_charge(
                &employee,
                "createEmployeeOrder",
                &order,
                employee.actor_id(),
                EmploymentStatus::Active,
                95,
                "TWD",
                6200,
                audit_timestamp(95, 500),
                source_ref("order:create"),
            )
            .expect("deduction should append");
        service
            .export_sftp_batch(
                &payroll,
                "1970-04",
                "cycle-1970-04-lock-state-invariant",
                1,
                20,
                PayrollSortField::DeliveryDate,
                SortOrder::Asc,
                audit_timestamp(95, 510),
            )
            .expect("batch export should succeed");

        {
            let mut state = lock_state(&service.state).expect("state lock should resolve");
            state
                .cycle_lock_state_by_cycle
                .remove("cycle-1970-04-lock-state-invariant");
        }

        let error = service
            .export_sftp_batch(
                &payroll,
                "1970-04",
                "cycle-1970-04-lock-state-invariant",
                1,
                20,
                PayrollSortField::DeliveryDate,
                SortOrder::Asc,
                audit_timestamp(95, 520),
            )
            .expect_err("missing lock state should fail");
        assert!(matches!(
            error,
            PayrollLedgerError::SettlementCycleLockStateMissing { .. }
        ));
    }

    #[test]
    fn deduction_status_covers_terminated_employee_refund_and_deduction_failure_exceptions() {
        let audit_trail = ImmutableAuditTrail::default();
        let service = PayrollLedgerService::new(PayrollRetentionPolicy::default(), audit_trail);
        let active_employee = employee_actor();
        let terminated_employee = terminated_employee_actor();
        let payroll = payroll_actor();
        let terminated_order = order_id("ord-payroll-ledger-terminated");
        let active_order = order_id("ord-payroll-ledger-failed");
        let refunded_order = order_id("ord-payroll-ledger-refunded");

        service
            .reconcile_order_charge(
                &terminated_employee,
                "createEmployeeOrder",
                &terminated_order,
                terminated_employee.actor_id(),
                EmploymentStatus::Terminated,
                101,
                "TWD",
                7300,
                audit_timestamp(101, 450),
                source_ref("order:create:terminated"),
            )
            .expect("terminated employee deduction should append");
        service
            .reconcile_order_charge(
                &active_employee,
                "createEmployeeOrder",
                &active_order,
                active_employee.actor_id(),
                EmploymentStatus::Active,
                101,
                "TWD",
                5400,
                audit_timestamp(101, 455),
                source_ref("order:create:active"),
            )
            .expect("active employee deduction should append");
        service
            .reconcile_order_charge(
                &active_employee,
                "createEmployeeOrder",
                &refunded_order,
                active_employee.actor_id(),
                EmploymentStatus::Active,
                101,
                "TWD",
                6100,
                audit_timestamp(101, 457),
                source_ref("order:create:refunded"),
            )
            .expect("refunded order initial deduction should append");
        service
            .reconcile_order_charge(
                &active_employee,
                "cancelEmployeeOrder",
                &refunded_order,
                active_employee.actor_id(),
                EmploymentStatus::Active,
                101,
                "TWD",
                0,
                audit_timestamp(101, 458),
                source_ref("order:cancel:refunded"),
            )
            .expect("refunded order adjustment should append");

        let exported = service
            .export_sftp_batch(
                &payroll,
                "1970-04",
                "cycle-1970-04-exception",
                1,
                50,
                PayrollSortField::DeliveryDate,
                SortOrder::Asc,
                audit_timestamp(101, 470),
            )
            .expect("cycle export should succeed");
        service
            .sync_hr_api_adjunct(
                &payroll,
                exported.batch().batch_id(),
                PayrollHrApiSyncOutcome::Failed,
                Some("adjunct endpoint timeout".to_owned()),
                audit_timestamp(101, 520),
            )
            .expect("failed HR sync should be recorded");

        let replay = service
            .export_sftp_batch(
                &payroll,
                "1970-04",
                "cycle-1970-04-post-failure",
                1,
                50,
                PayrollSortField::DeliveryDate,
                SortOrder::Asc,
                audit_timestamp(101, 550),
            )
            .expect("new cycle should include exception statuses");
        let by_order = replay
            .items()
            .iter()
            .map(|record| (record.order_id().as_str().to_owned(), record.status()))
            .collect::<BTreeMap<_, _>>();

        assert_eq!(
            by_order.get(terminated_order.as_str()),
            Some(&PayrollDeductionStatus::EmployeeTerminated)
        );
        assert_eq!(
            by_order.get(active_order.as_str()),
            Some(&PayrollDeductionStatus::DeductionFailed)
        );
        assert_eq!(
            by_order.get(refunded_order.as_str()),
            Some(&PayrollDeductionStatus::Refunded)
        );
    }

    #[test]
    fn export_second_cycle_marks_open_deductions_as_locked() {
        let audit_trail = ImmutableAuditTrail::default();
        let service = PayrollLedgerService::new(PayrollRetentionPolicy::default(), audit_trail);
        let employee = employee_actor();
        let payroll = payroll_actor();
        let order = order_id("ord-payroll-ledger-locked");

        service
            .reconcile_order_charge(
                &employee,
                "createEmployeeOrder",
                &order,
                employee.actor_id(),
                EmploymentStatus::Active,
                112,
                "TWD",
                4800,
                audit_timestamp(112, 500),
                source_ref("order:create:lock"),
            )
            .expect("deduction should append");

        let first_cycle = service
            .export_sftp_batch(
                &payroll,
                "1970-04",
                "cycle-1970-04-lock-1",
                1,
                50,
                PayrollSortField::DeliveryDate,
                SortOrder::Asc,
                audit_timestamp(112, 520),
            )
            .expect("first cycle export should succeed");
        assert_eq!(
            first_cycle.items()[0].status(),
            PayrollDeductionStatus::Ready
        );

        let second_cycle = service
            .export_sftp_batch(
                &payroll,
                "1970-04",
                "cycle-1970-04-lock-2",
                1,
                50,
                PayrollSortField::DeliveryDate,
                SortOrder::Asc,
                audit_timestamp(112, 550),
            )
            .expect("second cycle export should succeed");
        assert_eq!(
            second_cycle.items()[0].status(),
            PayrollDeductionStatus::Locked
        );
    }

    #[test]
    fn retention_purge_removes_expired_payroll_data() {
        let audit_trail = ImmutableAuditTrail::default();
        let retention = PayrollRetentionPolicy::new(1, 1, 1).expect("policy should be valid");
        let service = PayrollLedgerService::new(retention, audit_trail);
        let employee = employee_actor();
        let payroll = payroll_actor();
        let committee = committee_actor();
        let order = order_id("ord-payroll-ledger-004");
        let amount = Money::new("TWD", 10000).expect("money should be valid");

        service
            .reconcile_order_charge(
                &employee,
                "createEmployeeOrder",
                &order,
                employee.actor_id(),
                EmploymentStatus::Active,
                120,
                amount.currency(),
                10000,
                audit_timestamp(10, 600),
                source_ref("order:create"),
            )
            .expect("deduction should append");

        let dispute = service
            .open_dispute(
                &employee,
                &order,
                &actor_id("payroll-queue-system"),
                "legacy dispute",
                audit_timestamp(10, 700),
            )
            .expect("dispute should open");

        service
            .resolve_dispute_rejected(
                &payroll,
                dispute.dispute_id(),
                audit_timestamp(10, 710),
                "rejected",
            )
            .expect("dispute rejection should succeed");

        service
            .export_sftp_batch(
                &payroll,
                "1970-01",
                "cycle-1970-01-retention",
                1,
                10,
                PayrollSortField::DeliveryDate,
                SortOrder::Asc,
                audit_timestamp(10, 720),
            )
            .expect("batch export should succeed");

        let report = service
            .purge_expired_data(&committee, audit_timestamp(20, 0))
            .expect("retention purge should succeed");
        assert!(report.purged_ledger_entries > 0);
        assert!(report.purged_disputes > 0);
        assert!(report.purged_exchange_batches > 0);

        let view_error = service
            .employee_order_view(&employee, &order)
            .expect_err("expired order payroll data should be removed");
        assert!(matches!(
            view_error,
            PayrollLedgerError::OrderNotRegistered(_)
        ));
    }
}
