use std::cmp::Ordering;
use std::collections::BTreeMap;
use std::fmt;
use std::sync::{Arc, Mutex};

use crate::audit::{
    AuditAction, AuditCorrelationId, AuditEntityRef, AuditEntityType, AuditEvidenceWrite,
    AuditIdentityLink, AuditTimestamp, AuditTrailError, ImmutableAuditTrail,
};
use crate::identity::{ActorId, AuthenticatedActorContext, Role};
use crate::vendor_compliance::VendorId;

const UPSERT_ANOMALY_RULE_OPERATION_ID: &str = "upsertAnomalyRule";
const EVALUATE_ANOMALY_ALERTS_OPERATION_ID: &str = "evaluateAnomalyAlerts";
const ASSIGN_ANOMALY_ALERT_OWNER_OPERATION_ID: &str = "assignAnomalyAlertOwner";
const UPDATE_ANOMALY_ALERT_STATUS_OPERATION_ID: &str = "updateAnomalyAlertStatus";
const CLOSE_ANOMALY_ALERT_OPERATION_ID: &str = "closeAnomalyAlert";
const MAX_RULE_TEXT_LENGTH: usize = 280;
const MAX_NOTE_LENGTH: usize = 280;
const MAX_EVIDENCE_REF_LENGTH: usize = 280;
const MAX_TICKET_REFERENCE_LENGTH: usize = 128;

#[derive(Debug, Clone, PartialEq, Eq, PartialOrd, Ord, Hash)]
pub struct AnomalyRuleId(String);

impl AnomalyRuleId {
    pub fn parse(value: impl Into<String>) -> Result<Self, AnomalyAlertError> {
        let value = value.into();
        let Some(suffix) = value.strip_prefix("rule-") else {
            return Err(AnomalyAlertError::InvalidRuleId);
        };
        if !(3..=64).contains(&suffix.len())
            || !suffix.chars().all(|character| {
                character.is_ascii_lowercase() || character.is_ascii_digit() || character == '-'
            })
        {
            return Err(AnomalyAlertError::InvalidRuleId);
        };
        Ok(Self(value))
    }

    pub fn as_str(&self) -> &str {
        &self.0
    }
}

impl fmt::Display for AnomalyRuleId {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(self.as_str())
    }
}

#[derive(Debug, Clone, PartialEq, Eq, PartialOrd, Ord, Hash)]
pub struct AnomalyAlertId(String);

impl AnomalyAlertId {
    pub fn parse(value: impl Into<String>) -> Result<Self, AnomalyAlertError> {
        let value = value.into();
        let trimmed = value.trim();
        if trimmed.is_empty() {
            return Err(AnomalyAlertError::InvalidAlertId);
        }
        Ok(Self(trimmed.to_owned()))
    }

    pub fn as_str(&self) -> &str {
        &self.0
    }
}

impl fmt::Display for AnomalyAlertId {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(self.as_str())
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Hash)]
pub enum AnomalyRuleKind {
    ExpiryRisk,
    OnTimeDegradation,
    SatisfactionDrop,
    ComplaintSpike,
}

impl AnomalyRuleKind {
    pub const fn as_str(self) -> &'static str {
        match self {
            Self::ExpiryRisk => "EXPIRY_RISK",
            Self::OnTimeDegradation => "ON_TIME_DEGRADATION",
            Self::SatisfactionDrop => "SATISFACTION_DROP",
            Self::ComplaintSpike => "COMPLAINT_SPIKE",
        }
    }

    pub fn parse(value: &str) -> Option<Self> {
        match value.trim().to_ascii_uppercase().as_str() {
            "EXPIRY_RISK" => Some(Self::ExpiryRisk),
            "ON_TIME_DEGRADATION" => Some(Self::OnTimeDegradation),
            "SATISFACTION_DROP" => Some(Self::SatisfactionDrop),
            "COMPLAINT_SPIKE" => Some(Self::ComplaintSpike),
            _ => None,
        }
    }
}

impl fmt::Display for AnomalyRuleKind {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(self.as_str())
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum AnomalyThresholdComparator {
    LessThan,
    LessThanOrEqual,
    GreaterThan,
    GreaterThanOrEqual,
}

impl AnomalyThresholdComparator {
    pub const fn as_str(self) -> &'static str {
        match self {
            Self::LessThan => "LT",
            Self::LessThanOrEqual => "LTE",
            Self::GreaterThan => "GT",
            Self::GreaterThanOrEqual => "GTE",
        }
    }

    pub fn parse(value: &str) -> Option<Self> {
        match value.trim().to_ascii_uppercase().as_str() {
            "LT" => Some(Self::LessThan),
            "LTE" => Some(Self::LessThanOrEqual),
            "GT" => Some(Self::GreaterThan),
            "GTE" => Some(Self::GreaterThanOrEqual),
            _ => None,
        }
    }

    fn is_triggered(self, observed: f64, threshold: f64) -> bool {
        match self {
            Self::LessThan => observed < threshold,
            Self::LessThanOrEqual => observed <= threshold,
            Self::GreaterThan => observed > threshold,
            Self::GreaterThanOrEqual => observed >= threshold,
        }
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum AnomalyAlertSeverity {
    Warning,
    Critical,
}

impl AnomalyAlertSeverity {
    pub const fn as_str(self) -> &'static str {
        match self {
            Self::Warning => "WARNING",
            Self::Critical => "CRITICAL",
        }
    }

    pub fn parse(value: &str) -> Option<Self> {
        match value.trim().to_ascii_uppercase().as_str() {
            "WARNING" => Some(Self::Warning),
            "CRITICAL" => Some(Self::Critical),
            _ => None,
        }
    }
}

#[derive(Debug, Clone, PartialEq)]
pub struct AnomalyRule {
    rule_id: AnomalyRuleId,
    kind: AnomalyRuleKind,
    display_name: String,
    description: String,
    governance_issue_id: String,
    enabled: bool,
    threshold_value: f64,
    threshold_comparator: AnomalyThresholdComparator,
    evaluation_window_days: u16,
    sla_minutes: u32,
    severity: AnomalyAlertSeverity,
}

impl AnomalyRule {
    #[allow(clippy::too_many_arguments)]
    pub fn new(
        rule_id: AnomalyRuleId,
        kind: AnomalyRuleKind,
        display_name: impl Into<String>,
        description: impl Into<String>,
        governance_issue_id: impl Into<String>,
        enabled: bool,
        threshold_value: f64,
        threshold_comparator: AnomalyThresholdComparator,
        evaluation_window_days: u16,
        sla_minutes: u32,
        severity: AnomalyAlertSeverity,
    ) -> Result<Self, AnomalyAlertError> {
        let display_name = normalize_rule_text(display_name.into(), "displayName")?;
        let description = normalize_rule_text(description.into(), "description")?;
        let governance_issue_id =
            normalize_rule_text(governance_issue_id.into(), "governanceIssueId")?;

        if !threshold_value.is_finite() || threshold_value < 0.0 {
            return Err(AnomalyAlertError::InvalidThresholdValue {
                rule_id: rule_id.clone(),
                threshold_value,
            });
        }
        if evaluation_window_days == 0 {
            return Err(AnomalyAlertError::InvalidEvaluationWindowDays {
                rule_id: rule_id.clone(),
                evaluation_window_days,
            });
        }
        if sla_minutes == 0 {
            return Err(AnomalyAlertError::InvalidSlaMinutes {
                rule_id: rule_id.clone(),
                sla_minutes,
            });
        }

        validate_kind_threshold(kind, threshold_value, threshold_comparator, &rule_id)?;

        Ok(Self {
            rule_id,
            kind,
            display_name,
            description,
            governance_issue_id,
            enabled,
            threshold_value,
            threshold_comparator,
            evaluation_window_days,
            sla_minutes,
            severity,
        })
    }

    pub fn default_governance_rules() -> Vec<Self> {
        vec![
            Self::new(
                AnomalyRuleId::parse("rule-expiry-risk").expect("static rule id should parse"),
                AnomalyRuleKind::ExpiryRisk,
                "Compliance Expiry Risk",
                "Trigger when nearest required compliance document expiry is inside governance lead time.",
                "issue-anomaly-governance-playbook",
                true,
                14.0,
                AnomalyThresholdComparator::LessThanOrEqual,
                30,
                24 * 60,
                AnomalyAlertSeverity::Critical,
            )
            .expect("static rule should be valid"),
            Self::new(
                AnomalyRuleId::parse("rule-on-time-degradation")
                    .expect("static rule id should parse"),
                AnomalyRuleKind::OnTimeDegradation,
                "On-Time Delivery Degradation",
                "Trigger when observed on-time delivery ratio drops below baseline.",
                "issue-anomaly-governance-playbook",
                true,
                0.92,
                AnomalyThresholdComparator::LessThan,
                7,
                12 * 60,
                AnomalyAlertSeverity::Critical,
            )
            .expect("static rule should be valid"),
            Self::new(
                AnomalyRuleId::parse("rule-satisfaction-drop")
                    .expect("static rule id should parse"),
                AnomalyRuleKind::SatisfactionDrop,
                "Satisfaction Drop",
                "Trigger when satisfaction signal falls below accepted floor.",
                "issue-anomaly-governance-playbook",
                true,
                4.0,
                AnomalyThresholdComparator::LessThan,
                7,
                24 * 60,
                AnomalyAlertSeverity::Warning,
            )
            .expect("static rule should be valid"),
            Self::new(
                AnomalyRuleId::parse("rule-complaint-spike")
                    .expect("static rule id should parse"),
                AnomalyRuleKind::ComplaintSpike,
                "Complaint Spike",
                "Trigger when complaint count exceeds governance threshold.",
                "issue-anomaly-governance-playbook",
                true,
                5.0,
                AnomalyThresholdComparator::GreaterThanOrEqual,
                7,
                8 * 60,
                AnomalyAlertSeverity::Critical,
            )
            .expect("static rule should be valid"),
        ]
    }

    pub fn rule_id(&self) -> &AnomalyRuleId {
        &self.rule_id
    }

    pub fn kind(&self) -> AnomalyRuleKind {
        self.kind
    }

    pub fn display_name(&self) -> &str {
        &self.display_name
    }

    pub fn description(&self) -> &str {
        &self.description
    }

    pub fn governance_issue_id(&self) -> &str {
        &self.governance_issue_id
    }

    pub fn enabled(&self) -> bool {
        self.enabled
    }

    pub fn threshold_value(&self) -> f64 {
        self.threshold_value
    }

    pub fn threshold_comparator(&self) -> AnomalyThresholdComparator {
        self.threshold_comparator
    }

    pub fn evaluation_window_days(&self) -> u16 {
        self.evaluation_window_days
    }

    pub fn sla_minutes(&self) -> u32 {
        self.sla_minutes
    }

    pub fn severity(&self) -> AnomalyAlertSeverity {
        self.severity
    }
}

fn validate_kind_threshold(
    kind: AnomalyRuleKind,
    threshold_value: f64,
    comparator: AnomalyThresholdComparator,
    rule_id: &AnomalyRuleId,
) -> Result<(), AnomalyAlertError> {
    match kind {
        AnomalyRuleKind::ExpiryRisk => {
            if !matches!(
                comparator,
                AnomalyThresholdComparator::LessThan | AnomalyThresholdComparator::LessThanOrEqual
            ) {
                return Err(AnomalyAlertError::InvalidRuleComparator {
                    rule_id: rule_id.clone(),
                    kind,
                    comparator,
                });
            }
        }
        AnomalyRuleKind::OnTimeDegradation => {
            if !(0.0..=1.0).contains(&threshold_value) {
                return Err(AnomalyAlertError::InvalidThresholdValue {
                    rule_id: rule_id.clone(),
                    threshold_value,
                });
            }
            if !matches!(
                comparator,
                AnomalyThresholdComparator::LessThan | AnomalyThresholdComparator::LessThanOrEqual
            ) {
                return Err(AnomalyAlertError::InvalidRuleComparator {
                    rule_id: rule_id.clone(),
                    kind,
                    comparator,
                });
            }
        }
        AnomalyRuleKind::SatisfactionDrop => {
            if !(0.0..=5.0).contains(&threshold_value) {
                return Err(AnomalyAlertError::InvalidThresholdValue {
                    rule_id: rule_id.clone(),
                    threshold_value,
                });
            }
            if !matches!(
                comparator,
                AnomalyThresholdComparator::LessThan | AnomalyThresholdComparator::LessThanOrEqual
            ) {
                return Err(AnomalyAlertError::InvalidRuleComparator {
                    rule_id: rule_id.clone(),
                    kind,
                    comparator,
                });
            }
        }
        AnomalyRuleKind::ComplaintSpike => {
            if !matches!(
                comparator,
                AnomalyThresholdComparator::GreaterThan
                    | AnomalyThresholdComparator::GreaterThanOrEqual
            ) {
                return Err(AnomalyAlertError::InvalidRuleComparator {
                    rule_id: rule_id.clone(),
                    kind,
                    comparator,
                });
            }
        }
    }
    Ok(())
}

fn normalize_rule_text(value: String, field: &str) -> Result<String, AnomalyAlertError> {
    let trimmed = value.trim();
    if trimmed.is_empty() || trimmed.chars().count() > MAX_RULE_TEXT_LENGTH {
        return Err(AnomalyAlertError::InvalidRuleText {
            field: field.to_owned(),
        });
    }
    Ok(trimmed.to_owned())
}

#[derive(Debug, Clone, PartialEq)]
pub struct AnomalySignalSnapshot {
    vendor_id: VendorId,
    observed_at: AuditTimestamp,
    days_until_expiry: Option<f64>,
    on_time_rate: Option<f64>,
    satisfaction_score: Option<f64>,
    complaint_count: Option<f64>,
}

impl AnomalySignalSnapshot {
    pub fn new(vendor_id: VendorId, observed_at: AuditTimestamp) -> Self {
        Self {
            vendor_id,
            observed_at,
            days_until_expiry: None,
            on_time_rate: None,
            satisfaction_score: None,
            complaint_count: None,
        }
    }

    pub fn with_days_until_expiry(mut self, value: Option<f64>) -> Self {
        self.days_until_expiry = value;
        self
    }

    pub fn with_on_time_rate(mut self, value: Option<f64>) -> Self {
        self.on_time_rate = value;
        self
    }

    pub fn with_satisfaction_score(mut self, value: Option<f64>) -> Self {
        self.satisfaction_score = value;
        self
    }

    pub fn with_complaint_count(mut self, value: Option<f64>) -> Self {
        self.complaint_count = value;
        self
    }

    pub fn vendor_id(&self) -> &VendorId {
        &self.vendor_id
    }

    pub fn observed_at(&self) -> AuditTimestamp {
        self.observed_at
    }

    fn metric_for_rule(&self, kind: AnomalyRuleKind) -> Option<f64> {
        match kind {
            AnomalyRuleKind::ExpiryRisk => self.days_until_expiry,
            AnomalyRuleKind::OnTimeDegradation => self.on_time_rate,
            AnomalyRuleKind::SatisfactionDrop => self.satisfaction_score,
            AnomalyRuleKind::ComplaintSpike => self.complaint_count,
        }
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum AnomalyAlertStatus {
    Open,
    Acknowledged,
    RemediationInProgress,
    Escalated,
    Closed,
}

impl AnomalyAlertStatus {
    pub const fn as_str(self) -> &'static str {
        match self {
            Self::Open => "OPEN",
            Self::Acknowledged => "ACKNOWLEDGED",
            Self::RemediationInProgress => "REMEDIATION_IN_PROGRESS",
            Self::Escalated => "ESCALATED",
            Self::Closed => "CLOSED",
        }
    }

    pub fn parse(value: &str) -> Option<Self> {
        match value.trim().to_ascii_uppercase().as_str() {
            "OPEN" => Some(Self::Open),
            "ACKNOWLEDGED" => Some(Self::Acknowledged),
            "REMEDIATION_IN_PROGRESS" => Some(Self::RemediationInProgress),
            "ESCALATED" => Some(Self::Escalated),
            "CLOSED" => Some(Self::Closed),
            _ => None,
        }
    }

    fn is_terminal(self) -> bool {
        matches!(self, Self::Closed)
    }
}

impl fmt::Display for AnomalyAlertStatus {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(self.as_str())
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum AnomalySlaStatus {
    OnTrack,
    Breached,
}

impl AnomalySlaStatus {
    pub const fn as_str(self) -> &'static str {
        match self {
            Self::OnTrack => "ON_TRACK",
            Self::Breached => "BREACHED",
        }
    }

    pub fn parse(value: &str) -> Option<Self> {
        match value.trim().to_ascii_uppercase().as_str() {
            "ON_TRACK" => Some(Self::OnTrack),
            "BREACHED" => Some(Self::Breached),
            _ => None,
        }
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum AnomalyAlertTransition {
    Acknowledge,
    StartRemediation,
    Escalate,
    Close,
}

impl AnomalyAlertTransition {
    pub const fn as_str(self) -> &'static str {
        match self {
            Self::Acknowledge => "ACKNOWLEDGE",
            Self::StartRemediation => "START_REMEDIATION",
            Self::Escalate => "ESCALATE",
            Self::Close => "CLOSE",
        }
    }

    pub fn parse(value: &str) -> Option<Self> {
        match value.trim().to_ascii_uppercase().as_str() {
            "ACKNOWLEDGE" => Some(Self::Acknowledge),
            "START_REMEDIATION" => Some(Self::StartRemediation),
            "ESCALATE" => Some(Self::Escalate),
            "CLOSE" => Some(Self::Close),
            _ => None,
        }
    }

    fn target_status(self) -> AnomalyAlertStatus {
        match self {
            Self::Acknowledge => AnomalyAlertStatus::Acknowledged,
            Self::StartRemediation => AnomalyAlertStatus::RemediationInProgress,
            Self::Escalate => AnomalyAlertStatus::Escalated,
            Self::Close => AnomalyAlertStatus::Closed,
        }
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum AnomalyAlertTraceEventType {
    Triggered,
    OwnerAssigned,
    StatusTransitioned,
    Closed,
}

impl AnomalyAlertTraceEventType {
    pub const fn as_str(self) -> &'static str {
        match self {
            Self::Triggered => "TRIGGERED",
            Self::OwnerAssigned => "OWNER_ASSIGNED",
            Self::StatusTransitioned => "STATUS_TRANSITIONED",
            Self::Closed => "CLOSED",
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AnomalyAlertTraceEvent {
    occurred_at: AuditTimestamp,
    actor_id: ActorId,
    event_type: AnomalyAlertTraceEventType,
    status: AnomalyAlertStatus,
    note: Option<String>,
}

impl AnomalyAlertTraceEvent {
    pub fn occurred_at(&self) -> AuditTimestamp {
        self.occurred_at
    }

    pub fn actor_id(&self) -> &ActorId {
        &self.actor_id
    }

    pub fn event_type(&self) -> AnomalyAlertTraceEventType {
        self.event_type
    }

    pub fn status(&self) -> AnomalyAlertStatus {
        self.status
    }

    pub fn note(&self) -> Option<&str> {
        self.note.as_deref()
    }
}

#[derive(Debug, Clone, PartialEq)]
pub struct AnomalyAlertRecord {
    alert_id: AnomalyAlertId,
    vendor_id: VendorId,
    rule_id: AnomalyRuleId,
    rule_kind: AnomalyRuleKind,
    rule_display_name: String,
    governance_issue_id: String,
    status: AnomalyAlertStatus,
    owner_actor_id: ActorId,
    severity: AnomalyAlertSeverity,
    observed_value: f64,
    threshold_value: f64,
    threshold_comparator: AnomalyThresholdComparator,
    observed_at: AuditTimestamp,
    opened_at: AuditTimestamp,
    updated_at: AuditTimestamp,
    sla_due_at: AuditTimestamp,
    escalated_at: Option<AuditTimestamp>,
    closed_at: Option<AuditTimestamp>,
    closure_note: Option<String>,
    closure_evidence_refs: Vec<String>,
    ticket_reference: Option<String>,
    trace: Vec<AnomalyAlertTraceEvent>,
}

impl AnomalyAlertRecord {
    pub fn alert_id(&self) -> &AnomalyAlertId {
        &self.alert_id
    }

    pub fn vendor_id(&self) -> &VendorId {
        &self.vendor_id
    }

    pub fn rule_id(&self) -> &AnomalyRuleId {
        &self.rule_id
    }

    pub fn rule_kind(&self) -> AnomalyRuleKind {
        self.rule_kind
    }

    pub fn rule_display_name(&self) -> &str {
        &self.rule_display_name
    }

    pub fn governance_issue_id(&self) -> &str {
        &self.governance_issue_id
    }

    pub fn status(&self) -> AnomalyAlertStatus {
        self.status
    }

    pub fn owner_actor_id(&self) -> &ActorId {
        &self.owner_actor_id
    }

    pub fn severity(&self) -> AnomalyAlertSeverity {
        self.severity
    }

    pub fn observed_value(&self) -> f64 {
        self.observed_value
    }

    pub fn threshold_value(&self) -> f64 {
        self.threshold_value
    }

    pub fn threshold_comparator(&self) -> AnomalyThresholdComparator {
        self.threshold_comparator
    }

    pub fn observed_at(&self) -> AuditTimestamp {
        self.observed_at
    }

    pub fn opened_at(&self) -> AuditTimestamp {
        self.opened_at
    }

    pub fn updated_at(&self) -> AuditTimestamp {
        self.updated_at
    }

    pub fn sla_due_at(&self) -> AuditTimestamp {
        self.sla_due_at
    }

    pub fn escalated_at(&self) -> Option<AuditTimestamp> {
        self.escalated_at
    }

    pub fn closed_at(&self) -> Option<AuditTimestamp> {
        self.closed_at
    }

    pub fn closure_note(&self) -> Option<&str> {
        self.closure_note.as_deref()
    }

    pub fn closure_evidence_refs(&self) -> &[String] {
        &self.closure_evidence_refs
    }

    pub fn ticket_reference(&self) -> Option<&str> {
        self.ticket_reference.as_deref()
    }

    pub fn trace(&self) -> &[AnomalyAlertTraceEvent] {
        &self.trace
    }

    pub fn sla_status(&self, as_of: AuditTimestamp) -> AnomalySlaStatus {
        let comparison_point = self.closed_at.unwrap_or(as_of);
        if compare_timestamps(comparison_point, self.sla_due_at).is_gt() {
            AnomalySlaStatus::Breached
        } else {
            AnomalySlaStatus::OnTrack
        }
    }

    pub fn is_escalated(&self) -> bool {
        self.escalated_at.is_some() || self.status == AnomalyAlertStatus::Escalated
    }
}

#[derive(Debug, Clone, PartialEq, Eq, Default)]
pub struct AnomalyAlertQuery {
    pub vendor_id: Option<VendorId>,
    pub owner_actor_id: Option<ActorId>,
    pub status: Option<AnomalyAlertStatus>,
    pub escalated_only: Option<bool>,
    pub sla_status: Option<AnomalySlaStatus>,
}

#[derive(Debug, Clone, PartialEq)]
pub struct AnomalyAlertEvaluationResult {
    triggered_alerts: Vec<AnomalyAlertRecord>,
}

impl AnomalyAlertEvaluationResult {
    pub fn triggered_alerts(&self) -> &[AnomalyAlertRecord] {
        &self.triggered_alerts
    }
}
#[derive(Debug, Clone)]
pub struct AnomalyAlertWorkflow {
    state: Arc<Mutex<AnomalyAlertState>>,
    audit_trail: ImmutableAuditTrail,
}

#[derive(Debug, Clone)]
struct AnomalyAlertState {
    next_alert_sequence: u64,
    rules: BTreeMap<AnomalyRuleId, AnomalyRule>,
    alerts: BTreeMap<AnomalyAlertId, AnomalyAlertRecord>,
    open_alert_index: BTreeMap<(VendorId, AnomalyRuleId), AnomalyAlertId>,
}

impl AnomalyAlertWorkflow {
    pub fn with_default_rules(audit_trail: ImmutableAuditTrail) -> Self {
        let rules = AnomalyRule::default_governance_rules()
            .into_iter()
            .map(|rule| (rule.rule_id().clone(), rule))
            .collect::<BTreeMap<_, _>>();
        Self {
            state: Arc::new(Mutex::new(AnomalyAlertState {
                next_alert_sequence: 0,
                rules,
                alerts: BTreeMap::new(),
                open_alert_index: BTreeMap::new(),
            })),
            audit_trail,
        }
    }

    pub fn list_rules(&self) -> Result<Vec<AnomalyRule>, AnomalyAlertError> {
        let state = lock_state(&self.state)?;
        Ok(state.rules.values().cloned().collect())
    }

    pub fn upsert_rule(
        &self,
        actor: &AuthenticatedActorContext,
        rule: AnomalyRule,
        occurred_at: AuditTimestamp,
    ) -> Result<AnomalyRule, AnomalyAlertError> {
        ensure_committee_role(actor)?;

        let mut state = lock_state(&self.state)?;
        let previous_state = state.clone();
        state.rules.insert(rule.rule_id().clone(), rule.clone());

        if let Err(error) = self.append_audit_event(
            actor,
            UPSERT_ANOMALY_RULE_OPERATION_ID,
            AuditAction::UpsertAnomalyDetectionRule,
            AuditEntityType::AnomalyRule,
            rule.rule_id().as_str().to_owned(),
            format!(
                "upsert anomaly rule kind={} threshold={} comparator={} enabled={} windowDays={} slaMinutes={}",
                rule.kind().as_str(),
                rule.threshold_value(),
                rule.threshold_comparator().as_str(),
                rule.enabled(),
                rule.evaluation_window_days(),
                rule.sla_minutes(),
            ),
            AuditCorrelationId::parse(format!("anomaly-rule:{}", rule.rule_id().as_str()))
                .map_err(AnomalyAlertError::AuditTrail)?,
            occurred_at,
        ) {
            *state = previous_state;
            return Err(error);
        }

        Ok(rule)
    }

    pub fn evaluate_rules(
        &self,
        actor: &AuthenticatedActorContext,
        snapshot: AnomalySignalSnapshot,
        default_owner_actor_id: &ActorId,
    ) -> Result<AnomalyAlertEvaluationResult, AnomalyAlertError> {
        ensure_committee_role(actor)?;

        let mut state = lock_state(&self.state)?;
        let previous_state = state.clone();
        let active_rules = state
            .rules
            .values()
            .filter(|rule| rule.enabled())
            .cloned()
            .collect::<Vec<_>>();

        let mut triggered_alerts = Vec::new();
        for rule in active_rules {
            let Some(observed_value) = snapshot.metric_for_rule(rule.kind()) else {
                continue;
            };
            if !rule
                .threshold_comparator()
                .is_triggered(observed_value, rule.threshold_value())
            {
                continue;
            }

            let dedupe_key = (snapshot.vendor_id().clone(), rule.rule_id().clone());
            if let Some(existing_alert_id) = state.open_alert_index.get(&dedupe_key) {
                if let Some(existing_alert) = state.alerts.get(existing_alert_id).cloned() {
                    triggered_alerts.push(existing_alert);
                    continue;
                }
            }

            state.next_alert_sequence = state
                .next_alert_sequence
                .checked_add(1)
                .ok_or(AnomalyAlertError::AlertSequenceOverflow)?;
            let alert_id =
                AnomalyAlertId::parse(format!("alt-{:016x}", state.next_alert_sequence))?;
            let opened_at = snapshot.observed_at();
            let alert = AnomalyAlertRecord {
                alert_id: alert_id.clone(),
                vendor_id: snapshot.vendor_id().clone(),
                rule_id: rule.rule_id().clone(),
                rule_kind: rule.kind(),
                rule_display_name: rule.display_name().to_owned(),
                governance_issue_id: rule.governance_issue_id().to_owned(),
                status: AnomalyAlertStatus::Open,
                owner_actor_id: default_owner_actor_id.clone(),
                severity: rule.severity(),
                observed_value,
                threshold_value: rule.threshold_value(),
                threshold_comparator: rule.threshold_comparator(),
                observed_at: opened_at,
                opened_at,
                updated_at: opened_at,
                sla_due_at: add_minutes(opened_at, rule.sla_minutes())?,
                escalated_at: None,
                closed_at: None,
                closure_note: None,
                closure_evidence_refs: Vec::new(),
                ticket_reference: None,
                trace: vec![AnomalyAlertTraceEvent {
                    occurred_at: opened_at,
                    actor_id: actor.actor_id().clone(),
                    event_type: AnomalyAlertTraceEventType::Triggered,
                    status: AnomalyAlertStatus::Open,
                    note: Some(format!(
                        "rule={} observed={} threshold={} comparator={}",
                        rule.rule_id().as_str(),
                        observed_value,
                        rule.threshold_value(),
                        rule.threshold_comparator().as_str(),
                    )),
                }],
            };

            state.open_alert_index.insert(dedupe_key, alert_id.clone());
            state.alerts.insert(alert_id.clone(), alert.clone());

            if let Err(error) = self.append_audit_event(
                actor,
                EVALUATE_ANOMALY_ALERTS_OPERATION_ID,
                AuditAction::TriggerAnomalyAlert,
                AuditEntityType::AnomalyAlert,
                alert_id.as_str().to_owned(),
                format!(
                    "trigger anomaly alert vendor={} rule={} observed={} threshold={} comparator={} owner={}",
                    snapshot.vendor_id().as_str(),
                    rule.rule_id().as_str(),
                    observed_value,
                    rule.threshold_value(),
                    rule.threshold_comparator().as_str(),
                    default_owner_actor_id.as_str(),
                ),
                AuditCorrelationId::parse(format!(
                    "anomaly-alert:{}:{}",
                    snapshot.vendor_id().as_str(),
                    rule.rule_id().as_str()
                ))
                .map_err(AnomalyAlertError::AuditTrail)?,
                opened_at,
            ) {
                *state = previous_state;
                return Err(error);
            }

            triggered_alerts.push(alert);
        }

        Ok(AnomalyAlertEvaluationResult { triggered_alerts })
    }

    pub fn assign_owner(
        &self,
        actor: &AuthenticatedActorContext,
        alert_id: &AnomalyAlertId,
        owner_actor_id: &ActorId,
        occurred_at: AuditTimestamp,
        note: Option<String>,
    ) -> Result<AnomalyAlertRecord, AnomalyAlertError> {
        ensure_committee_role(actor)?;

        let mut state = lock_state(&self.state)?;
        let previous_state = state.clone();
        let alert = state
            .alerts
            .get_mut(alert_id)
            .ok_or_else(|| AnomalyAlertError::AlertNotFound(alert_id.clone()))?;
        if alert.status().is_terminal() {
            return Err(AnomalyAlertError::AlertAlreadyClosed {
                alert_id: alert_id.clone(),
            });
        }

        let normalized_note = normalize_optional_note(note)?;
        alert.owner_actor_id = owner_actor_id.clone();
        alert.updated_at = occurred_at;
        alert.trace.push(AnomalyAlertTraceEvent {
            occurred_at,
            actor_id: actor.actor_id().clone(),
            event_type: AnomalyAlertTraceEventType::OwnerAssigned,
            status: alert.status(),
            note: normalized_note.clone(),
        });

        if let Err(error) = self.append_audit_event(
            actor,
            ASSIGN_ANOMALY_ALERT_OWNER_OPERATION_ID,
            AuditAction::AssignAnomalyAlertOwner,
            AuditEntityType::AnomalyAlert,
            alert_id.as_str().to_owned(),
            format!(
                "assign anomaly alert owner={}{}",
                owner_actor_id.as_str(),
                normalized_note
                    .as_ref()
                    .map(|value| format!(" note={value}"))
                    .unwrap_or_default(),
            ),
            AuditCorrelationId::parse(format!("anomaly-alert:{}", alert_id.as_str()))
                .map_err(AnomalyAlertError::AuditTrail)?,
            occurred_at,
        ) {
            *state = previous_state;
            return Err(error);
        }

        Ok(alert.clone())
    }

    pub fn transition_alert(
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
        ensure_committee_role(actor)?;

        let mut state = lock_state(&self.state)?;
        let previous_state = state.clone();
        let mut close_dedupe_key: Option<(VendorId, AnomalyRuleId)> = None;
        let normalized_note = normalize_optional_note(note)?;
        let mut normalized_ticket_reference =
            normalize_optional_ticket_reference(ticket_reference)?;

        let updated_alert = {
            let alert = state
                .alerts
                .get_mut(alert_id)
                .ok_or_else(|| AnomalyAlertError::AlertNotFound(alert_id.clone()))?;

            if alert.status().is_terminal() {
                return Err(AnomalyAlertError::AlertAlreadyClosed {
                    alert_id: alert_id.clone(),
                });
            }

            let to_status = transition.target_status();
            if !is_valid_status_transition(alert.status(), to_status) {
                return Err(AnomalyAlertError::InvalidStatusTransition {
                    alert_id: alert_id.clone(),
                    from_status: alert.status(),
                    to_status,
                });
            }

            alert.status = to_status;
            alert.updated_at = occurred_at;

            let event_type = if transition == AnomalyAlertTransition::Close {
                AnomalyAlertTraceEventType::Closed
            } else {
                AnomalyAlertTraceEventType::StatusTransitioned
            };

            match transition {
                AnomalyAlertTransition::Escalate => {
                    if alert.escalated_at.is_none() {
                        alert.escalated_at = Some(occurred_at);
                    }
                }
                AnomalyAlertTransition::Close => {
                    let required_note = normalize_required_note(closure_note)?;
                    let required_evidence = normalize_closure_evidence(closure_evidence_refs)?;
                    alert.closed_at = Some(occurred_at);
                    alert.closure_note = Some(required_note);
                    alert.closure_evidence_refs = required_evidence;
                    alert.ticket_reference = normalized_ticket_reference.take();
                    close_dedupe_key = Some((alert.vendor_id().clone(), alert.rule_id().clone()));
                }
                AnomalyAlertTransition::Acknowledge | AnomalyAlertTransition::StartRemediation => {}
            }

            alert.trace.push(AnomalyAlertTraceEvent {
                occurred_at,
                actor_id: actor.actor_id().clone(),
                event_type,
                status: to_status,
                note: normalized_note.clone(),
            });

            alert.clone()
        };

        if let Some(dedupe_key) = close_dedupe_key {
            state.open_alert_index.remove(&dedupe_key);
        }

        let (operation_id, action) = if transition == AnomalyAlertTransition::Close {
            (
                CLOSE_ANOMALY_ALERT_OPERATION_ID,
                AuditAction::CloseAnomalyAlert,
            )
        } else {
            (
                UPDATE_ANOMALY_ALERT_STATUS_OPERATION_ID,
                AuditAction::AdvanceAnomalyAlertStatus,
            )
        };

        let reason = if transition == AnomalyAlertTransition::Close {
            format!(
                "close anomaly alert note={} evidenceCount={}{}",
                updated_alert
                    .closure_note()
                    .expect("closure note should be present after close"),
                updated_alert.closure_evidence_refs().len(),
                updated_alert
                    .ticket_reference()
                    .map(|value| format!(" ticketReference={value}"))
                    .unwrap_or_default(),
            )
        } else {
            format!(
                "transition anomaly alert toStatus={}{}",
                updated_alert.status().as_str(),
                normalized_note
                    .as_ref()
                    .map(|value| format!(" note={value}"))
                    .unwrap_or_default(),
            )
        };

        if let Err(error) = self.append_audit_event(
            actor,
            operation_id,
            action,
            AuditEntityType::AnomalyAlert,
            alert_id.as_str().to_owned(),
            reason,
            AuditCorrelationId::parse(format!("anomaly-alert:{}", alert_id.as_str()))
                .map_err(AnomalyAlertError::AuditTrail)?,
            occurred_at,
        ) {
            *state = previous_state;
            return Err(error);
        }

        Ok(updated_alert)
    }

    pub fn query_alerts(
        &self,
        query: &AnomalyAlertQuery,
        as_of: AuditTimestamp,
    ) -> Result<Vec<AnomalyAlertRecord>, AnomalyAlertError> {
        let state = lock_state(&self.state)?;
        let mut alerts = state
            .alerts
            .values()
            .filter(|alert| {
                query
                    .vendor_id
                    .as_ref()
                    .map(|vendor_id| alert.vendor_id() == vendor_id)
                    .unwrap_or(true)
                    && query
                        .owner_actor_id
                        .as_ref()
                        .map(|owner_actor_id| alert.owner_actor_id() == owner_actor_id)
                        .unwrap_or(true)
                    && query
                        .status
                        .map(|status| alert.status() == status)
                        .unwrap_or(true)
                    && query
                        .escalated_only
                        .map(|required| {
                            if required {
                                alert.is_escalated()
                            } else {
                                !alert.is_escalated()
                            }
                        })
                        .unwrap_or(true)
                    && query
                        .sla_status
                        .map(|status| alert.sla_status(as_of) == status)
                        .unwrap_or(true)
            })
            .cloned()
            .collect::<Vec<_>>();

        alerts.sort_by(|left, right| {
            compare_timestamps(right.opened_at(), left.opened_at())
                .then_with(|| left.alert_id().as_str().cmp(right.alert_id().as_str()))
        });

        Ok(alerts)
    }

    pub fn audit_trail(&self) -> ImmutableAuditTrail {
        self.audit_trail.clone()
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
    ) -> Result<(), AnomalyAlertError> {
        let evidence = AuditEvidenceWrite::new_with_reason(
            occurred_at,
            AuditIdentityLink::from_actor(actor, operation_id),
            action,
            AuditEntityRef::new(entity_type, entity_id).map_err(AnomalyAlertError::AuditTrail)?,
            reason,
            correlation_id,
        )
        .map_err(AnomalyAlertError::AuditTrail)?;

        self.audit_trail
            .append(evidence)
            .map(|_| ())
            .map_err(AnomalyAlertError::AuditTrail)
    }
}

fn ensure_committee_role(actor: &AuthenticatedActorContext) -> Result<(), AnomalyAlertError> {
    if actor.role() != Role::CommitteeAdmin {
        return Err(AnomalyAlertError::UnauthorizedRole {
            actual: actor.role(),
        });
    }
    Ok(())
}

fn lock_state(
    state: &Arc<Mutex<AnomalyAlertState>>,
) -> Result<std::sync::MutexGuard<'_, AnomalyAlertState>, AnomalyAlertError> {
    state.lock().map_err(|_| AnomalyAlertError::StatePoisoned)
}

fn normalize_optional_note(note: Option<String>) -> Result<Option<String>, AnomalyAlertError> {
    match note {
        Some(value) => {
            let trimmed = value.trim();
            if trimmed.is_empty() {
                return Ok(None);
            }
            if trimmed.chars().count() > MAX_NOTE_LENGTH {
                return Err(AnomalyAlertError::InvalidNote);
            }
            Ok(Some(trimmed.to_owned()))
        }
        None => Ok(None),
    }
}

fn normalize_required_note(note: Option<String>) -> Result<String, AnomalyAlertError> {
    let Some(note) = normalize_optional_note(note)? else {
        return Err(AnomalyAlertError::ClosureNoteRequired);
    };
    Ok(note)
}

fn normalize_closure_evidence(refs: Vec<String>) -> Result<Vec<String>, AnomalyAlertError> {
    let normalized = refs
        .into_iter()
        .map(|value| value.trim().to_owned())
        .filter(|value| !value.is_empty())
        .map(|value| {
            if value.chars().count() > MAX_EVIDENCE_REF_LENGTH {
                Err(AnomalyAlertError::InvalidClosureEvidence)
            } else {
                Ok(value)
            }
        })
        .collect::<Result<Vec<_>, _>>()?;

    if normalized.is_empty() {
        return Err(AnomalyAlertError::ClosureEvidenceRequired);
    }
    Ok(normalized)
}

fn normalize_optional_ticket_reference(
    value: Option<String>,
) -> Result<Option<String>, AnomalyAlertError> {
    match value {
        Some(value) => {
            let trimmed = value.trim();
            if trimmed.is_empty() {
                return Ok(None);
            }
            if trimmed.chars().count() > MAX_TICKET_REFERENCE_LENGTH {
                return Err(AnomalyAlertError::InvalidTicketReference);
            }
            Ok(Some(trimmed.to_owned()))
        }
        None => Ok(None),
    }
}

fn compare_timestamps(left: AuditTimestamp, right: AuditTimestamp) -> Ordering {
    left.epoch_day()
        .cmp(&right.epoch_day())
        .then(left.minute_of_day().cmp(&right.minute_of_day()))
}

fn add_minutes(
    timestamp: AuditTimestamp,
    minutes: u32,
) -> Result<AuditTimestamp, AnomalyAlertError> {
    let minutes_per_day = i64::from(24_u16 * 60_u16);
    let base_total_minutes =
        i64::from(timestamp.epoch_day()) * minutes_per_day + i64::from(timestamp.minute_of_day());
    let next_total_minutes = base_total_minutes
        .checked_add(i64::from(minutes))
        .ok_or(AnomalyAlertError::TimestampOverflow)?;
    let epoch_day = i32::try_from(next_total_minutes.div_euclid(minutes_per_day))
        .map_err(|_| AnomalyAlertError::TimestampOverflow)?;
    let minute_of_day = u16::try_from(next_total_minutes.rem_euclid(minutes_per_day))
        .map_err(|_| AnomalyAlertError::TimestampOverflow)?;
    AuditTimestamp::new(epoch_day, minute_of_day).map_err(AnomalyAlertError::AuditTrail)
}

fn is_valid_status_transition(from: AnomalyAlertStatus, to: AnomalyAlertStatus) -> bool {
    matches!(
        (from, to),
        (AnomalyAlertStatus::Open, AnomalyAlertStatus::Acknowledged)
            | (
                AnomalyAlertStatus::Open,
                AnomalyAlertStatus::RemediationInProgress
            )
            | (AnomalyAlertStatus::Open, AnomalyAlertStatus::Escalated)
            | (AnomalyAlertStatus::Open, AnomalyAlertStatus::Closed)
            | (
                AnomalyAlertStatus::Acknowledged,
                AnomalyAlertStatus::RemediationInProgress
            )
            | (
                AnomalyAlertStatus::Acknowledged,
                AnomalyAlertStatus::Escalated
            )
            | (AnomalyAlertStatus::Acknowledged, AnomalyAlertStatus::Closed)
            | (
                AnomalyAlertStatus::RemediationInProgress,
                AnomalyAlertStatus::Escalated
            )
            | (
                AnomalyAlertStatus::RemediationInProgress,
                AnomalyAlertStatus::Closed
            )
            | (AnomalyAlertStatus::Escalated, AnomalyAlertStatus::Closed)
    )
}
#[derive(Debug, Clone, PartialEq)]
pub enum AnomalyAlertError {
    InvalidRuleId,
    InvalidAlertId,
    InvalidRuleText {
        field: String,
    },
    InvalidThresholdValue {
        rule_id: AnomalyRuleId,
        threshold_value: f64,
    },
    InvalidRuleComparator {
        rule_id: AnomalyRuleId,
        kind: AnomalyRuleKind,
        comparator: AnomalyThresholdComparator,
    },
    InvalidEvaluationWindowDays {
        rule_id: AnomalyRuleId,
        evaluation_window_days: u16,
    },
    InvalidSlaMinutes {
        rule_id: AnomalyRuleId,
        sla_minutes: u32,
    },
    AlertNotFound(AnomalyAlertId),
    AlertAlreadyClosed {
        alert_id: AnomalyAlertId,
    },
    InvalidStatusTransition {
        alert_id: AnomalyAlertId,
        from_status: AnomalyAlertStatus,
        to_status: AnomalyAlertStatus,
    },
    ClosureNoteRequired,
    ClosureEvidenceRequired,
    InvalidClosureEvidence,
    InvalidTicketReference,
    InvalidNote,
    AlertSequenceOverflow,
    TimestampOverflow,
    UnauthorizedRole {
        actual: Role,
    },
    StatePoisoned,
    AuditTrail(AuditTrailError),
}

impl fmt::Display for AnomalyAlertError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::InvalidRuleId => {
                f.write_str("anomaly rule id must match `^rule-[a-z0-9-]{3,64}$`")
            }
            Self::InvalidAlertId => f.write_str("anomaly alert id must not be empty"),
            Self::InvalidRuleText { field } => write!(
                f,
                "anomaly rule field `{field}` must be non-empty and at most {MAX_RULE_TEXT_LENGTH} chars"
            ),
            Self::InvalidThresholdValue {
                rule_id,
                threshold_value,
            } => write!(
                f,
                "anomaly rule {rule_id} has invalid threshold value {threshold_value}"
            ),
            Self::InvalidRuleComparator {
                rule_id,
                kind,
                comparator,
            } => write!(
                f,
                "anomaly rule {rule_id} kind {kind} does not support comparator {}",
                comparator.as_str()
            ),
            Self::InvalidEvaluationWindowDays {
                rule_id,
                evaluation_window_days,
            } => write!(
                f,
                "anomaly rule {rule_id} evaluationWindowDays must be > 0, got {evaluation_window_days}"
            ),
            Self::InvalidSlaMinutes {
                rule_id,
                sla_minutes,
            } => write!(
                f,
                "anomaly rule {rule_id} slaMinutes must be > 0, got {sla_minutes}"
            ),
            Self::AlertNotFound(alert_id) => write!(f, "anomaly alert {alert_id} not found"),
            Self::AlertAlreadyClosed { alert_id } => {
                write!(f, "anomaly alert {alert_id} is already closed")
            }
            Self::InvalidStatusTransition {
                alert_id,
                from_status,
                to_status,
            } => write!(
                f,
                "anomaly alert {alert_id} cannot transition from {from_status} to {to_status}"
            ),
            Self::ClosureNoteRequired => {
                f.write_str("closure note is required when closing an anomaly alert")
            }
            Self::ClosureEvidenceRequired => {
                f.write_str("closure evidence is required when closing an anomaly alert")
            }
            Self::InvalidClosureEvidence => write!(
                f,
                "closure evidence refs must be non-empty and at most {MAX_EVIDENCE_REF_LENGTH} chars"
            ),
            Self::InvalidTicketReference => write!(
                f,
                "ticket reference must be at most {MAX_TICKET_REFERENCE_LENGTH} characters"
            ),
            Self::InvalidNote => write!(
                f,
                "alert note must be at most {MAX_NOTE_LENGTH} characters"
            ),
            Self::AlertSequenceOverflow => f.write_str("anomaly alert sequence overflowed"),
            Self::TimestampOverflow => f.write_str("anomaly alert timestamp overflowed"),
            Self::UnauthorizedRole { actual } => write!(
                f,
                "committee-admin role is required for anomaly governance operations, got {actual:?}"
            ),
            Self::StatePoisoned => f.write_str("anomaly alert state is poisoned"),
            Self::AuditTrail(error) => write!(f, "{error}"),
        }
    }
}

impl std::error::Error for AnomalyAlertError {}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::audit::{AuditAction, AuditInvestigationFilter, AuditRetentionPolicy};
    use crate::identity::{AuthenticationSource, PlantScope};

    fn actor_id(value: &str) -> ActorId {
        ActorId::parse(value).expect("actor id should be valid")
    }

    fn committee_actor() -> AuthenticatedActorContext {
        AuthenticatedActorContext::new(
            actor_id("committee-anomaly-test"),
            Role::CommitteeAdmin,
            PlantScope::all(),
            AuthenticationSource::CorporateSso,
        )
        .expect("committee actor should be valid")
    }

    fn vendor_id(value: &str) -> VendorId {
        VendorId::parse(value).expect("vendor id should be valid")
    }

    #[test]
    fn default_rules_cover_required_governance_signals() {
        let rules = AnomalyRule::default_governance_rules();
        let kinds = rules
            .iter()
            .map(|rule| rule.kind())
            .collect::<std::collections::BTreeSet<_>>();
        assert_eq!(rules.len(), 4);
        assert!(kinds.contains(&AnomalyRuleKind::ExpiryRisk));
        assert!(kinds.contains(&AnomalyRuleKind::OnTimeDegradation));
        assert!(kinds.contains(&AnomalyRuleKind::SatisfactionDrop));
        assert!(kinds.contains(&AnomalyRuleKind::ComplaintSpike));
        assert!(rules
            .iter()
            .all(|rule| rule.governance_issue_id() == "issue-anomaly-governance-playbook"));
    }

    #[test]
    fn anomaly_rule_id_parse_enforces_contract_pattern() {
        let valid = AnomalyRuleId::parse("rule-expiry-risk")
            .expect("contract-conformant anomaly rule id should parse");
        assert_eq!(valid.as_str(), "rule-expiry-risk");

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
                AnomalyRuleId::parse(candidate.clone()).is_err(),
                "expected `{candidate}` to be rejected by anomaly rule id parser"
            );
        }
    }

    #[test]
    fn workflow_tracks_owner_lifecycle_sla_and_audit() {
        let committee = committee_actor();
        let default_owner = actor_id("anomaly-owner-default");
        let reassigned_owner = actor_id("anomaly-owner-reassigned");
        let audit_trail = ImmutableAuditTrail::new(AuditRetentionPolicy::default());
        let workflow = AnomalyAlertWorkflow::with_default_rules(audit_trail.clone());
        let observed_at = AuditTimestamp::new(100, 600).expect("timestamp should be valid");

        let evaluation = workflow
            .evaluate_rules(
                &committee,
                AnomalySignalSnapshot::new(vendor_id("ven-anomalytsta"), observed_at)
                    .with_on_time_rate(Some(0.80))
                    .with_complaint_count(Some(7.0)),
                &default_owner,
            )
            .expect("evaluation should succeed");
        assert_eq!(evaluation.triggered_alerts().len(), 2);

        let alert = evaluation
            .triggered_alerts()
            .iter()
            .find(|item| item.rule_kind() == AnomalyRuleKind::OnTimeDegradation)
            .expect("on-time degradation alert should exist");
        assert_eq!(alert.status(), AnomalyAlertStatus::Open);
        assert_eq!(alert.owner_actor_id(), &default_owner);
        assert_eq!(alert.sla_status(observed_at), AnomalySlaStatus::OnTrack);

        let reassigned = workflow
            .assign_owner(
                &committee,
                alert.alert_id(),
                &reassigned_owner,
                AuditTimestamp::new(100, 620).expect("timestamp should be valid"),
                Some("committee triage handoff".to_owned()),
            )
            .expect("owner assignment should succeed");
        assert_eq!(reassigned.owner_actor_id(), &reassigned_owner);

        let escalated = workflow
            .transition_alert(
                &committee,
                alert.alert_id(),
                AnomalyAlertTransition::Escalate,
                AuditTimestamp::new(100, 700).expect("timestamp should be valid"),
                Some("breach risk requires escalation".to_owned()),
                None,
                Vec::new(),
                None,
            )
            .expect("escalation should succeed");
        assert_eq!(escalated.status(), AnomalyAlertStatus::Escalated);
        assert!(escalated.is_escalated());

        let closed = workflow
            .transition_alert(
                &committee,
                alert.alert_id(),
                AnomalyAlertTransition::Close,
                AuditTimestamp::new(100, 730).expect("timestamp should be valid"),
                Some("ready to close".to_owned()),
                Some("vendor corrective actions validated".to_owned()),
                vec![
                    "evidence://anomaly/on-time/summary".to_owned(),
                    "runbook://ops/anomaly-remediation".to_owned(),
                ],
                Some("jira://OPS-52".to_owned()),
            )
            .expect("close should succeed");
        assert_eq!(closed.status(), AnomalyAlertStatus::Closed);
        assert_eq!(closed.ticket_reference(), Some("jira://OPS-52"));
        assert_eq!(closed.closure_evidence_refs().len(), 2);

        let closed_query = workflow
            .query_alerts(
                &AnomalyAlertQuery {
                    vendor_id: Some(vendor_id("ven-anomalytsta")),
                    owner_actor_id: Some(reassigned_owner.clone()),
                    status: Some(AnomalyAlertStatus::Closed),
                    escalated_only: Some(true),
                    sla_status: Some(AnomalySlaStatus::OnTrack),
                },
                AuditTimestamp::new(100, 800).expect("timestamp should be valid"),
            )
            .expect("query should succeed");
        assert_eq!(closed_query.len(), 1);
        assert_eq!(closed_query[0].alert_id(), alert.alert_id());

        let close_events = audit_trail
            .investigation_query(
                &committee,
                &AuditInvestigationFilter::default().with_action(AuditAction::CloseAnomalyAlert),
            )
            .expect("close audit events should be queryable");
        assert_eq!(close_events.len(), 1);
        assert_eq!(
            close_events[0].entity().entity_type(),
            AuditEntityType::AnomalyAlert
        );
    }
}
