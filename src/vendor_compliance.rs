use std::collections::{BTreeMap, BTreeSet};
use std::fmt;

use crate::identity::{ActorId, AuthenticatedActorContext, Role};

#[derive(Debug, Clone, PartialEq, Eq, PartialOrd, Ord, Hash)]
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

#[derive(Debug, Clone, PartialEq, Eq, PartialOrd, Ord, Hash)]
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

#[derive(Debug, Clone, PartialEq, Eq, PartialOrd, Ord, Hash)]
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

#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Hash)]
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

#[derive(Debug, Clone, PartialEq, Eq)]
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

#[derive(Debug, Clone, PartialEq, Eq)]
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

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum VendorReviewDecision {
    Approved,
    Rejected,
    RequestFix,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum VendorComplianceStatus {
    PendingReview,
    FixRequested,
    Active,
    Rejected,
    Suspended,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum SuspensionReason {
    MissingRequiredDocument {
        template_id: DocumentTemplateId,
    },
    ExpiredRequiredDocument {
        template_id: DocumentTemplateId,
        expired_on: ComplianceDate,
    },
}

#[derive(Debug, Clone, PartialEq, Eq)]
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

#[derive(Debug, Clone, PartialEq, Eq)]
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

#[derive(Debug, Clone, PartialEq, Eq)]
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
}

impl VendorComplianceLifecycle {
    pub fn new(retention_policy: HistoryRetentionPolicy) -> Self {
        Self {
            templates_by_category: BTreeMap::new(),
            vendors: BTreeMap::new(),
            retention_policy,
        }
    }

    pub fn retention_policy(&self) -> &HistoryRetentionPolicy {
        &self.retention_policy
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

    pub fn visible_vendor_ids_for_ordering(&self) -> Vec<&VendorId> {
        self.vendors
            .values()
            .filter(|vendor| vendor.is_visible_for_ordering())
            .map(|vendor| vendor.vendor_id())
            .collect()
    }

    pub fn upsert_document_template(
        &mut self,
        actor: &AuthenticatedActorContext,
        template: ComplianceDocumentTemplate,
    ) -> Result<(), VendorComplianceError> {
        ensure_role(actor, Role::CommitteeAdmin)?;
        let category_entry = self
            .templates_by_category
            .entry(template.vendor_category().clone())
            .or_default();
        category_entry.insert(template.template_id().clone(), template);
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
        self.vendors.insert(vendor_id, record);
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
        Ok(vendor.status)
    }

    pub fn run_lifecycle(
        &mut self,
        actor: &AuthenticatedActorContext,
        run_on: ComplianceDate,
    ) -> Result<LifecycleRunResult, VendorComplianceError> {
        ensure_role(actor, Role::CommitteeAdmin)?;

        let mut result = LifecycleRunResult::default();
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
                    VendorComplianceError::MissingTemplateConfiguration(vendor.category().clone())
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
                result.reminders.push(LifecycleExpiryReminder {
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
                    result.suspensions.push(LifecycleSuspension {
                        vendor_id: vendor.vendor_id.clone(),
                        reason,
                    });
                }
            } else if vendor.status == VendorComplianceStatus::Suspended {
                vendor.status = VendorComplianceStatus::Active;
                vendor.suspension_reason = None;
                vendor.push_history(actor, run_on, ComplianceHistoryKind::Reinstated);
                result.reinstatements.push(LifecycleReinstatement {
                    vendor_id: vendor.vendor_id.clone(),
                });
            }
        }

        Ok(result)
    }

    pub fn prune_history(
        &mut self,
        actor: &AuthenticatedActorContext,
        as_of: ComplianceDate,
    ) -> Result<HistoryPruneResult, VendorComplianceError> {
        ensure_role(actor, Role::CommitteeAdmin)?;

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

#[derive(Debug, Clone, PartialEq, Eq, PartialOrd, Ord)]
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
    UnauthorizedRole {
        expected: Role,
        actual: Role,
    },
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
                f.write_str("document reference must not be empty")
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
            Self::UnauthorizedRole { expected, actual } => write!(
                f,
                "operation requires role {expected:?}, but actor has role {actual:?}"
            ),
        }
    }
}

impl std::error::Error for VendorComplianceError {}
