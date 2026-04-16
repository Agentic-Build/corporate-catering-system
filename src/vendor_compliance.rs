use std::collections::{BTreeMap, BTreeSet};
use std::fmt;

use serde::{Deserialize, Serialize};

use crate::audit::{
    AuditAction, AuditCorrelationId, AuditEntityRef, AuditEntityType, AuditEvidenceWrite,
    AuditIdentityLink, AuditTimestamp, AuditTrailError, ImmutableAuditTrail,
};
use crate::identity::{ActorId, AuthenticatedActorContext, Role};
use crate::object_storage::ObjectStorageReference;
use crate::observability::{TelemetryOutcome, TelemetryService};

const UPSERT_COMPLIANCE_TEMPLATE_OPERATION_ID: &str = "upsertComplianceDocumentTemplate";
const REGISTER_VENDOR_APPLICATION_OPERATION_ID: &str = "registerVendorApplication";
const SUBMIT_VENDOR_DOCUMENT_OPERATION_ID: &str = "submitVendorComplianceDocument";
const REVIEW_VENDOR_APPLICATION_OPERATION_ID: &str = "reviewVendorApplication";
const RUN_VENDOR_LIFECYCLE_OPERATION_ID: &str = "runVendorComplianceLifecycle";
const PRUNE_VENDOR_HISTORY_OPERATION_ID: &str = "pruneVendorComplianceHistory";
const MINIO_BUCKET_COMPLIANCE_EVIDENCE_ENV: &str = "MINIO_BUCKET_COMPLIANCE_EVIDENCE";
const DEFAULT_COMPLIANCE_BUCKET: &str = "compliance-evidence";
const COMPLIANCE_DOCUMENT_OBJECT_KEY_PREFIX: &str = "compliance-documents";
const COMPLIANCE_DOCUMENT_MAX_SIZE_BYTES: u64 = 20 * 1024 * 1024;
const COMPLIANCE_DOCUMENT_ALLOWED_EXTENSIONS: [&str; 4] = ["pdf", "jpg", "jpeg", "png"];

#[derive(Debug, Clone, PartialEq, Eq, PartialOrd, Ord, Hash, Serialize, Deserialize)]
pub struct VendorId(String);

impl VendorId {
    pub fn parse(value: impl Into<String>) -> Result<Self, VendorComplianceError> {
        let value = value.into();
        if value.trim().is_empty() {
            return Err(VendorComplianceError::InvalidVendorId);
        }
        Ok(Self(value))
    }

    pub fn as_str(&self) -> &str {
        &self.0
    }
}

impl fmt::Display for VendorId {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(self.as_str())
    }
}

#[derive(Debug, Clone, PartialEq, Eq, PartialOrd, Ord, Hash, Serialize, Deserialize)]
pub struct VendorCategory(String);

impl VendorCategory {
    pub fn parse(value: impl Into<String>) -> Result<Self, VendorComplianceError> {
        let value = value.into();
        if value.trim().is_empty() {
            return Err(VendorComplianceError::InvalidVendorCategory);
        }
        Ok(Self(value))
    }

    pub fn as_str(&self) -> &str {
        &self.0
    }
}

impl fmt::Display for VendorCategory {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(self.as_str())
    }
}

#[derive(Debug, Clone, PartialEq, Eq, PartialOrd, Ord, Hash, Serialize, Deserialize)]
pub struct DocumentTemplateId(String);

impl DocumentTemplateId {
    pub fn parse(value: impl Into<String>) -> Result<Self, VendorComplianceError> {
        let value = value.into();
        if value.trim().is_empty() {
            return Err(VendorComplianceError::InvalidDocumentTemplateId);
        }
        Ok(Self(value))
    }

    pub fn as_str(&self) -> &str {
        &self.0
    }
}

impl fmt::Display for DocumentTemplateId {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(self.as_str())
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Hash, Serialize, Deserialize)]
pub struct ComplianceDate(i32);

impl ComplianceDate {
    pub const fn from_epoch_day(epoch_day: i32) -> Self {
        Self(epoch_day)
    }

    pub const fn epoch_day(self) -> i32 {
        self.0
    }

    pub const fn add_days(self, days: i32) -> Self {
        Self(self.0 + days)
    }

    pub const fn days_until(self, other: Self) -> i32 {
        other.0 - self.0
    }

    pub const fn days_since(self, earlier: Self) -> i32 {
        self.0 - earlier.0
    }
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct ComplianceDocumentTemplate {
    template_id: DocumentTemplateId,
    vendor_category: VendorCategory,
    display_name: String,
    required: bool,
    max_validity_days: u16,
    reminder_days_before_expiry: Vec<u16>,
    suspension_grace_days: u16,
}

impl ComplianceDocumentTemplate {
    #[allow(clippy::too_many_arguments)]
    pub fn new(
        template_id: DocumentTemplateId,
        vendor_category: VendorCategory,
        display_name: impl Into<String>,
        required: bool,
        max_validity_days: u16,
        reminder_days_before_expiry: Vec<u16>,
        suspension_grace_days: u16,
    ) -> Result<Self, VendorComplianceError> {
        let display_name = display_name.into();
        if display_name.trim().is_empty() {
            return Err(VendorComplianceError::InvalidTemplateDisplayName);
        }
        if max_validity_days == 0 {
            return Err(VendorComplianceError::InvalidTemplateValidityDays);
        }

        let unique_lead_days = reminder_days_before_expiry
            .into_iter()
            .collect::<BTreeSet<_>>()
            .into_iter()
            .collect::<Vec<_>>();

        if unique_lead_days
            .iter()
            .any(|days| *days > max_validity_days)
        {
            return Err(VendorComplianceError::ReminderLeadTimeOutOfRange);
        }

        let mut reminder_days_before_expiry = unique_lead_days;
        reminder_days_before_expiry.sort_unstable_by(|a, b| b.cmp(a));

        Ok(Self {
            template_id,
            vendor_category,
            display_name,
            required,
            max_validity_days,
            reminder_days_before_expiry,
            suspension_grace_days,
        })
    }

    pub fn template_id(&self) -> &DocumentTemplateId {
        &self.template_id
    }

    pub fn vendor_category(&self) -> &VendorCategory {
        &self.vendor_category
    }

    pub fn display_name(&self) -> &str {
        &self.display_name
    }

    pub fn required(&self) -> bool {
        self.required
    }

    pub fn max_validity_days(&self) -> u16 {
        self.max_validity_days
    }

    pub fn reminder_days_before_expiry(&self) -> &[u16] {
        &self.reminder_days_before_expiry
    }

    pub fn suspension_grace_days(&self) -> u16 {
        self.suspension_grace_days
    }
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct VendorDocumentSubmission {
    document_ref: String,
    submitted_on: ComplianceDate,
    expires_on: ComplianceDate,
}

impl VendorDocumentSubmission {
    pub fn new(
        document_ref: impl Into<String>,
        submitted_on: ComplianceDate,
        expires_on: ComplianceDate,
    ) -> Result<Self, VendorComplianceError> {
        let document_ref = document_ref.into();
        if document_ref.trim().is_empty() {
            return Err(VendorComplianceError::InvalidDocumentReference);
        }
        let object_ref = ObjectStorageReference::parse(document_ref.as_str())
            .map_err(|_| VendorComplianceError::InvalidDocumentReference)?;
        let expected_bucket = configured_compliance_bucket();
        let (bucket, key) = object_ref.split_parts();
        if bucket != expected_bucket.as_str() {
            return Err(VendorComplianceError::InvalidDocumentReference);
        }
        if !object_key_matches_expected_prefix(key, COMPLIANCE_DOCUMENT_OBJECT_KEY_PREFIX) {
            return Err(VendorComplianceError::InvalidDocumentReference);
        }
        if !object_key_matches_artifact_metadata(
            key,
            COMPLIANCE_DOCUMENT_MAX_SIZE_BYTES,
            &COMPLIANCE_DOCUMENT_ALLOWED_EXTENSIONS,
        ) {
            return Err(VendorComplianceError::InvalidDocumentReference);
        }
        if submitted_on >= expires_on {
            return Err(VendorComplianceError::InvalidDocumentExpiryWindow);
        }

        Ok(Self {
            document_ref,
            submitted_on,
            expires_on,
        })
    }

    pub fn document_ref(&self) -> &str {
        &self.document_ref
    }

    pub fn submitted_on(&self) -> ComplianceDate {
        self.submitted_on
    }

    pub fn expires_on(&self) -> ComplianceDate {
        self.expires_on
    }
}

fn configured_compliance_bucket() -> String {
    std::env::var(MINIO_BUCKET_COMPLIANCE_EVIDENCE_ENV)
        .ok()
        .map(|bucket| bucket.trim().to_owned())
        .filter(|bucket| !bucket.is_empty())
        .unwrap_or_else(|| DEFAULT_COMPLIANCE_BUCKET.to_owned())
}

fn object_key_matches_expected_prefix(object_key: &str, artifact_prefix: &str) -> bool {
    let segments = object_key
        .split('/')
        .filter(|segment| !segment.is_empty())
        .collect::<Vec<_>>();
    segments
        .iter()
        .position(|segment| *segment == artifact_prefix)
        .is_some_and(|index| segments.get(index + 1).is_some())
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

fn object_key_matches_artifact_metadata(
    object_key: &str,
    max_size_bytes: u64,
    allowed_extensions: &[&str],
) -> bool {
    let Some(object_file_name) = object_key.rsplit('/').next() else {
        return false;
    };
    let mut parts = object_file_name.splitn(3, '-');
    let Some(size_bytes_segment) = parts.next() else {
        return false;
    };
    let Some(digest_segment) = parts.next() else {
        return false;
    };
    let Some(file_name_segment) = parts.next() else {
        return false;
    };
    let Ok(size_bytes) = size_bytes_segment.parse::<u64>() else {
        return false;
    };
    if size_bytes == 0 || size_bytes > max_size_bytes {
        return false;
    }
    if digest_segment.is_empty()
        || !digest_segment
            .chars()
            .all(|character| character.is_ascii_hexdigit())
    {
        return false;
    }
    let Some(extension) = file_name_segment.rsplit('.').next() else {
        return false;
    };
    let normalized_extension = extension.to_ascii_lowercase();
    allowed_extensions.contains(&normalized_extension.as_str())
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

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
pub enum VendorReviewDecision {
    Approved,
    Rejected,
    RequestFix,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
pub enum VendorComplianceStatus {
    PendingReview,
    FixRequested,
    Active,
    Rejected,
    Suspended,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub enum SuspensionReason {
    MissingRequiredDocument {
        template_id: DocumentTemplateId,
    },
    ExpiredRequiredDocument {
        template_id: DocumentTemplateId,
        expired_on: ComplianceDate,
    },
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub enum ComplianceHistoryKind {
    ApplicationSubmitted {
        category: VendorCategory,
    },
    DocumentSubmitted {
        template_id: DocumentTemplateId,
        expires_on: ComplianceDate,
    },
    ReviewDecision {
        decision: VendorReviewDecision,
        comment: String,
    },
    ExpiryReminderIssued {
        template_id: DocumentTemplateId,
        expires_on: ComplianceDate,
        days_until_expiry: u16,
    },
    Suspended {
        reason: SuspensionReason,
    },
    Reinstated,
}

impl ComplianceHistoryKind {
    fn retention_bucket(&self) -> HistoryRetentionBucket {
        match self {
            Self::ApplicationSubmitted { .. }
            | Self::DocumentSubmitted { .. }
            | Self::ReviewDecision { .. } => HistoryRetentionBucket::Review,
            Self::ExpiryReminderIssued { .. } | Self::Suspended { .. } | Self::Reinstated => {
                HistoryRetentionBucket::Lifecycle
            }
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct ComplianceHistoryEntry {
    occurred_on: ComplianceDate,
    actor_id: ActorId,
    actor_role: Role,
    kind: ComplianceHistoryKind,
}

impl ComplianceHistoryEntry {
    pub fn occurred_on(&self) -> ComplianceDate {
        self.occurred_on
    }

    pub fn actor_id(&self) -> &ActorId {
        &self.actor_id
    }

    pub fn actor_role(&self) -> Role {
        self.actor_role
    }

    pub fn kind(&self) -> &ComplianceHistoryKind {
        &self.kind
    }
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct VendorComplianceRecord {
    vendor_id: VendorId,
    display_name: String,
    category: VendorCategory,
    status: VendorComplianceStatus,
    documents: BTreeMap<DocumentTemplateId, VendorDocumentSubmission>,
    history: Vec<ComplianceHistoryEntry>,
    suspension_reason: Option<SuspensionReason>,
    rejected_on: Option<ComplianceDate>,
    reminder_registry: BTreeSet<ReminderRegistryKey>,
}

impl VendorComplianceRecord {
    pub fn vendor_id(&self) -> &VendorId {
        &self.vendor_id
    }

    pub fn display_name(&self) -> &str {
        &self.display_name
    }

    pub fn category(&self) -> &VendorCategory {
        &self.category
    }

    pub fn status(&self) -> VendorComplianceStatus {
        self.status
    }

    pub fn documents(&self) -> &BTreeMap<DocumentTemplateId, VendorDocumentSubmission> {
        &self.documents
    }

    pub fn history(&self) -> &[ComplianceHistoryEntry] {
        &self.history
    }

    pub fn suspension_reason(&self) -> Option<&SuspensionReason> {
        self.suspension_reason.as_ref()
    }

    pub fn is_visible_for_ordering(&self) -> bool {
        self.status == VendorComplianceStatus::Active
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct HistoryRetentionPolicy {
    review_history_days: u16,
    lifecycle_history_days: u16,
    rejected_vendor_deletion_days: u16,
}

impl HistoryRetentionPolicy {
    pub fn new(
        review_history_days: u16,
        lifecycle_history_days: u16,
        rejected_vendor_deletion_days: u16,
    ) -> Result<Self, VendorComplianceError> {
        if review_history_days == 0
            || lifecycle_history_days == 0
            || rejected_vendor_deletion_days == 0
        {
            return Err(VendorComplianceError::InvalidHistoryRetentionPolicy);
        }

        Ok(Self {
            review_history_days,
            lifecycle_history_days,
            rejected_vendor_deletion_days,
        })
    }

    pub fn review_history_days(&self) -> u16 {
        self.review_history_days
    }

    pub fn lifecycle_history_days(&self) -> u16 {
        self.lifecycle_history_days
    }

    pub fn rejected_vendor_deletion_days(&self) -> u16 {
        self.rejected_vendor_deletion_days
    }
}

impl Default for HistoryRetentionPolicy {
    fn default() -> Self {
        Self {
            review_history_days: 2555,
            lifecycle_history_days: 2555,
            rejected_vendor_deletion_days: 365,
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct LifecycleExpiryReminder {
    vendor_id: VendorId,
    template_id: DocumentTemplateId,
    expires_on: ComplianceDate,
    days_until_expiry: u16,
}

impl LifecycleExpiryReminder {
    pub fn vendor_id(&self) -> &VendorId {
        &self.vendor_id
    }

    pub fn template_id(&self) -> &DocumentTemplateId {
        &self.template_id
    }

    pub fn expires_on(&self) -> ComplianceDate {
        self.expires_on
    }

    pub fn days_until_expiry(&self) -> u16 {
        self.days_until_expiry
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct LifecycleSuspension {
    vendor_id: VendorId,
    reason: SuspensionReason,
}

impl LifecycleSuspension {
    pub fn vendor_id(&self) -> &VendorId {
        &self.vendor_id
    }

    pub fn reason(&self) -> &SuspensionReason {
        &self.reason
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct LifecycleReinstatement {
    vendor_id: VendorId,
}

impl LifecycleReinstatement {
    pub fn vendor_id(&self) -> &VendorId {
        &self.vendor_id
    }
}

#[derive(Debug, Clone, PartialEq, Eq, Default)]
pub struct LifecycleRunResult {
    pub reminders: Vec<LifecycleExpiryReminder>,
    pub suspensions: Vec<LifecycleSuspension>,
    pub reinstatements: Vec<LifecycleReinstatement>,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
pub struct HistoryPruneResult {
    pub pruned_history_entries: usize,
    pub deleted_vendor_records: usize,
}

#[derive(Debug, Clone, Default)]
pub struct VendorComplianceLifecycle {
    templates_by_category:
        BTreeMap<VendorCategory, BTreeMap<DocumentTemplateId, ComplianceDocumentTemplate>>,
    vendors: BTreeMap<VendorId, VendorComplianceRecord>,
    retention_policy: HistoryRetentionPolicy,
    audit_trail: ImmutableAuditTrail,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub(crate) struct VendorComplianceLifecycleSnapshot {
    templates_by_category:
        BTreeMap<VendorCategory, BTreeMap<DocumentTemplateId, ComplianceDocumentTemplate>>,
    vendors: BTreeMap<VendorId, VendorComplianceRecord>,
}

impl VendorComplianceLifecycle {
    pub fn new(retention_policy: HistoryRetentionPolicy) -> Self {
        Self::with_audit_trail(retention_policy, ImmutableAuditTrail::default())
    }

    pub fn with_audit_trail(
        retention_policy: HistoryRetentionPolicy,
        audit_trail: ImmutableAuditTrail,
    ) -> Self {
        Self {
            templates_by_category: BTreeMap::new(),
            vendors: BTreeMap::new(),
            retention_policy,
            audit_trail,
        }
    }

    pub fn retention_policy(&self) -> &HistoryRetentionPolicy {
        &self.retention_policy
    }

    pub fn audit_trail(&self) -> ImmutableAuditTrail {
        self.audit_trail.clone()
    }

    pub fn templates_for_category(
        &self,
        category: &VendorCategory,
    ) -> Option<&BTreeMap<DocumentTemplateId, ComplianceDocumentTemplate>> {
        self.templates_by_category.get(category)
    }

    pub fn vendor(&self, vendor_id: &VendorId) -> Option<&VendorComplianceRecord> {
        self.vendors.get(vendor_id)
    }

    pub fn vendor_has_document_reference(
        &self,
        vendor_id: &VendorId,
        object_ref: &ObjectStorageReference,
    ) -> bool {
        self.vendors.get(vendor_id).is_some_and(|vendor| {
            vendor
                .documents()
                .values()
                .any(|submission| submission.document_ref() == object_ref.as_str())
        })
    }

    pub fn visible_vendor_ids_for_ordering(&self) -> Vec<&VendorId> {
        self.vendors
            .values()
            .filter(|vendor| vendor.is_visible_for_ordering())
            .map(|vendor| vendor.vendor_id())
            .collect()
    }

    pub(crate) fn snapshot(&self) -> VendorComplianceLifecycleSnapshot {
        VendorComplianceLifecycleSnapshot {
            templates_by_category: self.templates_by_category.clone(),
            vendors: self.vendors.clone(),
        }
    }

    pub(crate) fn from_snapshot(
        snapshot: VendorComplianceLifecycleSnapshot,
        retention_policy: HistoryRetentionPolicy,
        audit_trail: ImmutableAuditTrail,
    ) -> Result<Self, VendorComplianceError> {
        for (category_key, templates) in &snapshot.templates_by_category {
            for (template_id_key, template) in templates {
                if template.template_id() != template_id_key {
                    return Err(VendorComplianceError::PersistenceDataCorrupted(format!(
                        "template key `{}` does not match payload template id `{}`",
                        template_id_key.as_str(),
                        template.template_id().as_str()
                    )));
                }
                if template.vendor_category() != category_key {
                    return Err(VendorComplianceError::PersistenceDataCorrupted(format!(
                        "template `{}` category mismatch: key=`{}` payload=`{}`",
                        template_id_key.as_str(),
                        category_key.as_str(),
                        template.vendor_category().as_str()
                    )));
                }
            }
        }

        for (vendor_id_key, vendor) in &snapshot.vendors {
            if vendor.vendor_id() != vendor_id_key {
                return Err(VendorComplianceError::PersistenceDataCorrupted(format!(
                    "vendor key `{}` does not match payload vendor id `{}`",
                    vendor_id_key.as_str(),
                    vendor.vendor_id().as_str()
                )));
            }
        }

        Ok(Self {
            templates_by_category: snapshot.templates_by_category,
            vendors: snapshot.vendors,
            retention_policy,
            audit_trail,
        })
    }

    pub fn upsert_document_template(
        &mut self,
        actor: &AuthenticatedActorContext,
        template: ComplianceDocumentTemplate,
    ) -> Result<(), VendorComplianceError> {
        ensure_role(actor, Role::CommitteeAdmin)?;
        let audit_event = AuditEvidenceWrite::new(
            AuditTimestamp::now_taipei().map_err(VendorComplianceError::AuditTrail)?,
            AuditIdentityLink::from_actor(actor, UPSERT_COMPLIANCE_TEMPLATE_OPERATION_ID),
            AuditAction::UpsertComplianceDocumentTemplate,
            AuditEntityRef::new(
                AuditEntityType::ComplianceDocumentTemplate,
                template.template_id().as_str(),
            )
            .map_err(VendorComplianceError::AuditTrail)?,
            AuditCorrelationId::parse(format!("template:{}", template.template_id().as_str()))
                .map_err(VendorComplianceError::AuditTrail)?,
        );
        let previous_templates = self.templates_by_category.clone();
        let category_entry = self
            .templates_by_category
            .entry(template.vendor_category().clone())
            .or_default();
        category_entry.insert(template.template_id().clone(), template);
        if let Err(error) = self.audit_trail.append(audit_event) {
            self.templates_by_category = previous_templates;
            return Err(VendorComplianceError::AuditTrail(error));
        }
        Ok(())
    }

    pub fn register_vendor_application(
        &mut self,
        actor: &AuthenticatedActorContext,
        vendor_id: VendorId,
        display_name: impl Into<String>,
        category: VendorCategory,
        submitted_on: ComplianceDate,
    ) -> Result<(), VendorComplianceError> {
        ensure_role(actor, Role::VendorOperator)?;
        let audit_event = AuditEvidenceWrite::new(
            AuditTimestamp::from_epoch_day(submitted_on.epoch_day()),
            AuditIdentityLink::from_actor(actor, REGISTER_VENDOR_APPLICATION_OPERATION_ID),
            AuditAction::RegisterVendorApplication,
            AuditEntityRef::new(AuditEntityType::Vendor, vendor_id.as_str())
                .map_err(VendorComplianceError::AuditTrail)?,
            AuditCorrelationId::for_vendor(vendor_id.as_str())
                .map_err(VendorComplianceError::AuditTrail)?,
        );
        let display_name = display_name.into();
        if display_name.trim().is_empty() {
            return Err(VendorComplianceError::InvalidVendorDisplayName);
        }
        if self.vendors.contains_key(&vendor_id) {
            return Err(VendorComplianceError::VendorAlreadyExists(vendor_id));
        }
        if !self.templates_by_category.contains_key(&category) {
            return Err(VendorComplianceError::MissingTemplateConfiguration(
                category,
            ));
        }

        let mut record = VendorComplianceRecord {
            vendor_id: vendor_id.clone(),
            display_name,
            category: category.clone(),
            status: VendorComplianceStatus::PendingReview,
            documents: BTreeMap::new(),
            history: Vec::new(),
            suspension_reason: None,
            rejected_on: None,
            reminder_registry: BTreeSet::new(),
        };
        record.push_history(
            actor,
            submitted_on,
            ComplianceHistoryKind::ApplicationSubmitted { category },
        );
        let previous_vendors = self.vendors.clone();
        self.vendors.insert(vendor_id, record);
        if let Err(error) = self.audit_trail.append(audit_event) {
            self.vendors = previous_vendors;
            return Err(VendorComplianceError::AuditTrail(error));
        }
        Ok(())
    }

    pub fn submit_document(
        &mut self,
        actor: &AuthenticatedActorContext,
        vendor_id: &VendorId,
        template_id: &DocumentTemplateId,
        submission: VendorDocumentSubmission,
    ) -> Result<(), VendorComplianceError> {
        ensure_role(actor, Role::VendorOperator)?;
        let audit_event = AuditEvidenceWrite::new(
            AuditTimestamp::from_epoch_day(submission.submitted_on().epoch_day()),
            AuditIdentityLink::from_actor(actor, SUBMIT_VENDOR_DOCUMENT_OPERATION_ID),
            AuditAction::SubmitVendorComplianceDocument,
            AuditEntityRef::new(AuditEntityType::Vendor, vendor_id.as_str())
                .map_err(VendorComplianceError::AuditTrail)?,
            AuditCorrelationId::for_vendor(vendor_id.as_str())
                .map_err(VendorComplianceError::AuditTrail)?,
        );
        let vendor_category = self
            .vendors
            .get(vendor_id)
            .ok_or_else(|| VendorComplianceError::VendorNotFound(vendor_id.clone()))?
            .category
            .clone();
        let template = self
            .templates_by_category
            .get(&vendor_category)
            .ok_or_else(|| {
                VendorComplianceError::MissingTemplateConfiguration(vendor_category.clone())
            })?
            .get(template_id)
            .ok_or_else(|| VendorComplianceError::TemplateNotConfiguredForCategory {
                template_id: template_id.clone(),
                category: vendor_category,
            })?
            .clone();

        let max_allowed_expiry = submission
            .submitted_on()
            .add_days(i32::from(template.max_validity_days()));
        if submission.expires_on() > max_allowed_expiry {
            return Err(VendorComplianceError::DocumentExpiryExceedsTemplatePolicy {
                template_id: template_id.clone(),
                max_validity_days: template.max_validity_days(),
            });
        }
        let submission_ref = ObjectStorageReference::parse(submission.document_ref())
            .map_err(|_| VendorComplianceError::InvalidDocumentReference)?;
        let (_, key) = submission_ref.split_parts();
        if !object_key_matches_vendor_owner_scope(
            key,
            COMPLIANCE_DOCUMENT_OBJECT_KEY_PREFIX,
            vendor_id.as_str(),
        ) {
            return Err(VendorComplianceError::InvalidDocumentReference);
        }

        let previous_vendors = self.vendors.clone();
        let vendor = self
            .vendors
            .get_mut(vendor_id)
            .ok_or_else(|| VendorComplianceError::VendorNotFound(vendor_id.clone()))?;
        vendor
            .documents
            .insert(template_id.clone(), submission.clone());
        vendor.reminder_registry.retain(|marker| {
            !(marker.template_id == *template_id && marker.expires_on == submission.expires_on())
        });
        vendor.push_history(
            actor,
            submission.submitted_on(),
            ComplianceHistoryKind::DocumentSubmitted {
                template_id: template_id.clone(),
                expires_on: submission.expires_on(),
            },
        );
        if let Err(error) = self.audit_trail.append(audit_event) {
            self.vendors = previous_vendors;
            return Err(VendorComplianceError::AuditTrail(error));
        }
        Ok(())
    }

    pub fn review_application(
        &mut self,
        actor: &AuthenticatedActorContext,
        vendor_id: &VendorId,
        decision: VendorReviewDecision,
        comment: impl Into<String>,
        decided_on: ComplianceDate,
    ) -> Result<VendorComplianceStatus, VendorComplianceError> {
        ensure_role(actor, Role::CommitteeAdmin)?;
        let audit_event = AuditEvidenceWrite::new(
            AuditTimestamp::from_epoch_day(decided_on.epoch_day()),
            AuditIdentityLink::from_actor(actor, REVIEW_VENDOR_APPLICATION_OPERATION_ID),
            AuditAction::ReviewVendorApplication,
            AuditEntityRef::new(AuditEntityType::Vendor, vendor_id.as_str())
                .map_err(VendorComplianceError::AuditTrail)?,
            AuditCorrelationId::for_vendor(vendor_id.as_str())
                .map_err(VendorComplianceError::AuditTrail)?,
        );
        if decision == VendorReviewDecision::Approved {
            let vendor = self
                .vendors
                .get(vendor_id)
                .ok_or_else(|| VendorComplianceError::VendorNotFound(vendor_id.clone()))?;
            let gaps = self.approval_compliance_gaps(vendor, decided_on)?;
            if !gaps.is_empty() {
                return Err(VendorComplianceError::ApprovalBlockedByComplianceGap(gaps));
            }
        }

        let previous_vendors = self.vendors.clone();
        let vendor = self
            .vendors
            .get_mut(vendor_id)
            .ok_or_else(|| VendorComplianceError::VendorNotFound(vendor_id.clone()))?;
        let comment = comment.into();
        if comment.trim().len() < 5 {
            return Err(VendorComplianceError::InvalidReviewComment);
        }

        vendor.status = match decision {
            VendorReviewDecision::Approved => VendorComplianceStatus::Active,
            VendorReviewDecision::Rejected => VendorComplianceStatus::Rejected,
            VendorReviewDecision::RequestFix => VendorComplianceStatus::FixRequested,
        };

        if decision == VendorReviewDecision::Rejected {
            vendor.rejected_on = Some(decided_on);
        } else {
            vendor.rejected_on = None;
        }
        if vendor.status != VendorComplianceStatus::Suspended {
            vendor.suspension_reason = None;
        }
        vendor.push_history(
            actor,
            decided_on,
            ComplianceHistoryKind::ReviewDecision { decision, comment },
        );
        let status = vendor.status;
        if let Err(error) = self.audit_trail.append(audit_event) {
            self.vendors = previous_vendors;
            return Err(VendorComplianceError::AuditTrail(error));
        }
        Ok(status)
    }

    pub fn run_lifecycle(
        &mut self,
        actor: &AuthenticatedActorContext,
        run_on: ComplianceDate,
    ) -> Result<LifecycleRunResult, VendorComplianceError> {
        let telemetry = TelemetryService::ComplianceWorker.begin_operation(
            "runVendorComplianceLifecycle",
            Some(actor.actor_id().as_str()),
            None,
        );
        let result = (|| {
            ensure_role(actor, Role::CommitteeAdmin)?;
            let previous_vendors = self.vendors.clone();
            let audit_event = AuditEvidenceWrite::new(
                AuditTimestamp::from_epoch_day(run_on.epoch_day()),
                AuditIdentityLink::from_actor(actor, RUN_VENDOR_LIFECYCLE_OPERATION_ID),
                AuditAction::RunVendorComplianceLifecycle,
                AuditEntityRef::new(AuditEntityType::Vendor, "all")
                    .map_err(VendorComplianceError::AuditTrail)?,
                AuditCorrelationId::parse("compliance:lifecycle")
                    .map_err(VendorComplianceError::AuditTrail)?,
            );

            let mut run_result = LifecycleRunResult::default();
            for vendor in self.vendors.values_mut() {
                if !matches!(
                    vendor.status,
                    VendorComplianceStatus::Active | VendorComplianceStatus::Suspended
                ) {
                    continue;
                }
                let templates = self
                    .templates_by_category
                    .get(vendor.category())
                    .ok_or_else(|| {
                        VendorComplianceError::MissingTemplateConfiguration(
                            vendor.category().clone(),
                        )
                    })?;
                let evaluation = evaluate_vendor_lifecycle(vendor, templates, run_on);

                for reminder in evaluation.reminders {
                    let marker = ReminderRegistryKey {
                        template_id: reminder.template_id.clone(),
                        expires_on: reminder.expires_on,
                        lead_days: reminder.days_until_expiry,
                    };
                    if !vendor.reminder_registry.insert(marker) {
                        continue;
                    }

                    vendor.push_history(
                        actor,
                        run_on,
                        ComplianceHistoryKind::ExpiryReminderIssued {
                            template_id: reminder.template_id.clone(),
                            expires_on: reminder.expires_on,
                            days_until_expiry: reminder.days_until_expiry,
                        },
                    );
                    run_result.reminders.push(LifecycleExpiryReminder {
                        vendor_id: vendor.vendor_id.clone(),
                        template_id: reminder.template_id,
                        expires_on: reminder.expires_on,
                        days_until_expiry: reminder.days_until_expiry,
                    });
                }

                if let Some(reason) = evaluation.suspension_reason {
                    if vendor.status != VendorComplianceStatus::Suspended
                        || vendor.suspension_reason.as_ref() != Some(&reason)
                    {
                        vendor.status = VendorComplianceStatus::Suspended;
                        vendor.suspension_reason = Some(reason.clone());
                        vendor.push_history(
                            actor,
                            run_on,
                            ComplianceHistoryKind::Suspended {
                                reason: reason.clone(),
                            },
                        );
                        run_result.suspensions.push(LifecycleSuspension {
                            vendor_id: vendor.vendor_id.clone(),
                            reason,
                        });
                    }
                } else if vendor.status == VendorComplianceStatus::Suspended {
                    vendor.status = VendorComplianceStatus::Active;
                    vendor.suspension_reason = None;
                    vendor.push_history(actor, run_on, ComplianceHistoryKind::Reinstated);
                    run_result.reinstatements.push(LifecycleReinstatement {
                        vendor_id: vendor.vendor_id.clone(),
                    });
                }
            }

            if let Err(error) = self.audit_trail.append(audit_event) {
                self.vendors = previous_vendors;
                return Err(VendorComplianceError::AuditTrail(error));
            }
            Ok(run_result)
        })();
        telemetry.finish(if result.is_ok() {
            TelemetryOutcome::Success
        } else {
            TelemetryOutcome::Error
        });
        result
    }

    pub fn prune_history(
        &mut self,
        actor: &AuthenticatedActorContext,
        as_of: ComplianceDate,
    ) -> Result<HistoryPruneResult, VendorComplianceError> {
        ensure_role(actor, Role::CommitteeAdmin)?;
        let previous_vendors = self.vendors.clone();
        let audit_event = AuditEvidenceWrite::new(
            AuditTimestamp::from_epoch_day(as_of.epoch_day()),
            AuditIdentityLink::from_actor(actor, PRUNE_VENDOR_HISTORY_OPERATION_ID),
            AuditAction::PruneVendorComplianceHistory,
            AuditEntityRef::new(AuditEntityType::Vendor, "all")
                .map_err(VendorComplianceError::AuditTrail)?,
            AuditCorrelationId::parse("compliance:retention")
                .map_err(VendorComplianceError::AuditTrail)?,
        );

        let mut result = HistoryPruneResult::default();
        let mut vendors_to_delete = Vec::new();

        for (vendor_id, vendor) in &mut self.vendors {
            let before = vendor.history.len();
            vendor.history.retain(|entry| {
                let retention_days = match entry.kind.retention_bucket() {
                    HistoryRetentionBucket::Review => self.retention_policy.review_history_days(),
                    HistoryRetentionBucket::Lifecycle => {
                        self.retention_policy.lifecycle_history_days()
                    }
                };
                as_of.days_since(entry.occurred_on()) <= i32::from(retention_days)
            });
            result.pruned_history_entries += before.saturating_sub(vendor.history.len());

            if vendor.status == VendorComplianceStatus::Rejected
                && vendor
                    .rejected_on
                    .map(|rejected_on| {
                        as_of.days_since(rejected_on)
                            > i32::from(self.retention_policy.rejected_vendor_deletion_days())
                    })
                    .unwrap_or(false)
            {
                vendors_to_delete.push(vendor_id.clone());
            }
        }

        for vendor_id in vendors_to_delete {
            if self.vendors.remove(&vendor_id).is_some() {
                result.deleted_vendor_records += 1;
            }
        }

        if let Err(error) = self.audit_trail.append(audit_event) {
            self.vendors = previous_vendors;
            return Err(VendorComplianceError::AuditTrail(error));
        }

        Ok(result)
    }

    fn approval_compliance_gaps(
        &self,
        vendor: &VendorComplianceRecord,
        as_of: ComplianceDate,
    ) -> Result<Vec<SuspensionReason>, VendorComplianceError> {
        let templates = self
            .templates_by_category
            .get(vendor.category())
            .ok_or_else(|| {
                VendorComplianceError::MissingTemplateConfiguration(vendor.category().clone())
            })?;

        let mut gaps = Vec::new();
        for template in templates.values().filter(|template| template.required()) {
            match vendor.documents.get(template.template_id()) {
                None => gaps.push(SuspensionReason::MissingRequiredDocument {
                    template_id: template.template_id().clone(),
                }),
                Some(document) => {
                    if as_of > document.expires_on() {
                        gaps.push(SuspensionReason::ExpiredRequiredDocument {
                            template_id: template.template_id().clone(),
                            expired_on: document.expires_on(),
                        });
                    }
                }
            }
        }

        Ok(gaps)
    }
}

#[derive(Debug, Clone, PartialEq, Eq, PartialOrd, Ord, Serialize, Deserialize)]
struct ReminderRegistryKey {
    template_id: DocumentTemplateId,
    expires_on: ComplianceDate,
    lead_days: u16,
}

#[derive(Debug, Clone, PartialEq, Eq)]
struct LifecycleReminderCandidate {
    template_id: DocumentTemplateId,
    expires_on: ComplianceDate,
    days_until_expiry: u16,
}

#[derive(Debug, Clone, PartialEq, Eq, Default)]
struct VendorLifecycleEvaluation {
    reminders: Vec<LifecycleReminderCandidate>,
    suspension_reason: Option<SuspensionReason>,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum HistoryRetentionBucket {
    Review,
    Lifecycle,
}

fn evaluate_vendor_lifecycle(
    vendor: &VendorComplianceRecord,
    templates: &BTreeMap<DocumentTemplateId, ComplianceDocumentTemplate>,
    run_on: ComplianceDate,
) -> VendorLifecycleEvaluation {
    let mut evaluation = VendorLifecycleEvaluation::default();

    for template in templates.values().filter(|template| template.required()) {
        let Some(document) = vendor.documents.get(template.template_id()) else {
            evaluation.suspension_reason = Some(SuspensionReason::MissingRequiredDocument {
                template_id: template.template_id().clone(),
            });
            break;
        };

        let days_until_expiry = run_on.days_until(document.expires_on());
        for reminder_days in template.reminder_days_before_expiry() {
            if days_until_expiry == i32::from(*reminder_days) {
                evaluation.reminders.push(LifecycleReminderCandidate {
                    template_id: template.template_id().clone(),
                    expires_on: document.expires_on(),
                    days_until_expiry: *reminder_days,
                });
            }
        }

        if days_until_expiry < -i32::from(template.suspension_grace_days()) {
            evaluation.suspension_reason = Some(SuspensionReason::ExpiredRequiredDocument {
                template_id: template.template_id().clone(),
                expired_on: document.expires_on(),
            });
            break;
        }
    }

    evaluation
}

fn ensure_role(
    actor: &AuthenticatedActorContext,
    required_role: Role,
) -> Result<(), VendorComplianceError> {
    if actor.role() != required_role {
        return Err(VendorComplianceError::UnauthorizedRole {
            expected: required_role,
            actual: actor.role(),
        });
    }
    Ok(())
}

impl VendorComplianceRecord {
    fn push_history(
        &mut self,
        actor: &AuthenticatedActorContext,
        occurred_on: ComplianceDate,
        kind: ComplianceHistoryKind,
    ) {
        self.history.push(ComplianceHistoryEntry {
            occurred_on,
            actor_id: actor.actor_id().clone(),
            actor_role: actor.role(),
            kind,
        });
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum VendorComplianceError {
    InvalidVendorId,
    InvalidVendorCategory,
    InvalidDocumentTemplateId,
    InvalidTemplateDisplayName,
    InvalidTemplateValidityDays,
    ReminderLeadTimeOutOfRange,
    InvalidDocumentReference,
    InvalidDocumentExpiryWindow,
    InvalidVendorDisplayName,
    InvalidReviewComment,
    InvalidHistoryRetentionPolicy,
    MissingTemplateConfiguration(VendorCategory),
    TemplateNotConfiguredForCategory {
        template_id: DocumentTemplateId,
        category: VendorCategory,
    },
    DocumentExpiryExceedsTemplatePolicy {
        template_id: DocumentTemplateId,
        max_validity_days: u16,
    },
    VendorAlreadyExists(VendorId),
    VendorNotFound(VendorId),
    ApprovalBlockedByComplianceGap(Vec<SuspensionReason>),
    PersistenceDataCorrupted(String),
    UnauthorizedRole {
        expected: Role,
        actual: Role,
    },
    AuditTrail(AuditTrailError),
}

impl fmt::Display for VendorComplianceError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::InvalidVendorId => f.write_str("vendor id must not be empty"),
            Self::InvalidVendorCategory => f.write_str("vendor category must not be empty"),
            Self::InvalidDocumentTemplateId => {
                f.write_str("document template id must not be empty")
            }
            Self::InvalidTemplateDisplayName => {
                f.write_str("template display name must not be empty")
            }
            Self::InvalidTemplateValidityDays => {
                f.write_str("template max validity days must be greater than zero")
            }
            Self::ReminderLeadTimeOutOfRange => {
                f.write_str("reminder lead days cannot exceed template validity days")
            }
            Self::InvalidDocumentReference => {
                f.write_str(
                    "document reference must be a valid s3:// object reference in the managed compliance bucket",
                )
            }
            Self::InvalidDocumentExpiryWindow => {
                f.write_str("document expiry must be after submission date")
            }
            Self::InvalidVendorDisplayName => f.write_str("vendor display name must not be empty"),
            Self::InvalidReviewComment => {
                f.write_str("review comment must contain at least 5 non-whitespace characters")
            }
            Self::InvalidHistoryRetentionPolicy => {
                f.write_str("retention policy values must all be greater than zero")
            }
            Self::MissingTemplateConfiguration(category) => {
                write!(f, "no document templates configured for vendor category {category}")
            }
            Self::TemplateNotConfiguredForCategory {
                template_id,
                category,
            } => write!(
                f,
                "document template {template_id} is not configured for category {category}"
            ),
            Self::DocumentExpiryExceedsTemplatePolicy {
                template_id,
                max_validity_days,
            } => write!(
                f,
                "document for template {template_id} exceeds max validity policy of {max_validity_days} days"
            ),
            Self::VendorAlreadyExists(vendor_id) => {
                write!(f, "vendor {vendor_id} already exists")
            }
            Self::VendorNotFound(vendor_id) => {
                write!(f, "vendor {vendor_id} is not registered")
            }
            Self::ApprovalBlockedByComplianceGap(gaps) => write!(
                f,
                "vendor approval blocked because {} compliance gaps remain",
                gaps.len()
            ),
            Self::PersistenceDataCorrupted(message) => {
                write!(f, "persisted compliance state is corrupted: {message}")
            }
            Self::UnauthorizedRole { expected, actual } => write!(
                f,
                "operation requires role {expected:?}, but actor has role {actual:?}"
            ),
            Self::AuditTrail(error) => write!(f, "audit trail write failed: {error}"),
        }
    }
}

impl std::error::Error for VendorComplianceError {}
