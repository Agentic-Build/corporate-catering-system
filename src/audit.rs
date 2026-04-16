use std::collections::{BTreeMap, BTreeSet};
use std::fmt;
use std::fs;
use std::path::{Path, PathBuf};
use std::sync::{Arc, Mutex};
use std::time::{SystemTime, UNIX_EPOCH};

use serde::{Deserialize, Serialize};

use crate::identity::{ActorId, AuthenticatedActorContext, AuthenticationSource, PlantScope, Role};

const MINUTES_PER_DAY: u16 = 24 * 60;
const SECONDS_PER_DAY: i64 = 86_400;
const TAIPEI_FIXED_OFFSET_SECONDS: i64 = 8 * 60 * 60;
const PURGE_AUDIT_EVIDENCE_OPERATION_ID: &str = "purgeAuditEvidence";
const AUDIT_TRAIL_ENTITY_ID: &str = "audit-trail";
const PURGE_AUDIT_EVIDENCE_CORRELATION_PREFIX: &str = "audit-trail-retention-purge";

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

    pub const fn through_epoch_day(epoch_day: i32) -> Self {
        Self {
            epoch_day,
            minute_of_day: MINUTES_PER_DAY - 1,
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
    PurgeAuditEvidence,
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
            Self::PurgeAuditEvidence => "PURGE_AUDIT_EVIDENCE",
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
    AuditTrail,
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
            Self::AuditTrail => "AUDIT_TRAIL",
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
                storage_backend: StorageBackend::InMemory,
            })),
        }
    }

    pub fn with_json_storage(
        path: impl Into<PathBuf>,
        retention_policy: AuditRetentionPolicy,
    ) -> Result<Self, AuditTrailError> {
        let path = path.into();
        let storage_backend = StorageBackend::JsonFile(path.clone());
        let state = if let Some(snapshot) = load_snapshot_from_json_file(&path)? {
            AuditTrailState::from_persisted_snapshot(snapshot, retention_policy, storage_backend)?
        } else {
            AuditTrailState {
                retention_policy,
                next_evidence_id: 0,
                evidences: Vec::new(),
                storage_backend,
            }
        };
        Ok(Self {
            state: Arc::new(Mutex::new(state)),
        })
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
        let previous_next_evidence_id = state.next_evidence_id;
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
        if let Err(error) = persist_state_if_needed(&state) {
            state.evidences.pop();
            state.next_evidence_id = previous_next_evidence_id;
            return Err(error);
        }
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
        let previous_evidences = state.evidences.clone();
        let previous_next_evidence_id = state.next_evidence_id;
        let retention_days = i32::from(state.retention_policy.retention_days());
        state
            .evidences
            .retain(|evidence| as_of.days_since(evidence.occurred_at()) <= retention_days);
        let purged_events = before.saturating_sub(state.evidences.len());
        let purge_entity =
            match AuditEntityRef::new(AuditEntityType::AuditTrail, AUDIT_TRAIL_ENTITY_ID) {
                Ok(entity) => entity,
                Err(error) => {
                    state.evidences = previous_evidences;
                    return Err(error);
                }
            };
        let purge_correlation_id = match AuditCorrelationId::parse(format!(
            "{PURGE_AUDIT_EVIDENCE_CORRELATION_PREFIX}:{}",
            as_of.epoch_day()
        )) {
            Ok(correlation_id) => correlation_id,
            Err(error) => {
                state.evidences = previous_evidences;
                return Err(error);
            }
        };
        state.next_evidence_id = match state.next_evidence_id.checked_add(1) {
            Some(next_evidence_id) => next_evidence_id,
            None => {
                state.evidences = previous_evidences;
                return Err(AuditTrailError::EvidenceSequenceOverflow);
            }
        };
        let purge_evidence = ImmutableAuditEvidence {
            evidence_id: state.next_evidence_id,
            occurred_at: as_of,
            audit_identity: AuditIdentityLink::from_actor(actor, PURGE_AUDIT_EVIDENCE_OPERATION_ID),
            action: AuditAction::PurgeAuditEvidence,
            entity: purge_entity,
            correlation_id: purge_correlation_id,
        };
        state.evidences.push(purge_evidence);
        if let Err(error) = persist_state_if_needed(&state) {
            state.evidences = previous_evidences;
            state.next_evidence_id = previous_next_evidence_id;
            return Err(error);
        }
        Ok(AuditPurgeReport { purged_events })
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
    storage_backend: StorageBackend,
}

impl AuditTrailState {
    fn to_persisted_snapshot(&self) -> PersistedAuditTrailSnapshot {
        PersistedAuditTrailSnapshot {
            next_evidence_id: self.next_evidence_id,
            evidences: self
                .evidences
                .iter()
                .map(persisted_evidence_from_domain)
                .collect(),
        }
    }

    fn from_persisted_snapshot(
        snapshot: PersistedAuditTrailSnapshot,
        retention_policy: AuditRetentionPolicy,
        storage_backend: StorageBackend,
    ) -> Result<Self, AuditTrailError> {
        let evidences = snapshot
            .evidences
            .iter()
            .map(domain_evidence_from_persisted)
            .collect::<Result<Vec<_>, _>>()?;
        let mut seen_evidence_ids = BTreeSet::new();
        for evidence in &evidences {
            if !seen_evidence_ids.insert(evidence.evidence_id()) {
                return Err(AuditTrailError::PersistenceDataCorrupted(
                    "persisted evidence_id must be unique".to_owned(),
                ));
            }
        }
        let max_evidence_id = evidences
            .iter()
            .map(ImmutableAuditEvidence::evidence_id)
            .max();
        if let Some(max_evidence_id) = max_evidence_id {
            if max_evidence_id == 0 {
                return Err(AuditTrailError::PersistenceDataCorrupted(
                    "persisted evidence_id must be greater than zero".to_owned(),
                ));
            }
            if snapshot.next_evidence_id < max_evidence_id {
                return Err(AuditTrailError::PersistenceDataCorrupted(
                    "persisted next_evidence_id is smaller than max evidence id".to_owned(),
                ));
            }
        }
        Ok(Self {
            retention_policy,
            next_evidence_id: snapshot.next_evidence_id,
            evidences,
            storage_backend,
        })
    }
}

#[derive(Debug, Clone)]
enum StorageBackend {
    InMemory,
    JsonFile(PathBuf),
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

#[derive(Debug, Clone, Serialize, Deserialize)]
struct PersistedAuditTrailSnapshot {
    next_evidence_id: u64,
    evidences: Vec<PersistedImmutableAuditEvidence>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
struct PersistedImmutableAuditEvidence {
    evidence_id: u64,
    occurred_at: PersistedAuditTimestamp,
    audit_identity: PersistedAuditIdentity,
    action: PersistedAuditAction,
    entity: PersistedAuditEntity,
    correlation_id: String,
}

#[derive(Debug, Clone, Copy, Serialize, Deserialize)]
struct PersistedAuditTimestamp {
    epoch_day: i32,
    minute_of_day: u16,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
struct PersistedAuditIdentity {
    actor_id: String,
    role: PersistedRole,
    authentication_source: PersistedAuthenticationSource,
    plant_scope: PersistedPlantScope,
    operation_id: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
struct PersistedPlantScope {
    all_plants: bool,
    plant_ids: Vec<String>,
}

#[derive(Debug, Clone, Copy, Serialize, Deserialize)]
#[serde(rename_all = "SCREAMING_SNAKE_CASE")]
enum PersistedRole {
    Employee,
    VendorOperator,
    CommitteeAdmin,
    PayrollOperator,
}

#[derive(Debug, Clone, Copy, Serialize, Deserialize)]
#[serde(rename_all = "SCREAMING_SNAKE_CASE")]
enum PersistedAuthenticationSource {
    CorporateSso,
    VendorAccountMfa,
}

#[derive(Debug, Clone, Copy, Serialize, Deserialize)]
#[serde(rename_all = "SCREAMING_SNAKE_CASE")]
enum PersistedAuditAction {
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
    PurgeAuditEvidence,
    PruneVendorComplianceHistory,
    ExportPayrollDeductions,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
struct PersistedAuditEntity {
    entity_type: PersistedAuditEntityType,
    entity_id: String,
}

#[derive(Debug, Clone, Copy, Serialize, Deserialize)]
#[serde(rename_all = "SCREAMING_SNAKE_CASE")]
enum PersistedAuditEntityType {
    Order,
    MenuItem,
    Vendor,
    DeliveryMapping,
    ComplianceDocumentTemplate,
    FulfillmentBatch,
    Settlement,
    VendorOrderingPolicy,
    AuditTrail,
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

fn load_snapshot_from_json_file(
    path: &Path,
) -> Result<Option<PersistedAuditTrailSnapshot>, AuditTrailError> {
    if !path.exists() {
        return Ok(None);
    }
    let content = fs::read_to_string(path)
        .map_err(|error| AuditTrailError::PersistenceIo(error.to_string()))?;
    if content.trim().is_empty() {
        return Ok(None);
    }
    let snapshot = serde_json::from_str(&content)
        .map_err(|error| AuditTrailError::PersistenceSerde(error.to_string()))?;
    Ok(Some(snapshot))
}

fn persist_state_if_needed(state: &AuditTrailState) -> Result<(), AuditTrailError> {
    let StorageBackend::JsonFile(path) = &state.storage_backend else {
        return Ok(());
    };

    if let Some(parent) = path.parent() {
        if !parent.as_os_str().is_empty() {
            fs::create_dir_all(parent)
                .map_err(|error| AuditTrailError::PersistenceIo(error.to_string()))?;
        }
    }
    let snapshot = state.to_persisted_snapshot();
    let serialized = serde_json::to_string_pretty(&snapshot)
        .map_err(|error| AuditTrailError::PersistenceSerde(error.to_string()))?;
    let file_name = path
        .file_name()
        .and_then(|value| value.to_str())
        .ok_or_else(|| {
            AuditTrailError::PersistenceIo("audit storage path is missing file name".to_owned())
        })?;
    let temp_path = path.with_file_name(format!("{file_name}.tmp"));
    fs::write(&temp_path, serialized)
        .map_err(|error| AuditTrailError::PersistenceIo(error.to_string()))?;
    if let Err(error) = fs::rename(&temp_path, path) {
        let _ = fs::remove_file(&temp_path);
        return Err(AuditTrailError::PersistenceIo(error.to_string()));
    }
    Ok(())
}

fn persisted_evidence_from_domain(
    evidence: &ImmutableAuditEvidence,
) -> PersistedImmutableAuditEvidence {
    PersistedImmutableAuditEvidence {
        evidence_id: evidence.evidence_id(),
        occurred_at: PersistedAuditTimestamp {
            epoch_day: evidence.occurred_at().epoch_day(),
            minute_of_day: evidence.occurred_at().minute_of_day(),
        },
        audit_identity: persisted_audit_identity_from_domain(evidence.audit_identity()),
        action: persisted_audit_action_from_domain(evidence.action()),
        entity: persisted_audit_entity_from_domain(evidence.entity()),
        correlation_id: evidence.correlation_id().as_str().to_owned(),
    }
}

fn domain_evidence_from_persisted(
    persisted: &PersistedImmutableAuditEvidence,
) -> Result<ImmutableAuditEvidence, AuditTrailError> {
    if persisted.evidence_id == 0 {
        return Err(AuditTrailError::PersistenceDataCorrupted(
            "persisted evidence_id must be greater than zero".to_owned(),
        ));
    }
    Ok(ImmutableAuditEvidence {
        evidence_id: persisted.evidence_id,
        occurred_at: AuditTimestamp::new(
            persisted.occurred_at.epoch_day,
            persisted.occurred_at.minute_of_day,
        )?,
        audit_identity: domain_audit_identity_from_persisted(&persisted.audit_identity)?,
        action: domain_audit_action_from_persisted(persisted.action),
        entity: domain_audit_entity_from_persisted(&persisted.entity)?,
        correlation_id: AuditCorrelationId::parse(persisted.correlation_id.clone())?,
    })
}

fn persisted_audit_identity_from_domain(identity: &AuditIdentityLink) -> PersistedAuditIdentity {
    PersistedAuditIdentity {
        actor_id: identity.actor_id().as_str().to_owned(),
        role: persisted_role_from_domain(identity.role()),
        authentication_source: persisted_authentication_source_from_domain(
            identity.authentication_source(),
        ),
        plant_scope: persisted_plant_scope_from_domain(identity.plant_scope()),
        operation_id: identity.operation_id().to_owned(),
    }
}

fn domain_audit_identity_from_persisted(
    persisted: &PersistedAuditIdentity,
) -> Result<AuditIdentityLink, AuditTrailError> {
    let actor_id = ActorId::parse(persisted.actor_id.clone())
        .map_err(|error| AuditTrailError::PersistenceDataCorrupted(error.to_string()))?;
    let role = domain_role_from_persisted(persisted.role);
    let authentication_source =
        domain_authentication_source_from_persisted(persisted.authentication_source);
    let plant_scope = domain_plant_scope_from_persisted(&persisted.plant_scope)?;
    let actor = AuthenticatedActorContext::new(actor_id, role, plant_scope, authentication_source)
        .map_err(|error| AuditTrailError::PersistenceDataCorrupted(error.to_string()))?;
    Ok(AuditIdentityLink::from_actor(
        &actor,
        persisted.operation_id.clone(),
    ))
}

fn persisted_plant_scope_from_domain(scope: &PlantScope) -> PersistedPlantScope {
    match scope {
        PlantScope::AllPlants => PersistedPlantScope {
            all_plants: true,
            plant_ids: Vec::new(),
        },
        PlantScope::Restricted(plants) => PersistedPlantScope {
            all_plants: false,
            plant_ids: plants
                .iter()
                .map(|plant_id| plant_id.as_str().to_owned())
                .collect(),
        },
    }
}

fn domain_plant_scope_from_persisted(
    persisted: &PersistedPlantScope,
) -> Result<PlantScope, AuditTrailError> {
    if persisted.all_plants {
        return Ok(PlantScope::all());
    }
    let plant_ids = persisted
        .plant_ids
        .iter()
        .map(|plant_id| {
            crate::identity::PlantId::parse(plant_id.clone())
                .map_err(|error| AuditTrailError::PersistenceDataCorrupted(error.to_string()))
        })
        .collect::<Result<Vec<_>, _>>()?;
    PlantScope::restricted(plant_ids)
        .map_err(|error| AuditTrailError::PersistenceDataCorrupted(error.to_string()))
}

fn persisted_role_from_domain(role: Role) -> PersistedRole {
    match role {
        Role::Employee => PersistedRole::Employee,
        Role::VendorOperator => PersistedRole::VendorOperator,
        Role::CommitteeAdmin => PersistedRole::CommitteeAdmin,
        Role::PayrollOperator => PersistedRole::PayrollOperator,
    }
}

fn domain_role_from_persisted(role: PersistedRole) -> Role {
    match role {
        PersistedRole::Employee => Role::Employee,
        PersistedRole::VendorOperator => Role::VendorOperator,
        PersistedRole::CommitteeAdmin => Role::CommitteeAdmin,
        PersistedRole::PayrollOperator => Role::PayrollOperator,
    }
}

fn persisted_authentication_source_from_domain(
    source: AuthenticationSource,
) -> PersistedAuthenticationSource {
    match source {
        AuthenticationSource::CorporateSso => PersistedAuthenticationSource::CorporateSso,
        AuthenticationSource::VendorAccountMfa => PersistedAuthenticationSource::VendorAccountMfa,
    }
}

fn domain_authentication_source_from_persisted(
    source: PersistedAuthenticationSource,
) -> AuthenticationSource {
    match source {
        PersistedAuthenticationSource::CorporateSso => AuthenticationSource::CorporateSso,
        PersistedAuthenticationSource::VendorAccountMfa => AuthenticationSource::VendorAccountMfa,
    }
}

fn persisted_audit_action_from_domain(action: AuditAction) -> PersistedAuditAction {
    match action {
        AuditAction::CreateEmployeeOrder => PersistedAuditAction::CreateEmployeeOrder,
        AuditAction::UpdateEmployeeOrder => PersistedAuditAction::UpdateEmployeeOrder,
        AuditAction::VerifyPickupOrder => PersistedAuditAction::VerifyPickupOrder,
        AuditAction::MarkOrderSoldOut => PersistedAuditAction::MarkOrderSoldOut,
        AuditAction::MarkOrderRefundPending => PersistedAuditAction::MarkOrderRefundPending,
        AuditAction::MarkOrderRefunded => PersistedAuditAction::MarkOrderRefunded,
        AuditAction::UpsertVendorMenuItem => PersistedAuditAction::UpsertVendorMenuItem,
        AuditAction::UpsertVendorOrderingPolicy => PersistedAuditAction::UpsertVendorOrderingPolicy,
        AuditAction::AdvanceVendorFulfillmentDeliveryStatus => {
            PersistedAuditAction::AdvanceVendorFulfillmentDeliveryStatus
        }
        AuditAction::CreateVendorFulfillmentExportBatch => {
            PersistedAuditAction::CreateVendorFulfillmentExportBatch
        }
        AuditAction::UpsertVendorPlantDeliveryMapping => {
            PersistedAuditAction::UpsertVendorPlantDeliveryMapping
        }
        AuditAction::DeleteVendorPlantDeliveryMapping => {
            PersistedAuditAction::DeleteVendorPlantDeliveryMapping
        }
        AuditAction::UpsertComplianceDocumentTemplate => {
            PersistedAuditAction::UpsertComplianceDocumentTemplate
        }
        AuditAction::RegisterVendorApplication => PersistedAuditAction::RegisterVendorApplication,
        AuditAction::SubmitVendorComplianceDocument => {
            PersistedAuditAction::SubmitVendorComplianceDocument
        }
        AuditAction::ReviewVendorApplication => PersistedAuditAction::ReviewVendorApplication,
        AuditAction::RunVendorComplianceLifecycle => {
            PersistedAuditAction::RunVendorComplianceLifecycle
        }
        AuditAction::PurgeAuditEvidence => PersistedAuditAction::PurgeAuditEvidence,
        AuditAction::PruneVendorComplianceHistory => {
            PersistedAuditAction::PruneVendorComplianceHistory
        }
        AuditAction::ExportPayrollDeductions => PersistedAuditAction::ExportPayrollDeductions,
    }
}

fn domain_audit_action_from_persisted(action: PersistedAuditAction) -> AuditAction {
    match action {
        PersistedAuditAction::CreateEmployeeOrder => AuditAction::CreateEmployeeOrder,
        PersistedAuditAction::UpdateEmployeeOrder => AuditAction::UpdateEmployeeOrder,
        PersistedAuditAction::VerifyPickupOrder => AuditAction::VerifyPickupOrder,
        PersistedAuditAction::MarkOrderSoldOut => AuditAction::MarkOrderSoldOut,
        PersistedAuditAction::MarkOrderRefundPending => AuditAction::MarkOrderRefundPending,
        PersistedAuditAction::MarkOrderRefunded => AuditAction::MarkOrderRefunded,
        PersistedAuditAction::UpsertVendorMenuItem => AuditAction::UpsertVendorMenuItem,
        PersistedAuditAction::UpsertVendorOrderingPolicy => AuditAction::UpsertVendorOrderingPolicy,
        PersistedAuditAction::AdvanceVendorFulfillmentDeliveryStatus => {
            AuditAction::AdvanceVendorFulfillmentDeliveryStatus
        }
        PersistedAuditAction::CreateVendorFulfillmentExportBatch => {
            AuditAction::CreateVendorFulfillmentExportBatch
        }
        PersistedAuditAction::UpsertVendorPlantDeliveryMapping => {
            AuditAction::UpsertVendorPlantDeliveryMapping
        }
        PersistedAuditAction::DeleteVendorPlantDeliveryMapping => {
            AuditAction::DeleteVendorPlantDeliveryMapping
        }
        PersistedAuditAction::UpsertComplianceDocumentTemplate => {
            AuditAction::UpsertComplianceDocumentTemplate
        }
        PersistedAuditAction::RegisterVendorApplication => AuditAction::RegisterVendorApplication,
        PersistedAuditAction::SubmitVendorComplianceDocument => {
            AuditAction::SubmitVendorComplianceDocument
        }
        PersistedAuditAction::ReviewVendorApplication => AuditAction::ReviewVendorApplication,
        PersistedAuditAction::RunVendorComplianceLifecycle => {
            AuditAction::RunVendorComplianceLifecycle
        }
        PersistedAuditAction::PurgeAuditEvidence => AuditAction::PurgeAuditEvidence,
        PersistedAuditAction::PruneVendorComplianceHistory => {
            AuditAction::PruneVendorComplianceHistory
        }
        PersistedAuditAction::ExportPayrollDeductions => AuditAction::ExportPayrollDeductions,
    }
}

fn persisted_audit_entity_from_domain(entity: &AuditEntityRef) -> PersistedAuditEntity {
    PersistedAuditEntity {
        entity_type: persisted_entity_type_from_domain(entity.entity_type()),
        entity_id: entity.entity_id().to_owned(),
    }
}

fn domain_audit_entity_from_persisted(
    entity: &PersistedAuditEntity,
) -> Result<AuditEntityRef, AuditTrailError> {
    AuditEntityRef::new(
        domain_entity_type_from_persisted(entity.entity_type),
        entity.entity_id.clone(),
    )
}

fn persisted_entity_type_from_domain(entity_type: AuditEntityType) -> PersistedAuditEntityType {
    match entity_type {
        AuditEntityType::Order => PersistedAuditEntityType::Order,
        AuditEntityType::MenuItem => PersistedAuditEntityType::MenuItem,
        AuditEntityType::Vendor => PersistedAuditEntityType::Vendor,
        AuditEntityType::DeliveryMapping => PersistedAuditEntityType::DeliveryMapping,
        AuditEntityType::ComplianceDocumentTemplate => {
            PersistedAuditEntityType::ComplianceDocumentTemplate
        }
        AuditEntityType::FulfillmentBatch => PersistedAuditEntityType::FulfillmentBatch,
        AuditEntityType::Settlement => PersistedAuditEntityType::Settlement,
        AuditEntityType::VendorOrderingPolicy => PersistedAuditEntityType::VendorOrderingPolicy,
        AuditEntityType::AuditTrail => PersistedAuditEntityType::AuditTrail,
    }
}

fn domain_entity_type_from_persisted(entity_type: PersistedAuditEntityType) -> AuditEntityType {
    match entity_type {
        PersistedAuditEntityType::Order => AuditEntityType::Order,
        PersistedAuditEntityType::MenuItem => AuditEntityType::MenuItem,
        PersistedAuditEntityType::Vendor => AuditEntityType::Vendor,
        PersistedAuditEntityType::DeliveryMapping => AuditEntityType::DeliveryMapping,
        PersistedAuditEntityType::ComplianceDocumentTemplate => {
            AuditEntityType::ComplianceDocumentTemplate
        }
        PersistedAuditEntityType::FulfillmentBatch => AuditEntityType::FulfillmentBatch,
        PersistedAuditEntityType::Settlement => AuditEntityType::Settlement,
        PersistedAuditEntityType::VendorOrderingPolicy => AuditEntityType::VendorOrderingPolicy,
        PersistedAuditEntityType::AuditTrail => AuditEntityType::AuditTrail,
    }
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
    PersistenceIo(String),
    PersistenceSerde(String),
    PersistenceDataCorrupted(String),
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
            Self::PersistenceIo(message) => write!(f, "audit persistence io failed: {message}"),
            Self::PersistenceSerde(message) => {
                write!(f, "audit persistence serialization failed: {message}")
            }
            Self::PersistenceDataCorrupted(message) => {
                write!(f, "audit persistence data is corrupted: {message}")
            }
        }
    }
}

impl std::error::Error for AuditTrailError {}
