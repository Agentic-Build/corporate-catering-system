import { apiClient, ensureApiClientConfigured, normalizeApiFailure } from "$lib/platform/api";

export type VendorPage = Awaited<ReturnType<typeof apiClient.admin.listAdminVendors>>;
export type VendorView = VendorPage["items"][number];
export type VendorStatus = VendorView["status"];
export type VendorCategory = VendorView["vendorCategory"];
export type VendorReviewDecision = VendorView["reviewHistory"][number]["decision"];
export type VendorSortField = NonNullable<Parameters<typeof apiClient.admin.listAdminVendors>[2]>;
export type SortOrder = NonNullable<Parameters<typeof apiClient.admin.listAdminVendors>[3]>;

export type TemplatePage = Awaited<
  ReturnType<typeof apiClient.admin.listComplianceDocumentTemplates>
>;
export type TemplateView = TemplatePage["items"][number];

export type MappingPage = Awaited<
  ReturnType<typeof apiClient.admin.listVendorPlantDeliveryMappings>
>;
export type MappingView = MappingPage["items"][number];
export type MappingEffect = MappingView["effect"];

export type SettlementPage = Awaited<
  ReturnType<typeof apiClient.admin.closePayrollMonthlySettlement>
>;
export type SettlementRecord = SettlementPage["items"][number];
export type SettlementSortField = NonNullable<
  NonNullable<Parameters<typeof apiClient.admin.closePayrollMonthlySettlement>[0]>["sortBy"]
>;

export type PayrollDisputeView = Awaited<
  ReturnType<typeof apiClient.admin.updateAdminPayrollDispute>
>;
export type PayrollDisputeOperation = "ASSIGN_OWNER" | "RESOLVE_REFUND" | "RESOLVE_REJECTED";

export type AnomalyAlertList = Awaited<ReturnType<typeof apiClient.admin.listAnomalyAlerts>>;
export type AnomalyAlertView = AnomalyAlertList["items"][number];
export type AnomalyAlertStatusValue = NonNullable<
  Parameters<typeof apiClient.admin.listAnomalyAlerts>[2]
>;
export type AnomalySlaStatus = NonNullable<Parameters<typeof apiClient.admin.listAnomalyAlerts>[4]>;

export type AnomalyRuleList = Awaited<ReturnType<typeof apiClient.admin.listAnomalyRules>>;
export type AnomalyRuleView = AnomalyRuleList["items"][number];
export type AnomalyRuleKind = AnomalyRuleView["kind"];
export type AnomalyThresholdComparator = AnomalyRuleView["thresholdComparator"];
export type AnomalyRuleSeverity = AnomalyRuleView["severity"];

export type AuditInvestigations = Awaited<
  ReturnType<typeof apiClient.admin.queryAuditInvestigations>
>;
export type AuditEvidenceView = AuditInvestigations["items"][number];
export type AuditResponsibilities = Awaited<
  ReturnType<typeof apiClient.admin.queryAuditResponsibilities>
>;
export type AuditResponsibilityView = AuditResponsibilities["items"][number];
export type AuditAction = NonNullable<Parameters<typeof apiClient.admin.queryAuditInvestigations>[1]>;
export type AuditEntityType = NonNullable<
  Parameters<typeof apiClient.admin.queryAuditInvestigations>[2]
>;

export type OperationsAnalyticsView = Awaited<
  ReturnType<typeof apiClient.admin.getAdminOperationsAnalyticsDashboard>
>;

export const VENDOR_STATUS_OPTIONS: VendorStatus[] = [
  "PENDING_REVIEW",
  "FIX_REQUESTED",
  "APPROVED",
  "REJECTED",
  "SUSPENDED"
];

export const VENDOR_SORT_FIELD_OPTIONS: VendorSortField[] = [
  "createdAt",
  "status",
  "displayName",
  "vendorCategory"
];

export const VENDOR_CATEGORY_OPTIONS: VendorCategory[] = [
  "RESTAURANT",
  "BEVERAGE",
  "DESSERT",
  "HEALTHY_MEAL",
  "SNACK"
];

export const VENDOR_REVIEW_DECISION_OPTIONS: VendorReviewDecision[] = [
  "APPROVED",
  "REQUEST_FIX",
  "REJECTED"
];

export const MAPPING_EFFECT_OPTIONS: MappingEffect[] = ["ALLOW", "DENY"];

export const SETTLEMENT_SORT_FIELD_OPTIONS: SettlementSortField[] = [
  "deliveryDate",
  "employeeActorId",
  "amountMinor"
];

export const SETTLEMENT_EXCEPTION_CLASS_OPTIONS = [
  "DISPUTED",
  "DEDUCTION_FAILED",
  "EMPLOYEE_TERMINATED",
  "REFUNDED"
] as const;
export type SettlementExceptionClass = (typeof SETTLEMENT_EXCEPTION_CLASS_OPTIONS)[number];

export const PAYROLL_DISPUTE_OPERATION_OPTIONS: PayrollDisputeOperation[] = [
  "ASSIGN_OWNER",
  "RESOLVE_REFUND",
  "RESOLVE_REJECTED"
];

export const ANOMALY_STATUS_OPTIONS: AnomalyAlertStatusValue[] = [
  "OPEN",
  "ACKNOWLEDGED",
  "REMEDIATION_IN_PROGRESS",
  "ESCALATED",
  "CLOSED"
];

export const ANOMALY_SLA_STATUS_OPTIONS: AnomalySlaStatus[] = ["ON_TRACK", "BREACHED"];

export const ANOMALY_SEVERITY_OPTIONS: AnomalyRuleSeverity[] = ["WARNING", "CRITICAL"];

export const ANOMALY_RULE_KIND_OPTIONS: AnomalyRuleKind[] = [
  "EXPIRY_RISK",
  "ON_TIME_DEGRADATION",
  "SATISFACTION_DROP",
  "COMPLAINT_SPIKE"
];

export const ANOMALY_COMPARATOR_OPTIONS: AnomalyThresholdComparator[] = ["LT", "LTE", "GT", "GTE"];

export const ANOMALY_PATCH_OPERATION_OPTIONS = [
  "ASSIGN_OWNER",
  "ACKNOWLEDGE",
  "START_REMEDIATION",
  "ESCALATE",
  "CLOSE"
] as const;
export type AnomalyPatchOperation = (typeof ANOMALY_PATCH_OPERATION_OPTIONS)[number];

export const AUDIT_ACTION_OPTIONS: AuditAction[] = [
  "CREATE_EMPLOYEE_ORDER",
  "UPDATE_EMPLOYEE_ORDER",
  "VERIFY_PICKUP_ORDER",
  "MARK_ORDER_SOLD_OUT",
  "MARK_ORDER_REFUND_PENDING",
  "MARK_ORDER_REFUNDED",
  "UPSERT_VENDOR_MENU_ITEM",
  "UPSERT_VENDOR_ORDERING_POLICY",
  "ADVANCE_VENDOR_FULFILLMENT_DELIVERY_STATUS",
  "CREATE_VENDOR_FULFILLMENT_EXPORT_BATCH",
  "UPSERT_VENDOR_PLANT_DELIVERY_MAPPING",
  "DELETE_VENDOR_PLANT_DELIVERY_MAPPING",
  "UPSERT_COMPLIANCE_DOCUMENT_TEMPLATE",
  "REGISTER_VENDOR_APPLICATION",
  "SUBMIT_VENDOR_COMPLIANCE_DOCUMENT",
  "REVIEW_VENDOR_APPLICATION",
  "RUN_VENDOR_COMPLIANCE_LIFECYCLE",
  "PURGE_AUDIT_EVIDENCE",
  "PRUNE_VENDOR_COMPLIANCE_HISTORY",
  "EXPORT_PAYROLL_DEDUCTIONS",
  "APPEND_PAYROLL_LEDGER_ENTRY",
  "OPEN_PAYROLL_DISPUTE",
  "ASSIGN_PAYROLL_DISPUTE_OWNER",
  "RESOLVE_PAYROLL_DISPUTE",
  "EXPORT_PAYROLL_SFTP_BATCH",
  "LOCK_PAYROLL_SETTLEMENT_CYCLE",
  "UNLOCK_PAYROLL_SETTLEMENT_CYCLE",
  "SYNC_PAYROLL_HR_API_ADJUNCT",
  "PURGE_PAYROLL_DATA",
  "PURGE_ORDER_DATA",
  "UPSERT_ANOMALY_DETECTION_RULE",
  "TRIGGER_ANOMALY_ALERT",
  "ASSIGN_ANOMALY_ALERT_OWNER",
  "ADVANCE_ANOMALY_ALERT_STATUS",
  "CLOSE_ANOMALY_ALERT"
];

export const AUDIT_ENTITY_TYPE_OPTIONS: AuditEntityType[] = [
  "ORDER",
  "MENU_ITEM",
  "VENDOR",
  "DELIVERY_MAPPING",
  "COMPLIANCE_DOCUMENT_TEMPLATE",
  "FULFILLMENT_BATCH",
  "SETTLEMENT",
  "VENDOR_ORDERING_POLICY",
  "AUDIT_TRAIL",
  "PAYROLL_LEDGER_ENTRY",
  "PAYROLL_DISPUTE",
  "PAYROLL_EXCHANGE_BATCH",
  "PAYROLL_DATA_RETENTION",
  "ANOMALY_RULE",
  "ANOMALY_ALERT"
];

export function configureAdminApi(apiBearerToken: string | null): void {
  ensureApiClientConfigured(apiBearerToken);
}

export function describeApiError(error: unknown): string {
  return normalizeApiFailure(error).localizedMessage;
}

export function vendorStatusTone(
  status: VendorStatus
): "info" | "success" | "warning" | "danger" | "pending" | "neutral" {
  switch (status) {
    case "APPROVED":
      return "success";
    case "PENDING_REVIEW":
      return "warning";
    case "FIX_REQUESTED":
      return "info";
    case "REJECTED":
      return "danger";
    case "SUSPENDED":
      return "danger";
    default:
      return "neutral";
  }
}

export function anomalyStatusTone(
  status: AnomalyAlertStatusValue
): "info" | "success" | "warning" | "danger" | "pending" | "neutral" {
  switch (status) {
    case "OPEN":
      return "warning";
    case "ACKNOWLEDGED":
      return "info";
    case "REMEDIATION_IN_PROGRESS":
      return "info";
    case "ESCALATED":
      return "danger";
    case "CLOSED":
      return "success";
    default:
      return "neutral";
  }
}

export function slaStatusTone(
  status: AnomalySlaStatus
): "success" | "danger" {
  return status === "BREACHED" ? "danger" : "success";
}

export function mapSettlementExceptionClass(
  record: SettlementRecord
): SettlementExceptionClass | null {
  if (record.status === "DISPUTED") return "DISPUTED";
  if (record.status === "DEDUCTION_FAILED") return "DEDUCTION_FAILED";
  if (record.status === "EMPLOYEE_TERMINATED") return "EMPLOYEE_TERMINATED";
  if (record.status === "REFUNDED") return "REFUNDED";
  return null;
}

export function normalizeOptional(value: string): string | undefined {
  const trimmed = value.trim();
  return trimmed.length === 0 ? undefined : trimmed;
}

export function formatMetricValue(metricKey: string, value: number): string {
  if (metricKey.endsWith("_rate")) {
    return `${(value * 100).toFixed(2)}%`;
  }
  if (metricKey.endsWith("_score")) {
    return value.toFixed(2);
  }
  return Number.isInteger(value) ? `${value}` : value.toFixed(2);
}

export function epochSecondsToTaipeiDateTime(epochSeconds: number): string {
  const iso = new Date(epochSeconds * 1000).toISOString();
  return new Date(iso).toLocaleString("zh-TW", {
    hour12: false,
    timeZone: "Asia/Taipei"
  });
}

/**
 * Persist closed settlement cycles locally so the cycle list & dispute list can
 * surface recent work. Keyed by cycleKey + batchId.
 */
const RECENT_SETTLEMENTS_KEY = "admin.settlement.recentClose";
const MAX_RECENT_SETTLEMENTS = 12;

export interface RecentSettlementEntry {
  cycleKey: string;
  closedAtEpochMs: number;
  batchId: string;
  totalRecords: number;
  disputedRecords: number;
  deductionFailedRecords: number;
  refundedRecords: number;
  exceptions: Array<{
    disputeId?: string;
    orderId?: string;
    employeeActorId: string;
    status: string;
    amountMinor: number;
    currency: string;
  }>;
}

export function rememberSettlementClose(entry: RecentSettlementEntry): void {
  if (typeof window === "undefined") return;
  try {
    const raw = window.localStorage.getItem(RECENT_SETTLEMENTS_KEY);
    const list: RecentSettlementEntry[] = raw ? JSON.parse(raw) : [];
    const deduped = list.filter((e) => e.cycleKey !== entry.cycleKey);
    deduped.unshift(entry);
    window.localStorage.setItem(
      RECENT_SETTLEMENTS_KEY,
      JSON.stringify(deduped.slice(0, MAX_RECENT_SETTLEMENTS))
    );
  } catch {
    // Ignore storage errors; the hub falls back to empty list.
  }
}

export function readRecentSettlements(): RecentSettlementEntry[] {
  if (typeof window === "undefined") return [];
  try {
    const raw = window.localStorage.getItem(RECENT_SETTLEMENTS_KEY);
    if (!raw) return [];
    const parsed = JSON.parse(raw);
    if (!Array.isArray(parsed)) return [];
    return parsed as RecentSettlementEntry[];
  } catch {
    return [];
  }
}
