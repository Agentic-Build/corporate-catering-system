use std::collections::{BTreeMap, BTreeSet};
use std::fmt;
use std::sync::{Arc, Mutex};
use std::time::{SystemTime, UNIX_EPOCH};

use crate::identity::{ActorId, AuthenticatedActorContext, AuthenticationSource, PlantScope, Role};

const MINUTES_PER_DAY: u16 = 24 * 60;
const SECONDS_PER_DAY: i64 = 86_400;
const TAIPEI_FIXED_OFFSET_SECONDS: i64 = 8 * 60 * 60;

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AuditIdentityLink {
    actor_id: ActorId,
    role: Role,
    authentication_source: AuthenticationSource,
    plant_scope: PlantScope,
    operation_id: String,
}

impl AuditIdentityLink {
    pub fn from_actor(actor: &AuthenticatedActorContext, operation_id: impl Into<String>) -> Self {
        Self {
            actor_id: actor.actor_id().clone(),
            role: actor.role(),
            authentication_source: actor.authentication_source(),
            plant_scope: actor.plant_scope().clone(),
            operation_id: operation_id.into(),
        }
    }

    pub fn actor_id(&self) -> &ActorId {
        &self.actor_id
    }

    pub fn role(&self) -> Role {
        self.role
    }

    pub fn authentication_source(&self) -> AuthenticationSource {
        self.authentication_source
    }

    pub fn plant_scope(&self) -> &PlantScope {
        &self.plant_scope
    }

    pub fn operation_id(&self) -> &str {
        &self.operation_id
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Hash)]
pub struct AuditTimestamp {
    epoch_day: i32,
    minute_of_day: u16,
}

impl AuditTimestamp {
    pub fn new(epoch_day: i32, minute_of_day: u16) -> Result<Self, AuditTrailError> {
        if minute_of_day >= MINUTES_PER_DAY {
            return Err(AuditTrailError::InvalidMinuteOfDay { minute_of_day });
        }
        Ok(Self {
            epoch_day,
            minute_of_day,
        })
    }

    pub const fn from_epoch_day(epoch_day: i32) -> Self {
        Self {
            epoch_day,
            minute_of_day: 0,
        }
    }

    pub fn from_taipei_business_moment(
        epoch_day: i32,
        minute_of_day: u16,
    ) -> Result<Self, AuditTrailError> {
        Self::new(epoch_day, minute_of_day)
    }

    pub fn now_taipei() -> Result<Self, AuditTrailError> {
        let unix_seconds = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .map_err(|error| AuditTrailError::SystemClockUnavailable(error.to_string()))?
            .as_secs();
        let unix_seconds_i64 = i64::try_from(unix_seconds)
            .map_err(|_| AuditTrailError::SystemClockUnavailable("clock overflow".to_owned()))?;
        let shifted_seconds = unix_seconds_i64
            .checked_add(TAIPEI_FIXED_OFFSET_SECONDS)
            .ok_or_else(|| AuditTrailError::SystemClockUnavailable("clock overflow".to_owned()))?;
        let epoch_day = i32::try_from(shifted_seconds.div_euclid(SECONDS_PER_DAY))
            .map_err(|_| AuditTrailError::SystemClockUnavailable("clock overflow".to_owned()))?;
        let minute_of_day = u16::try_from(shifted_seconds.rem_euclid(SECONDS_PER_DAY) / 60)
            .map_err(|_| AuditTrailError::SystemClockUnavailable("clock overflow".to_owned()))?;
        Self::new(epoch_day, minute_of_day)
    }

    pub fn epoch_day(self) -> i32 {
        self.epoch_day
    }

    pub fn minute_of_day(self) -> u16 {
        self.minute_of_day
    }

    fn days_since(self, earlier: Self) -> i32 {
        self.epoch_day - earlier.epoch_day
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Hash)]
pub enum AuditAction {
    CreateEmployeeOrder,
    UpdateEmployeeOrder,
    VerifyPickupOrder,
    MarkOrderSoldOut,
    MarkOrderRefundPending,
    MarkOrderRefunded,
    UpsertVendorMenuItem,
    UpsertVendorOrderingPolicy,
    AdvanceVendorFulfillmentDeliveryStatus,
    CreateVendorFulfillmentExportBatch,
    UpsertVendorPlantDeliveryMapping,
    DeleteVendorPlantDeliveryMapping,
    UpsertComplianceDocumentTemplate,
    RegisterVendorApplication,
    SubmitVendorComplianceDocument,
    ReviewVendorApplication,
    RunVendorComplianceLifecycle,
    PruneVendorComplianceHistory,
    ExportPayrollDeductions,
}

impl AuditAction {
    pub const fn as_str(self) -> &'static str {
        match self {
            Self::CreateEmployeeOrder => "CREATE_EMPLOYEE_ORDER",
            Self::UpdateEmployeeOrder => "UPDATE_EMPLOYEE_ORDER",
            Self::VerifyPickupOrder => "VERIFY_PICKUP_ORDER",
            Self::MarkOrderSoldOut => "MARK_ORDER_SOLD_OUT",
            Self::MarkOrderRefundPending => "MARK_ORDER_REFUND_PENDING",
            Self::MarkOrderRefunded => "MARK_ORDER_REFUNDED",
            Self::UpsertVendorMenuItem => "UPSERT_VENDOR_MENU_ITEM",
            Self::UpsertVendorOrderingPolicy => "UPSERT_VENDOR_ORDERING_POLICY",
            Self::AdvanceVendorFulfillmentDeliveryStatus => {
                "ADVANCE_VENDOR_FULFILLMENT_DELIVERY_STATUS"
            }
            Self::CreateVendorFulfillmentExportBatch => "CREATE_VENDOR_FULFILLMENT_EXPORT_BATCH",
            Self::UpsertVendorPlantDeliveryMapping => "UPSERT_VENDOR_PLANT_DELIVERY_MAPPING",
            Self::DeleteVendorPlantDeliveryMapping => "DELETE_VENDOR_PLANT_DELIVERY_MAPPING",
            Self::UpsertComplianceDocumentTemplate => "UPSERT_COMPLIANCE_DOCUMENT_TEMPLATE",
            Self::RegisterVendorApplication => "REGISTER_VENDOR_APPLICATION",
            Self::SubmitVendorComplianceDocument => "SUBMIT_VENDOR_COMPLIANCE_DOCUMENT",
            Self::ReviewVendorApplication => "REVIEW_VENDOR_APPLICATION",
            Self::RunVendorComplianceLifecycle => "RUN_VENDOR_COMPLIANCE_LIFECYCLE",
            Self::PruneVendorComplianceHistory => "PRUNE_VENDOR_COMPLIANCE_HISTORY",
            Self::ExportPayrollDeductions => "EXPORT_PAYROLL_DEDUCTIONS",
        }
    }
}

impl fmt::Display for AuditAction {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(self.as_str())
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Hash)]
pub enum AuditEntityType {
    Order,
    MenuItem,
    Vendor,
    DeliveryMapping,
    ComplianceDocumentTemplate,
    FulfillmentBatch,
    Settlement,
    VendorOrderingPolicy,
}

impl AuditEntityType {
    pub const fn as_str(self) -> &'static str {
        match self {
            Self::Order => "ORDER",
            Self::MenuItem => "MENU_ITEM",
            Self::Vendor => "VENDOR",
            Self::DeliveryMapping => "DELIVERY_MAPPING",
            Self::ComplianceDocumentTemplate => "COMPLIANCE_DOCUMENT_TEMPLATE",
            Self::FulfillmentBatch => "FULFILLMENT_BATCH",
            Self::Settlement => "SETTLEMENT",
            Self::VendorOrderingPolicy => "VENDOR_ORDERING_POLICY",
        }
    }
}

impl fmt::Display for AuditEntityType {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(self.as_str())
    }
}

#[derive(Debug, Clone, PartialEq, Eq, PartialOrd, Ord, Hash)]
pub struct AuditEntityRef {
    entity_type: AuditEntityType,
    entity_id: String,
}

impl AuditEntityRef {
    pub fn new(
        entity_type: AuditEntityType,
        entity_id: impl Into<String>,
    ) -> Result<Self, AuditTrailError> {
        let entity_id = entity_id.into();
        if entity_id.trim().is_empty() {
            return Err(AuditTrailError::InvalidEntityId);
        }
        Ok(Self {
            entity_type,
            entity_id,
        })
    }

    pub fn entity_type(&self) -> AuditEntityType {
        self.entity_type
    }

    pub fn entity_id(&self) -> &str {
        &self.entity_id
    }
}

#[derive(Debug, Clone, PartialEq, Eq, PartialOrd, Ord, Hash)]
pub struct AuditCorrelationId(String);

impl AuditCorrelationId {
    pub fn parse(value: impl Into<String>) -> Result<Self, AuditTrailError> {
        let value = value.into();
        if value.trim().is_empty() {
            return Err(AuditTrailError::InvalidCorrelationId);
        }
        Ok(Self(value))
    }

    pub fn for_vendor(vendor_id: impl AsRef<str>) -> Result<Self, AuditTrailError> {
        Self::parse(format!("vendor:{}", vendor_id.as_ref().trim()))
    }

    pub fn for_order(order_id: impl AsRef<str>) -> Result<Self, AuditTrailError> {
        Self::parse(format!("order:{}", order_id.as_ref().trim()))
    }

    pub fn as_str(&self) -> &str {
        &self.0
    }
}

impl fmt::Display for AuditCorrelationId {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(self.as_str())
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ImmutableAuditEvidence {
    evidence_id: u64,
    occurred_at: AuditTimestamp,
    audit_identity: AuditIdentityLink,
    action: AuditAction,
    entity: AuditEntityRef,
    correlation_id: AuditCorrelationId,
}

impl ImmutableAuditEvidence {
    pub fn evidence_id(&self) -> u64 {
        self.evidence_id
    }

    pub fn occurred_at(&self) -> AuditTimestamp {
        self.occurred_at
    }

    pub fn audit_identity(&self) -> &AuditIdentityLink {
        &self.audit_identity
    }

    pub fn action(&self) -> AuditAction {
        self.action
    }

    pub fn entity(&self) -> &AuditEntityRef {
        &self.entity
    }

    pub fn correlation_id(&self) -> &AuditCorrelationId {
        &self.correlation_id
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AuditEvidenceWrite {
    occurred_at: AuditTimestamp,
    audit_identity: AuditIdentityLink,
    action: AuditAction,
    entity: AuditEntityRef,
    correlation_id: AuditCorrelationId,
}

impl AuditEvidenceWrite {
    pub fn new(
        occurred_at: AuditTimestamp,
        audit_identity: AuditIdentityLink,
        action: AuditAction,
        entity: AuditEntityRef,
        correlation_id: AuditCorrelationId,
    ) -> Self {
        Self {
            occurred_at,
            audit_identity,
            action,
            entity,
            correlation_id,
        }
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct AuditRetentionPolicy {
    retention_days: u16,
}

impl AuditRetentionPolicy {
    pub fn new(retention_days: u16) -> Result<Self, AuditTrailError> {
        if retention_days == 0 {
            return Err(AuditTrailError::InvalidRetentionPolicy);
        }
        Ok(Self { retention_days })
    }

    pub fn retention_days(self) -> u16 {
        self.retention_days
    }
}

impl Default for AuditRetentionPolicy {
    fn default() -> Self {
        Self {
            retention_days: 2555,
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq, Default)]
pub struct AuditInvestigationFilter {
    actor_id: Option<ActorId>,
    action: Option<AuditAction>,
    entity_type: Option<AuditEntityType>,
    entity_id: Option<String>,
    occurred_from: Option<AuditTimestamp>,
    occurred_to: Option<AuditTimestamp>,
    correlation_id: Option<AuditCorrelationId>,
}

impl AuditInvestigationFilter {
    pub fn with_actor_id(mut self, actor_id: ActorId) -> Self {
        self.actor_id = Some(actor_id);
        self
    }

    pub fn with_action(mut self, action: AuditAction) -> Self {
        self.action = Some(action);
        self
    }

    pub fn with_entity(
        mut self,
        entity_type: AuditEntityType,
        entity_id: impl Into<String>,
    ) -> Result<Self, AuditTrailError> {
        let entity_id = entity_id.into();
        if entity_id.trim().is_empty() {
            return Err(AuditTrailError::InvalidEntityId);
        }
        self.entity_type = Some(entity_type);
        self.entity_id = Some(entity_id);
        Ok(self)
    }

    pub fn with_time_range(
        mut self,
        occurred_from: Option<AuditTimestamp>,
        occurred_to: Option<AuditTimestamp>,
    ) -> Self {
        self.occurred_from = occurred_from;
        self.occurred_to = occurred_to;
        self
    }

    pub fn with_correlation_id(mut self, correlation_id: AuditCorrelationId) -> Self {
        self.correlation_id = Some(correlation_id);
        self
    }

    fn matches(&self, evidence: &ImmutableAuditEvidence) -> bool {
        if let Some(actor_id) = &self.actor_id {
            if evidence.audit_identity().actor_id() != actor_id {
                return false;
            }
        }
        if let Some(action) = self.action {
            if evidence.action() != action {
                return false;
            }
        }
        if let Some(entity_type) = self.entity_type {
            if evidence.entity().entity_type() != entity_type {
                return false;
            }
        }
        if let Some(entity_id) = &self.entity_id {
            if evidence.entity().entity_id() != entity_id {
                return false;
            }
        }
        if let Some(occurred_from) = self.occurred_from {
            if evidence.occurred_at() < occurred_from {
                return false;
            }
        }
        if let Some(occurred_to) = self.occurred_to {
            if evidence.occurred_at() > occurred_to {
                return false;
            }
        }
        if let Some(correlation_id) = &self.correlation_id {
            if evidence.correlation_id() != correlation_id {
                return false;
            }
        }
        true
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ResponsibilityAttribution {
    actor_id: ActorId,
    role: Role,
    authentication_source: AuthenticationSource,
    event_count: usize,
    actions: Vec<AuditAction>,
    entities: Vec<AuditEntityRef>,
}

impl ResponsibilityAttribution {
    pub fn actor_id(&self) -> &ActorId {
        &self.actor_id
    }

    pub fn role(&self) -> Role {
        self.role
    }

    pub fn authentication_source(&self) -> AuthenticationSource {
        self.authentication_source
    }

    pub fn event_count(&self) -> usize {
        self.event_count
    }

    pub fn actions(&self) -> &[AuditAction] {
        &self.actions
    }

    pub fn entities(&self) -> &[AuditEntityRef] {
        &self.entities
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
pub struct AuditPurgeReport {
    pub purged_events: usize,
}

#[derive(Debug, Clone)]
pub struct ImmutableAuditTrail {
    state: Arc<Mutex<AuditTrailState>>,
}

impl Default for ImmutableAuditTrail {
    fn default() -> Self {
        Self::new(AuditRetentionPolicy::default())
    }
}

impl ImmutableAuditTrail {
    pub fn new(retention_policy: AuditRetentionPolicy) -> Self {
        Self {
            state: Arc::new(Mutex::new(AuditTrailState {
                retention_policy,
                next_evidence_id: 0,
                evidences: Vec::new(),
            })),
        }
    }

    pub fn retention_policy(&self) -> Result<AuditRetentionPolicy, AuditTrailError> {
        let state = lock_state(&self.state)?;
        Ok(state.retention_policy)
    }

    pub fn append(
        &self,
        write: AuditEvidenceWrite,
    ) -> Result<ImmutableAuditEvidence, AuditTrailError> {
        let mut state = lock_state(&self.state)?;
        state.next_evidence_id = state
            .next_evidence_id
            .checked_add(1)
            .ok_or(AuditTrailError::EvidenceSequenceOverflow)?;
        let evidence = ImmutableAuditEvidence {
            evidence_id: state.next_evidence_id,
            occurred_at: write.occurred_at,
            audit_identity: write.audit_identity,
            action: write.action,
            entity: write.entity,
            correlation_id: write.correlation_id,
        };
        state.evidences.push(evidence.clone());
        Ok(evidence)
    }

    pub fn investigation_query(
        &self,
        investigator: &AuthenticatedActorContext,
        filter: &AuditInvestigationFilter,
    ) -> Result<Vec<ImmutableAuditEvidence>, AuditTrailError> {
        ensure_committee_admin(investigator)?;
        let state = lock_state(&self.state)?;
        let mut matched = state
            .evidences
            .iter()
            .filter(|evidence| filter.matches(evidence))
            .cloned()
            .collect::<Vec<_>>();
        matched.sort_by(|left, right| {
            left.occurred_at()
                .cmp(&right.occurred_at())
                .then_with(|| left.evidence_id().cmp(&right.evidence_id()))
        });
        Ok(matched)
    }

    pub fn investigation_responsibility_query(
        &self,
        investigator: &AuthenticatedActorContext,
        filter: &AuditInvestigationFilter,
    ) -> Result<Vec<ResponsibilityAttribution>, AuditTrailError> {
        ensure_committee_admin(investigator)?;
        let events = self.investigation_query(investigator, filter)?;
        let mut grouped = BTreeMap::<String, ResponsibilityAccumulator>::new();

        for event in events {
            let actor_key = event.audit_identity().actor_id().as_str().to_owned();
            let entry = grouped
                .entry(actor_key)
                .or_insert_with(|| ResponsibilityAccumulator {
                    actor_id: event.audit_identity().actor_id().clone(),
                    role: event.audit_identity().role(),
                    authentication_source: event.audit_identity().authentication_source(),
                    event_count: 0,
                    actions: BTreeSet::new(),
                    entities: BTreeSet::new(),
                });
            entry.event_count += 1;
            entry.actions.insert(event.action());
            entry.entities.insert(event.entity().clone());
        }

        let mut attributions = grouped
            .into_values()
            .map(|accumulator| ResponsibilityAttribution {
                actor_id: accumulator.actor_id,
                role: accumulator.role,
                authentication_source: accumulator.authentication_source,
                event_count: accumulator.event_count,
                actions: accumulator.actions.into_iter().collect(),
                entities: accumulator.entities.into_iter().collect(),
            })
            .collect::<Vec<_>>();
        attributions.sort_by(|left, right| {
            right
                .event_count()
                .cmp(&left.event_count())
                .then_with(|| left.actor_id().as_str().cmp(right.actor_id().as_str()))
        });
        Ok(attributions)
    }

    pub fn purge_expired_evidence(
        &self,
        actor: &AuthenticatedActorContext,
        as_of: AuditTimestamp,
    ) -> Result<AuditPurgeReport, AuditTrailError> {
        ensure_committee_admin(actor)?;
        let mut state = lock_state(&self.state)?;
        let before = state.evidences.len();
        let retention_days = i32::from(state.retention_policy.retention_days());
        state
            .evidences
            .retain(|evidence| as_of.days_since(evidence.occurred_at()) <= retention_days);
        Ok(AuditPurgeReport {
            purged_events: before.saturating_sub(state.evidences.len()),
        })
    }

    pub fn evidence_count(&self) -> Result<usize, AuditTrailError> {
        let state = lock_state(&self.state)?;
        Ok(state.evidences.len())
    }
}

#[derive(Debug, Clone)]
struct AuditTrailState {
    retention_policy: AuditRetentionPolicy,
    next_evidence_id: u64,
    evidences: Vec<ImmutableAuditEvidence>,
}

#[derive(Debug, Clone)]
struct ResponsibilityAccumulator {
    actor_id: ActorId,
    role: Role,
    authentication_source: AuthenticationSource,
    event_count: usize,
    actions: BTreeSet<AuditAction>,
    entities: BTreeSet<AuditEntityRef>,
}

fn ensure_committee_admin(actor: &AuthenticatedActorContext) -> Result<(), AuditTrailError> {
    if actor.role() != Role::CommitteeAdmin {
        return Err(AuditTrailError::UnauthorizedInvestigatorRole {
            actual: actor.role(),
        });
    }
    Ok(())
}

fn lock_state(
    state: &Arc<Mutex<AuditTrailState>>,
) -> Result<std::sync::MutexGuard<'_, AuditTrailState>, AuditTrailError> {
    state.lock().map_err(|_| AuditTrailError::StatePoisoned)
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum AuditTrailError {
    InvalidMinuteOfDay { minute_of_day: u16 },
    InvalidEntityId,
    InvalidCorrelationId,
    InvalidRetentionPolicy,
    UnauthorizedInvestigatorRole { actual: Role },
    EvidenceSequenceOverflow,
    SystemClockUnavailable(String),
    StatePoisoned,
}

impl fmt::Display for AuditTrailError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::InvalidMinuteOfDay { minute_of_day } => write!(
                f,
                "audit minute_of_day must be between 0 and 1439, got {minute_of_day}"
            ),
            Self::InvalidEntityId => f.write_str("audit entity id must not be empty"),
            Self::InvalidCorrelationId => f.write_str("audit correlation id must not be empty"),
            Self::InvalidRetentionPolicy => {
                f.write_str("audit retention policy requires retention_days > 0")
            }
            Self::UnauthorizedInvestigatorRole { actual } => write!(
                f,
                "committee-admin role is required for investigation queries, got {actual:?}"
            ),
            Self::EvidenceSequenceOverflow => f.write_str("audit evidence sequence overflowed"),
            Self::SystemClockUnavailable(message) => {
                write!(f, "audit clock resolution failed: {message}")
            }
            Self::StatePoisoned => {
                f.write_str("audit trail state is poisoned due to a previous panic")
            }
        }
    }
}

impl std::error::Error for AuditTrailError {}
