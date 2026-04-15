use std::cmp::Ordering;
use std::collections::BTreeMap;
use std::fmt;
use std::fs;
use std::path::{Path, PathBuf};

use serde::{Deserialize, Serialize};

use crate::audit::AuditIdentityLink;
use crate::identity::{
    ActorId, AuthenticatedActorContext, AuthenticationSource, PlantId, PlantScope, Role,
};
use crate::vendor_compliance::{VendorComplianceLifecycle, VendorId};

const MINUTES_PER_DAY: u16 = 24 * 60;
const SECONDS_PER_DAY: i64 = 86_400;
const TAIPEI_FIXED_OFFSET_SECONDS: i64 = 8 * 60 * 60;
const UPSERT_MAPPING_OPERATION_ID: &str = "upsertVendorPlantDeliveryMapping";
const REMOVE_MAPPING_OPERATION_ID: &str = "deleteVendorPlantDeliveryMapping";

#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum DeliverabilityApi {
    Browse,
    Search,
    Order,
}

impl DeliverabilityApi {
    pub const fn as_str(self) -> &'static str {
        match self {
            Self::Browse => "browse",
            Self::Search => "search",
            Self::Order => "order",
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq, PartialOrd, Ord, Hash)]
pub struct DeliveryMappingId(String);

impl DeliveryMappingId {
    pub fn parse(value: impl Into<String>) -> Result<Self, VendorPlantDeliveryError> {
        let value = value.into();
        if value.trim().is_empty() {
            return Err(VendorPlantDeliveryError::InvalidMappingId);
        }
        Ok(Self(value))
    }

    pub fn as_str(&self) -> &str {
        &self.0
    }
}

impl fmt::Display for DeliveryMappingId {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(self.as_str())
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Hash)]
pub struct TaipeiBusinessMoment {
    epoch_day: i32,
    minute_of_day: u16,
}

impl TaipeiBusinessMoment {
    pub fn new(epoch_day: i32, minute_of_day: u16) -> Result<Self, VendorPlantDeliveryError> {
        if minute_of_day >= MINUTES_PER_DAY {
            return Err(VendorPlantDeliveryError::InvalidMinuteOfDay { minute_of_day });
        }
        Ok(Self {
            epoch_day,
            minute_of_day,
        })
    }

    pub fn from_utc_unix_seconds(unix_seconds: i64) -> Result<Self, VendorPlantDeliveryError> {
        let shifted_seconds = unix_seconds
            .checked_add(TAIPEI_FIXED_OFFSET_SECONDS)
            .ok_or(VendorPlantDeliveryError::TimestampOutOfRange)?;
        let epoch_day = i32::try_from(shifted_seconds.div_euclid(SECONDS_PER_DAY))
            .map_err(|_| VendorPlantDeliveryError::TimestampOutOfRange)?;
        let minute_of_day = u16::try_from(shifted_seconds.rem_euclid(SECONDS_PER_DAY) / 60)
            .map_err(|_| VendorPlantDeliveryError::TimestampOutOfRange)?;
        Self::new(epoch_day, minute_of_day)
    }

    pub fn epoch_day(self) -> i32 {
        self.epoch_day
    }

    pub fn minute_of_day(self) -> u16 {
        self.minute_of_day
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ServiceWindow {
    starts_at: TaipeiBusinessMoment,
    ends_at: TaipeiBusinessMoment,
}

impl ServiceWindow {
    pub fn new(
        starts_at: TaipeiBusinessMoment,
        ends_at: TaipeiBusinessMoment,
    ) -> Result<Self, VendorPlantDeliveryError> {
        if ends_at <= starts_at {
            return Err(VendorPlantDeliveryError::InvalidServiceWindow);
        }
        Ok(Self { starts_at, ends_at })
    }

    pub fn starts_at(&self) -> TaipeiBusinessMoment {
        self.starts_at
    }

    pub fn ends_at(&self) -> TaipeiBusinessMoment {
        self.ends_at
    }

    pub fn is_active_at(&self, moment: TaipeiBusinessMoment) -> bool {
        moment >= self.starts_at && moment < self.ends_at
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum DeliveryRuleEffect {
    Allow,
    Deny,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct VendorPlantDeliveryMapping {
    mapping_id: DeliveryMappingId,
    vendor_id: VendorId,
    plant_id: PlantId,
    service_window: ServiceWindow,
    effect: DeliveryRuleEffect,
    precedence: u16,
}

impl VendorPlantDeliveryMapping {
    pub fn new(
        mapping_id: DeliveryMappingId,
        vendor_id: VendorId,
        plant_id: PlantId,
        service_window: ServiceWindow,
        effect: DeliveryRuleEffect,
        precedence: u16,
    ) -> Self {
        Self {
            mapping_id,
            vendor_id,
            plant_id,
            service_window,
            effect,
            precedence,
        }
    }

    pub fn mapping_id(&self) -> &DeliveryMappingId {
        &self.mapping_id
    }

    pub fn vendor_id(&self) -> &VendorId {
        &self.vendor_id
    }

    pub fn plant_id(&self) -> &PlantId {
        &self.plant_id
    }

    pub fn service_window(&self) -> &ServiceWindow {
        &self.service_window
    }

    pub fn effect(&self) -> DeliveryRuleEffect {
        self.effect
    }

    pub fn precedence(&self) -> u16 {
        self.precedence
    }

    fn applies_to(&self, plant_id: &PlantId, at: TaipeiBusinessMoment) -> bool {
        self.plant_id == *plant_id && self.service_window.is_active_at(at)
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum DeliveryMappingAuditKind {
    Upserted,
    Removed,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct DeliveryMappingAuditEntry {
    occurred_at: TaipeiBusinessMoment,
    audit_identity: AuditIdentityLink,
    kind: DeliveryMappingAuditKind,
    mapping: VendorPlantDeliveryMapping,
}

impl DeliveryMappingAuditEntry {
    pub fn occurred_at(&self) -> TaipeiBusinessMoment {
        self.occurred_at
    }

    pub fn audit_identity(&self) -> &AuditIdentityLink {
        &self.audit_identity
    }

    pub fn kind(&self) -> DeliveryMappingAuditKind {
        self.kind
    }

    pub fn mapping(&self) -> &VendorPlantDeliveryMapping {
        &self.mapping
    }
}

#[derive(Debug, Clone)]
struct VersionedMapping {
    mapping: VendorPlantDeliveryMapping,
    revision: u64,
}

#[derive(Debug, Clone)]
enum StorageBackend {
    InMemory,
    JsonFile(PathBuf),
}

impl Default for StorageBackend {
    fn default() -> Self {
        Self::InMemory
    }
}

#[derive(Debug, Clone)]
struct PolicyStateSnapshot {
    mappings_by_vendor: BTreeMap<VendorId, BTreeMap<DeliveryMappingId, VersionedMapping>>,
    audit_log: Vec<DeliveryMappingAuditEntry>,
    next_revision: u64,
}

#[derive(Debug, Clone, Default)]
pub struct VendorPlantDeliveryPolicy {
    mappings_by_vendor: BTreeMap<VendorId, BTreeMap<DeliveryMappingId, VersionedMapping>>,
    audit_log: Vec<DeliveryMappingAuditEntry>,
    next_revision: u64,
    storage_backend: StorageBackend,
}

impl VendorPlantDeliveryPolicy {
    pub fn new() -> Self {
        Self::default()
    }

    pub fn with_json_storage(path: impl Into<PathBuf>) -> Result<Self, VendorPlantDeliveryError> {
        let path = path.into();
        let persisted_snapshot = load_snapshot_from_json_file(&path)?;
        let policy = match persisted_snapshot {
            Some(snapshot) => {
                Self::from_persisted_snapshot(snapshot, StorageBackend::JsonFile(path.clone()))?
            }
            None => Self {
                storage_backend: StorageBackend::JsonFile(path.clone()),
                ..Self::default()
            },
        };
        if !path.exists() {
            policy.persist_if_needed()?;
        }
        Ok(policy)
    }

    pub fn upsert_mapping(
        &mut self,
        actor: &AuthenticatedActorContext,
        changed_at: TaipeiBusinessMoment,
        mapping: VendorPlantDeliveryMapping,
    ) -> Result<(), VendorPlantDeliveryError> {
        ensure_role(actor, Role::CommitteeAdmin)?;
        let previous_state = self.capture_state();
        let revision = self.next_revision();
        let vendor_id = mapping.vendor_id().clone();
        let mapping_id = mapping.mapping_id().clone();
        self.mappings_by_vendor
            .entry(vendor_id)
            .or_default()
            .insert(
                mapping_id,
                VersionedMapping {
                    mapping: mapping.clone(),
                    revision,
                },
            );
        self.audit_log.push(DeliveryMappingAuditEntry {
            occurred_at: changed_at,
            audit_identity: AuditIdentityLink::from_actor(actor, UPSERT_MAPPING_OPERATION_ID),
            kind: DeliveryMappingAuditKind::Upserted,
            mapping,
        });
        if let Err(error) = self.persist_if_needed() {
            self.restore_state(previous_state);
            return Err(error);
        }
        Ok(())
    }

    pub fn remove_mapping(
        &mut self,
        actor: &AuthenticatedActorContext,
        changed_at: TaipeiBusinessMoment,
        vendor_id: &VendorId,
        mapping_id: &DeliveryMappingId,
    ) -> Result<(), VendorPlantDeliveryError> {
        ensure_role(actor, Role::CommitteeAdmin)?;
        let previous_state = self.capture_state();

        let (removed_mapping, remove_vendor_entry) = {
            let vendor_mappings = self.mappings_by_vendor.get_mut(vendor_id).ok_or_else(|| {
                VendorPlantDeliveryError::MappingNotFound {
                    vendor_id: vendor_id.clone(),
                    mapping_id: mapping_id.clone(),
                }
            })?;
            let removed = vendor_mappings.remove(mapping_id).ok_or_else(|| {
                VendorPlantDeliveryError::MappingNotFound {
                    vendor_id: vendor_id.clone(),
                    mapping_id: mapping_id.clone(),
                }
            })?;
            (removed.mapping, vendor_mappings.is_empty())
        };

        if remove_vendor_entry {
            self.mappings_by_vendor.remove(vendor_id);
        }

        self.audit_log.push(DeliveryMappingAuditEntry {
            occurred_at: changed_at,
            audit_identity: AuditIdentityLink::from_actor(actor, REMOVE_MAPPING_OPERATION_ID),
            kind: DeliveryMappingAuditKind::Removed,
            mapping: removed_mapping,
        });
        if let Err(error) = self.persist_if_needed() {
            self.restore_state(previous_state);
            return Err(error);
        }
        Ok(())
    }

    pub fn mappings_for_vendor(&self, vendor_id: &VendorId) -> Vec<&VendorPlantDeliveryMapping> {
        self.mappings_by_vendor
            .get(vendor_id)
            .map(|mappings| mappings.values().map(|mapping| &mapping.mapping).collect())
            .unwrap_or_default()
    }

    pub fn audit_log(&self) -> &[DeliveryMappingAuditEntry] {
        &self.audit_log
    }

    pub fn employee_visible_vendor_ids_for_browse(
        &self,
        compliance: &VendorComplianceLifecycle,
        plant_id: &PlantId,
        at: TaipeiBusinessMoment,
    ) -> Vec<VendorId> {
        self.employee_visible_vendor_ids_for_api(
            compliance,
            plant_id,
            at,
            DeliverabilityApi::Browse,
        )
    }

    pub fn employee_visible_vendor_ids_for_search(
        &self,
        compliance: &VendorComplianceLifecycle,
        plant_id: &PlantId,
        at: TaipeiBusinessMoment,
    ) -> Vec<VendorId> {
        self.employee_visible_vendor_ids_for_api(
            compliance,
            plant_id,
            at,
            DeliverabilityApi::Search,
        )
    }

    pub fn ensure_vendor_deliverable_for_browse(
        &self,
        compliance: &VendorComplianceLifecycle,
        vendor_id: &VendorId,
        plant_id: &PlantId,
        at: TaipeiBusinessMoment,
    ) -> Result<(), VendorPlantDeliveryError> {
        self.ensure_vendor_deliverable_for_api(
            compliance,
            vendor_id,
            plant_id,
            at,
            DeliverabilityApi::Browse,
        )
    }

    pub fn ensure_vendor_deliverable_for_search(
        &self,
        compliance: &VendorComplianceLifecycle,
        vendor_id: &VendorId,
        plant_id: &PlantId,
        at: TaipeiBusinessMoment,
    ) -> Result<(), VendorPlantDeliveryError> {
        self.ensure_vendor_deliverable_for_api(
            compliance,
            vendor_id,
            plant_id,
            at,
            DeliverabilityApi::Search,
        )
    }

    pub fn ensure_vendor_deliverable_for_order(
        &self,
        compliance: &VendorComplianceLifecycle,
        vendor_id: &VendorId,
        plant_id: &PlantId,
        at: TaipeiBusinessMoment,
    ) -> Result<(), VendorPlantDeliveryError> {
        self.ensure_vendor_deliverable_for_api(
            compliance,
            vendor_id,
            plant_id,
            at,
            DeliverabilityApi::Order,
        )
    }

    fn employee_visible_vendor_ids_for_api(
        &self,
        compliance: &VendorComplianceLifecycle,
        plant_id: &PlantId,
        at: TaipeiBusinessMoment,
        api: DeliverabilityApi,
    ) -> Vec<VendorId> {
        compliance
            .visible_vendor_ids_for_ordering()
            .into_iter()
            .filter_map(|vendor_id| {
                self.ensure_vendor_deliverable_for_api(compliance, vendor_id, plant_id, at, api)
                    .ok()
                    .map(|_| vendor_id.clone())
            })
            .collect()
    }

    fn ensure_vendor_deliverable_for_api(
        &self,
        compliance: &VendorComplianceLifecycle,
        vendor_id: &VendorId,
        plant_id: &PlantId,
        at: TaipeiBusinessMoment,
        api: DeliverabilityApi,
    ) -> Result<(), VendorPlantDeliveryError> {
        let vendor = compliance
            .vendor(vendor_id)
            .ok_or_else(|| VendorPlantDeliveryError::VendorNotFound(vendor_id.clone()))?;
        if !vendor.is_visible_for_ordering() {
            return Err(VendorPlantDeliveryError::VendorNotEligibleForOrdering(
                vendor_id.clone(),
            ));
        }

        let Some(effective_mapping) = self.select_effective_mapping(vendor_id, plant_id, at) else {
            return Err(VendorPlantDeliveryError::DeliverabilityRuleMissing {
                vendor_id: vendor_id.clone(),
                plant_id: plant_id.clone(),
                api,
            });
        };

        if effective_mapping.mapping.effect() == DeliveryRuleEffect::Deny {
            return Err(VendorPlantDeliveryError::DeliverabilityDenied {
                vendor_id: vendor_id.clone(),
                plant_id: plant_id.clone(),
                mapping_id: effective_mapping.mapping.mapping_id().clone(),
                api,
            });
        }

        Ok(())
    }

    fn select_effective_mapping(
        &self,
        vendor_id: &VendorId,
        plant_id: &PlantId,
        at: TaipeiBusinessMoment,
    ) -> Option<&VersionedMapping> {
        self.mappings_by_vendor
            .get(vendor_id)?
            .values()
            .filter(|mapping| mapping.mapping.applies_to(plant_id, at))
            .max_by(|left, right| compare_versioned_mapping(left, right))
    }

    fn next_revision(&mut self) -> u64 {
        self.next_revision = self.next_revision.saturating_add(1);
        self.next_revision
    }

    fn capture_state(&self) -> PolicyStateSnapshot {
        PolicyStateSnapshot {
            mappings_by_vendor: self.mappings_by_vendor.clone(),
            audit_log: self.audit_log.clone(),
            next_revision: self.next_revision,
        }
    }

    fn restore_state(&mut self, snapshot: PolicyStateSnapshot) {
        self.mappings_by_vendor = snapshot.mappings_by_vendor;
        self.audit_log = snapshot.audit_log;
        self.next_revision = snapshot.next_revision;
    }

    fn persist_if_needed(&self) -> Result<(), VendorPlantDeliveryError> {
        let StorageBackend::JsonFile(path) = &self.storage_backend else {
            return Ok(());
        };

        if let Some(parent) = path.parent() {
            fs::create_dir_all(parent)
                .map_err(|error| VendorPlantDeliveryError::PersistenceIo(error.to_string()))?;
        }

        let snapshot = self.to_persisted_snapshot();
        let serialized = serde_json::to_string_pretty(&snapshot)
            .map_err(|error| VendorPlantDeliveryError::PersistenceSerde(error.to_string()))?;
        fs::write(path, serialized)
            .map_err(|error| VendorPlantDeliveryError::PersistenceIo(error.to_string()))?;
        Ok(())
    }

    fn to_persisted_snapshot(&self) -> PersistedPolicySnapshot {
        let mappings = self
            .mappings_by_vendor
            .iter()
            .flat_map(|(_vendor_id, mappings)| mappings.values())
            .map(|versioned| PersistedVersionedMapping {
                mapping: persisted_mapping_from_domain(&versioned.mapping),
                revision: versioned.revision,
            })
            .collect();
        let audit_log = self
            .audit_log
            .iter()
            .map(|entry| PersistedAuditEntry {
                occurred_at: PersistedBusinessMoment::from_domain(entry.occurred_at()),
                kind: match entry.kind() {
                    DeliveryMappingAuditKind::Upserted => {
                        PersistedDeliveryMappingAuditKind::Upserted
                    }
                    DeliveryMappingAuditKind::Removed => PersistedDeliveryMappingAuditKind::Removed,
                },
                mapping: persisted_mapping_from_domain(entry.mapping()),
                audit_identity: persisted_audit_identity_from_domain(entry.audit_identity()),
            })
            .collect();

        PersistedPolicySnapshot {
            next_revision: self.next_revision,
            mappings,
            audit_log,
        }
    }

    fn from_persisted_snapshot(
        snapshot: PersistedPolicySnapshot,
        storage_backend: StorageBackend,
    ) -> Result<Self, VendorPlantDeliveryError> {
        let mut mappings_by_vendor: BTreeMap<
            VendorId,
            BTreeMap<DeliveryMappingId, VersionedMapping>,
        > = BTreeMap::new();
        for versioned in snapshot.mappings {
            let mapping = domain_mapping_from_persisted(&versioned.mapping)?;
            mappings_by_vendor
                .entry(mapping.vendor_id().clone())
                .or_default()
                .insert(
                    mapping.mapping_id().clone(),
                    VersionedMapping {
                        mapping,
                        revision: versioned.revision,
                    },
                );
        }

        let mut audit_log = Vec::with_capacity(snapshot.audit_log.len());
        for persisted_audit_entry in snapshot.audit_log {
            let actor_context =
                actor_context_from_persisted(&persisted_audit_entry.audit_identity)?;
            let audit_identity = AuditIdentityLink::from_actor(
                &actor_context,
                persisted_audit_entry.audit_identity.operation_id,
            );
            let kind = match persisted_audit_entry.kind {
                PersistedDeliveryMappingAuditKind::Upserted => DeliveryMappingAuditKind::Upserted,
                PersistedDeliveryMappingAuditKind::Removed => DeliveryMappingAuditKind::Removed,
            };
            audit_log.push(DeliveryMappingAuditEntry {
                occurred_at: persisted_audit_entry.occurred_at.to_domain()?,
                audit_identity,
                kind,
                mapping: domain_mapping_from_persisted(&persisted_audit_entry.mapping)?,
            });
        }

        Ok(Self {
            mappings_by_vendor,
            audit_log,
            next_revision: snapshot.next_revision,
            storage_backend,
        })
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
struct PersistedPolicySnapshot {
    next_revision: u64,
    mappings: Vec<PersistedVersionedMapping>,
    audit_log: Vec<PersistedAuditEntry>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
struct PersistedVersionedMapping {
    mapping: PersistedMapping,
    revision: u64,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
struct PersistedMapping {
    mapping_id: String,
    vendor_id: String,
    plant_id: String,
    service_window: PersistedServiceWindow,
    effect: PersistedDeliveryRuleEffect,
    precedence: u16,
}

#[derive(Debug, Clone, Copy, Serialize, Deserialize)]
#[serde(rename_all = "SCREAMING_SNAKE_CASE")]
enum PersistedDeliveryRuleEffect {
    Allow,
    Deny,
}

#[derive(Debug, Clone, Copy, Serialize, Deserialize)]
struct PersistedBusinessMoment {
    epoch_day: i32,
    minute_of_day: u16,
}

impl PersistedBusinessMoment {
    fn from_domain(moment: TaipeiBusinessMoment) -> Self {
        Self {
            epoch_day: moment.epoch_day(),
            minute_of_day: moment.minute_of_day(),
        }
    }

    fn to_domain(self) -> Result<TaipeiBusinessMoment, VendorPlantDeliveryError> {
        TaipeiBusinessMoment::new(self.epoch_day, self.minute_of_day)
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
struct PersistedServiceWindow {
    starts_at: PersistedBusinessMoment,
    ends_at: PersistedBusinessMoment,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
struct PersistedAuditEntry {
    occurred_at: PersistedBusinessMoment,
    audit_identity: PersistedAuditIdentity,
    kind: PersistedDeliveryMappingAuditKind,
    mapping: PersistedMapping,
}

#[derive(Debug, Clone, Copy, Serialize, Deserialize)]
#[serde(rename_all = "SCREAMING_SNAKE_CASE")]
enum PersistedDeliveryMappingAuditKind {
    Upserted,
    Removed,
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
#[serde(rename_all = "SCREAMING_SNAKE_CASE")]
enum PersistedRole {
    Employee,
    VendorOperator,
    CommitteeAdmin,
    PayrollOperator,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "SCREAMING_SNAKE_CASE")]
enum PersistedAuthenticationSource {
    CorporateSso,
    VendorAccountMfa,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
struct PersistedPlantScope {
    all_plants: bool,
    plant_ids: Vec<String>,
}

fn load_snapshot_from_json_file(
    path: &Path,
) -> Result<Option<PersistedPolicySnapshot>, VendorPlantDeliveryError> {
    if !path.exists() {
        return Ok(None);
    }
    let content = fs::read_to_string(path)
        .map_err(|error| VendorPlantDeliveryError::PersistenceIo(error.to_string()))?;
    if content.trim().is_empty() {
        return Ok(None);
    }
    let snapshot = serde_json::from_str(&content)
        .map_err(|error| VendorPlantDeliveryError::PersistenceSerde(error.to_string()))?;
    Ok(Some(snapshot))
}

fn persisted_mapping_from_domain(mapping: &VendorPlantDeliveryMapping) -> PersistedMapping {
    PersistedMapping {
        mapping_id: mapping.mapping_id().as_str().to_owned(),
        vendor_id: mapping.vendor_id().as_str().to_owned(),
        plant_id: mapping.plant_id().as_str().to_owned(),
        service_window: PersistedServiceWindow {
            starts_at: PersistedBusinessMoment::from_domain(mapping.service_window().starts_at()),
            ends_at: PersistedBusinessMoment::from_domain(mapping.service_window().ends_at()),
        },
        effect: match mapping.effect() {
            DeliveryRuleEffect::Allow => PersistedDeliveryRuleEffect::Allow,
            DeliveryRuleEffect::Deny => PersistedDeliveryRuleEffect::Deny,
        },
        precedence: mapping.precedence(),
    }
}

fn domain_mapping_from_persisted(
    mapping: &PersistedMapping,
) -> Result<VendorPlantDeliveryMapping, VendorPlantDeliveryError> {
    let starts_at = mapping.service_window.starts_at.to_domain()?;
    let ends_at = mapping.service_window.ends_at.to_domain()?;
    let effect = match mapping.effect {
        PersistedDeliveryRuleEffect::Allow => DeliveryRuleEffect::Allow,
        PersistedDeliveryRuleEffect::Deny => DeliveryRuleEffect::Deny,
    };
    Ok(VendorPlantDeliveryMapping::new(
        DeliveryMappingId::parse(mapping.mapping_id.clone())?,
        VendorId::parse(mapping.vendor_id.clone()).map_err(|error| {
            VendorPlantDeliveryError::PersistenceDataCorrupted(error.to_string())
        })?,
        PlantId::parse(mapping.plant_id.clone()).map_err(|error| {
            VendorPlantDeliveryError::PersistenceDataCorrupted(error.to_string())
        })?,
        ServiceWindow::new(starts_at, ends_at)?,
        effect,
        mapping.precedence,
    ))
}

fn persisted_audit_identity_from_domain(
    audit_identity: &AuditIdentityLink,
) -> PersistedAuditIdentity {
    PersistedAuditIdentity {
        actor_id: audit_identity.actor_id().as_str().to_owned(),
        role: persisted_role_from_domain(audit_identity.role()),
        authentication_source: persisted_authentication_source_from_domain(
            audit_identity.authentication_source(),
        ),
        plant_scope: persisted_plant_scope_from_domain(audit_identity.plant_scope()),
        operation_id: audit_identity.operation_id().to_owned(),
    }
}

fn actor_context_from_persisted(
    persisted: &PersistedAuditIdentity,
) -> Result<AuthenticatedActorContext, VendorPlantDeliveryError> {
    let actor_id = ActorId::parse(persisted.actor_id.clone())
        .map_err(|error| VendorPlantDeliveryError::PersistenceDataCorrupted(error.to_string()))?;
    let role = domain_role_from_persisted(persisted.role.clone());
    let authentication_source =
        domain_authentication_source_from_persisted(persisted.authentication_source.clone());
    let plant_scope = domain_plant_scope_from_persisted(&persisted.plant_scope)?;
    AuthenticatedActorContext::new(actor_id, role, plant_scope, authentication_source)
        .map_err(|error| VendorPlantDeliveryError::PersistenceDataCorrupted(error.to_string()))
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
    scope: &PersistedPlantScope,
) -> Result<PlantScope, VendorPlantDeliveryError> {
    if scope.all_plants {
        return Ok(PlantScope::all());
    }

    let plants = scope
        .plant_ids
        .iter()
        .map(|plant_id| {
            PlantId::parse(plant_id.clone()).map_err(|error| {
                VendorPlantDeliveryError::PersistenceDataCorrupted(error.to_string())
            })
        })
        .collect::<Result<Vec<_>, _>>()?;
    PlantScope::restricted(plants)
        .map_err(|error| VendorPlantDeliveryError::PersistenceDataCorrupted(error.to_string()))
}

fn compare_versioned_mapping(left: &VersionedMapping, right: &VersionedMapping) -> Ordering {
    left.mapping
        .precedence()
        .cmp(&right.mapping.precedence())
        .then_with(|| left.revision.cmp(&right.revision))
        .then_with(|| {
            left.mapping
                .mapping_id()
                .as_str()
                .cmp(right.mapping.mapping_id().as_str())
        })
}

fn ensure_role(
    actor: &AuthenticatedActorContext,
    required_role: Role,
) -> Result<(), VendorPlantDeliveryError> {
    if actor.role() != required_role {
        return Err(VendorPlantDeliveryError::UnauthorizedRole {
            expected: required_role,
            actual: actor.role(),
        });
    }
    Ok(())
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum VendorPlantDeliveryError {
    InvalidMappingId,
    InvalidMinuteOfDay {
        minute_of_day: u16,
    },
    InvalidServiceWindow,
    TimestampOutOfRange,
    UnauthorizedRole {
        expected: Role,
        actual: Role,
    },
    MappingNotFound {
        vendor_id: VendorId,
        mapping_id: DeliveryMappingId,
    },
    VendorNotFound(VendorId),
    VendorNotEligibleForOrdering(VendorId),
    DeliverabilityRuleMissing {
        vendor_id: VendorId,
        plant_id: PlantId,
        api: DeliverabilityApi,
    },
    DeliverabilityDenied {
        vendor_id: VendorId,
        plant_id: PlantId,
        mapping_id: DeliveryMappingId,
        api: DeliverabilityApi,
    },
    PersistenceIo(String),
    PersistenceSerde(String),
    PersistenceDataCorrupted(String),
}

impl fmt::Display for VendorPlantDeliveryError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::InvalidMappingId => f.write_str("delivery mapping id must not be empty"),
            Self::InvalidMinuteOfDay { minute_of_day } => write!(
                f,
                "minute_of_day must be between 0 and 1439, got {minute_of_day}"
            ),
            Self::InvalidServiceWindow => {
                f.write_str("service window end must be strictly after start")
            }
            Self::TimestampOutOfRange => {
                f.write_str("timestamp cannot be represented in Taipei business-time space")
            }
            Self::UnauthorizedRole { expected, actual } => write!(
                f,
                "operation requires role {expected:?}, but actor has role {actual:?}"
            ),
            Self::MappingNotFound {
                vendor_id,
                mapping_id,
            } => write!(
                f,
                "delivery mapping {mapping_id} for vendor {vendor_id} does not exist"
            ),
            Self::VendorNotFound(vendor_id) => {
                write!(f, "vendor {vendor_id} is not registered")
            }
            Self::VendorNotEligibleForOrdering(vendor_id) => {
                write!(f, "vendor {vendor_id} is not active for ordering")
            }
            Self::DeliverabilityRuleMissing {
                vendor_id,
                plant_id,
                api,
            } => write!(
                f,
                "vendor {vendor_id} has no active deliverability rule for plant {plant_id} during {} API evaluation",
                api.as_str()
            ),
            Self::DeliverabilityDenied {
                vendor_id,
                plant_id,
                mapping_id,
                api,
            } => write!(
                f,
                "vendor {vendor_id} is denied for plant {plant_id} by mapping {mapping_id} during {} API evaluation",
                api.as_str()
            ),
            Self::PersistenceIo(message) => {
                write!(f, "delivery mapping persistence I/O error: {message}")
            }
            Self::PersistenceSerde(message) => {
                write!(f, "delivery mapping persistence serialization error: {message}")
            }
            Self::PersistenceDataCorrupted(message) => {
                write!(f, "delivery mapping persisted state is corrupted: {message}")
            }
        }
    }
}

impl std::error::Error for VendorPlantDeliveryError {}
