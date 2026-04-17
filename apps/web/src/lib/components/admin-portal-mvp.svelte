<script lang="ts">
  import { onDestroy, onMount } from "svelte";

  import {
    ANOMALY_RELEASE_SIGN_OFF_ISSUE_ID,
    SETTLEMENT_RELEASE_SIGN_OFF_ISSUE_ID,
    formatTaipeiDateTime,
    isIssueSignOffConfirmed,
    parseBooleanFlag,
    parseEvidenceRefsInput,
    parseIssueChecklist,
    parseOptionalEpochDay,
    parseOptionalMinuteOfDay,
    parseOptionalNumber,
    parseRequiredNumber,
    toTaipeiDateTime,
    todayTaipeiIsoDate
  } from "$lib/admin/portal";
  import { zhTW } from "$lib/i18n/zh-tw";
  import { apiClient, ensureApiClientConfigured } from "$lib/platform/api";
  import { normalizeApiFailure } from "$lib/platform/api/failure";

  interface Props {
    sectionId: string;
    actorDisplayName: string;
    actorId: string;
    provider: string;
    apiBearerToken: string | null;
  }

  let { sectionId, actorDisplayName, actorId, provider, apiBearerToken }: Props = $props();

  type VendorPage = Awaited<ReturnType<typeof apiClient.admin.listAdminVendors>>;
  type VendorView = VendorPage["items"][number];
  type VendorStatus = VendorView["status"];
  type VendorCategory = VendorView["vendorCategory"];
  type VendorReviewDecision = VendorView["reviewHistory"][number]["decision"];
  type VendorSortField = NonNullable<Parameters<typeof apiClient.admin.listAdminVendors>[2]>;
  type SortOrder = NonNullable<Parameters<typeof apiClient.admin.listAdminVendors>[3]>;

  type TemplatePage = Awaited<ReturnType<typeof apiClient.admin.listComplianceDocumentTemplates>>;
  type TemplateView = TemplatePage["items"][number];

  type MappingPage = Awaited<ReturnType<typeof apiClient.admin.listVendorPlantDeliveryMappings>>;
  type MappingView = MappingPage["items"][number];
  type MappingEffect = MappingView["effect"];

  type SettlementPage = Awaited<ReturnType<typeof apiClient.admin.closePayrollMonthlySettlement>>;
  type SettlementRecord = SettlementPage["items"][number];
  type SettlementSortField = NonNullable<
    NonNullable<Parameters<typeof apiClient.admin.closePayrollMonthlySettlement>[0]>["sortBy"]
  >;

  type PayrollDisputeView = Awaited<ReturnType<typeof apiClient.admin.updateAdminPayrollDispute>>;
  type PayrollDisputeOperation = "ASSIGN_OWNER" | "RESOLVE_REFUND" | "RESOLVE_REJECTED";

  type AnomalyAlertList = Awaited<ReturnType<typeof apiClient.admin.listAnomalyAlerts>>;
  type AnomalyAlertView = AnomalyAlertList["items"][number];
  type AnomalyAlertStatus = NonNullable<Parameters<typeof apiClient.admin.listAnomalyAlerts>[2]>;
  type AnomalySlaStatus = NonNullable<Parameters<typeof apiClient.admin.listAnomalyAlerts>[4]>;

  type AnomalyRuleList = Awaited<ReturnType<typeof apiClient.admin.listAnomalyRules>>;
  type AnomalyRuleView = AnomalyRuleList["items"][number];

  type AuditInvestigations = Awaited<ReturnType<typeof apiClient.admin.queryAuditInvestigations>>;
  type AuditEvidenceView = AuditInvestigations["items"][number];
  type AuditResponsibilities = Awaited<ReturnType<typeof apiClient.admin.queryAuditResponsibilities>>;
  type AuditResponsibilityView = AuditResponsibilities["items"][number];
  type AuditAction = NonNullable<Parameters<typeof apiClient.admin.queryAuditInvestigations>[1]>;
  type AuditEntityType = NonNullable<Parameters<typeof apiClient.admin.queryAuditInvestigations>[2]>;

  type OperationsAnalyticsView = Awaited<
    ReturnType<typeof apiClient.admin.getAdminOperationsAnalyticsDashboard>
  >;

  interface PortalNotification {
    id: number;
    kind: "success" | "error" | "info";
    message: string;
  }

  const sectionTitleById: Record<string, string> = {
    overview: zhTW.nav.sections.admin.overview,
    vendors: zhTW.nav.sections.admin.vendors,
    settlement: zhTW.nav.sections.admin.settlement,
    anomalies: zhTW.nav.sections.admin.anomalies,
    audit: zhTW.nav.sections.admin.audit,
    analytics: zhTW.nav.sections.admin.analytics
  };

  const sectionDescriptionById: Record<string, string> = {
    overview: zhTW.portal.admin.sectionDescriptions.overview,
    vendors: zhTW.portal.admin.sectionDescriptions.vendors,
    settlement: zhTW.portal.admin.sectionDescriptions.settlement,
    anomalies: zhTW.portal.admin.sectionDescriptions.anomalies,
    audit: zhTW.portal.admin.sectionDescriptions.audit,
    analytics: zhTW.portal.admin.sectionDescriptions.analytics
  };

  const vendorStatusOptions: VendorStatus[] = [
    "PENDING_REVIEW",
    "FIX_REQUESTED",
    "APPROVED",
    "REJECTED",
    "SUSPENDED"
  ];
  const vendorSortFieldOptions: VendorSortField[] = [
    "createdAt",
    "status",
    "displayName",
    "vendorCategory"
  ];
  const vendorCategoryOptions: VendorCategory[] = [
    "RESTAURANT",
    "BEVERAGE",
    "DESSERT",
    "HEALTHY_MEAL",
    "SNACK"
  ];
  const vendorReviewDecisionOptions: VendorReviewDecision[] = [
    "APPROVED",
    "REQUEST_FIX",
    "REJECTED"
  ];
  const mappingEffectOptions: MappingEffect[] = ["ALLOW", "DENY"];
  const settlementSortFieldOptions: SettlementSortField[] = [
    "deliveryDate",
    "employeeActorId",
    "amountMinor"
  ];
  const settlementExceptionClassOptions = [
    "DISPUTED",
    "DEDUCTION_FAILED",
    "EMPLOYEE_TERMINATED",
    "REFUNDED"
  ] as const;
  const payrollDisputeOperationOptions: PayrollDisputeOperation[] = [
    "ASSIGN_OWNER",
    "RESOLVE_REFUND",
    "RESOLVE_REJECTED"
  ];
  const anomalyStatusOptions: AnomalyAlertStatus[] = [
    "OPEN",
    "ACKNOWLEDGED",
    "REMEDIATION_IN_PROGRESS",
    "ESCALATED",
    "CLOSED"
  ];
  const anomalySlaStatusOptions: AnomalySlaStatus[] = ["ON_TRACK", "BREACHED"];
  const anomalySeverityOptions: AnomalyRuleView["severity"][] = ["WARNING", "CRITICAL"];
  const anomalyRuleKindOptions: AnomalyRuleView["kind"][] = [
    "EXPIRY_RISK",
    "ON_TIME_DEGRADATION",
    "SATISFACTION_DROP",
    "COMPLAINT_SPIKE"
  ];
  const anomalyComparatorOptions: AnomalyRuleView["thresholdComparator"][] = [
    "LT",
    "LTE",
    "GT",
    "GTE"
  ];
  const anomalyPatchOperationOptions = [
    "ASSIGN_OWNER",
    "ACKNOWLEDGE",
    "START_REMEDIATION",
    "ESCALATE",
    "CLOSE"
  ] as const;
  const auditActionOptions: AuditAction[] = [
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
  const auditEntityTypeOptions: AuditEntityType[] = [
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

  const todayIsoDate = todayTaipeiIsoDate();

  let notifications = $state<PortalNotification[]>([]);
  let nextNotificationId = 1;
  const notificationTimeouts = new Set<ReturnType<typeof setTimeout>>();

  let vendors = $state<VendorView[]>([]);
  let vendorPageMeta = $state<VendorPage["page"] | null>(null);
  let vendorLoading = $state(false);
  let vendorError = $state<string | null>(null);
  let selectedVendorId = $state<string | null>(null);
  let vendorStatusFilter = $state<"ALL" | VendorStatus>("ALL");
  let vendorSortBy = $state<VendorSortField>("createdAt");
  let vendorSortOrder = $state<SortOrder>("desc");

  let reviewDecision = $state<VendorReviewDecision>("APPROVED");
  let reviewComment = $state("符合入駐規範並通過福委會審核。");
  let reviewSubmitting = $state(false);

  let templateItems = $state<TemplateView[]>([]);
  let templatePageMeta = $state<TemplatePage["page"] | null>(null);
  let templateLoading = $state(false);
  let templateError = $state<string | null>(null);
  let templateCategoryFilter = $state<"ALL" | VendorCategory>("ALL");
  let templateDraft = $state({
    vendorCategory: "RESTAURANT" as VendorCategory,
    templateId: "business-license",
    displayName: "商業登記文件",
    required: true,
    maxValidityDays: 365,
    reminderDaysBeforeExpiryCsv: "30,14,7",
    suspensionGraceDays: 3
  });
  let templateSaving = $state(false);

  let lifecycleRunDate = $state(todayIsoDate);
  let lifecycleDryRun = $state(false);
  let lifecycleRunning = $state(false);
  let lifecycleResult = $state<Awaited<ReturnType<typeof apiClient.admin.runVendorComplianceLifecycle>> | null>(
    null
  );

  let mappings = $state<MappingView[]>([]);
  let mappingAuditTrail = $state<MappingPage["auditTrail"]>([]);
  let mappingPageMeta = $state<MappingPage["page"] | null>(null);
  let mappingLoading = $state(false);
  let mappingError = $state<string | null>(null);
  let mappingVendorFilter = $state("");
  let mappingPlantFilter = $state("");
  let mappingActiveAt = $state("");
  let mappingDraft = $state({
    vendorId: "",
    mappingId: "",
    plantId: "",
    effect: "ALLOW" as MappingEffect,
    precedence: 100,
    serviceWindowStartsAtLocal: `${todayIsoDate}T10:00`,
    serviceWindowEndsAtLocal: `${todayIsoDate}T14:00`
  });
  let mappingSaving = $state(false);
  let deletingMappingById = $state<Record<string, boolean>>({});

  let objectStorageRef = $state("");
  let objectStorageLinkLoading = $state(false);
  let objectStorageLinkError = $state<string | null>(null);
  let objectStorageLinkResult = $state<Awaited<
    ReturnType<typeof apiClient.admin.createAdminObjectStorageAccessLink>
  > | null>(null);

  let settlementIssueChecklistRaw = $state("");
  let settlementCloseDraft = $state({
    cycleKey: "",
    page: 1,
    pageSize: 200,
    sortBy: "deliveryDate" as SettlementSortField,
    sortOrder: "desc" as SortOrder
  });
  let settlementClosing = $state(false);
  let settlementError = $state<string | null>(null);
  let settlementPage = $state<SettlementPage | null>(null);
  let settlementExceptionClassFilter = $state<
    "ALL" | (typeof settlementExceptionClassOptions)[number]
  >("ALL");

  let settlementLockDraft = $state({
    cycleKey: "",
    lockReason: "",
    unlockReason: ""
  });
  let settlementLocking = $state(false);
  let settlementUnlocking = $state(false);
  let settlementLockState = $state<Awaited<
    ReturnType<typeof apiClient.admin.lockPayrollSettlementCycle>
  >["settlementCycle"] | null>(null);

  let disputeDraft = $state({
    disputeId: "",
    operation: "ASSIGN_OWNER" as PayrollDisputeOperation,
    ownerActorId: "",
    note: "",
    refundAmountMinor: ""
  });
  let disputeSubmitting = $state(false);
  let disputeResult = $state<PayrollDisputeView | null>(null);

  let anomalyAlerts = $state<AnomalyAlertView[]>([]);
  let anomalyAlertsLoading = $state(false);
  let anomalyAlertsError = $state<string | null>(null);
  let selectedAlertId = $state<string | null>(null);
  let anomalyFilters = $state({
    vendorId: "",
    ownerActorId: "",
    status: "ALL" as "ALL" | AnomalyAlertStatus,
    escalatedOnly: "ALL",
    slaStatus: "ALL" as "ALL" | AnomalySlaStatus,
    asOfEpochDay: "",
    asOfMinuteOfDay: ""
  });

  let anomalyIssueChecklistRaw = $state("");
  let anomalyPatchDraft = $state({
    operation: "ACKNOWLEDGE" as (typeof anomalyPatchOperationOptions)[number],
    ownerActorId: "",
    note: "",
    closureNote: "",
    closureEvidenceRefsCsv: "",
    ticketReference: ""
  });
  let anomalyPatchSubmitting = $state(false);

  let anomalyEvaluationDraft = $state({
    vendorId: "",
    defaultOwnerActorId: "",
    daysUntilExpiry: "",
    onTimeRate: "",
    satisfactionScore: "",
    complaintCount: "",
    observedAtEpochDay: "",
    observedAtMinuteOfDay: ""
  });
  let anomalyEvaluating = $state(false);
  let anomalyEvaluationResult = $state<
    Awaited<ReturnType<typeof apiClient.admin.evaluateAnomalyAlerts>>["triggeredAlerts"]
  >([]);

  let anomalyRules = $state<AnomalyRuleView[]>([]);
  let anomalyRulesLoading = $state(false);
  let anomalyRulesError = $state<string | null>(null);
  let anomalyRuleDraft = $state({
    ruleId: "rule-custom-governance",
    kind: "EXPIRY_RISK" as AnomalyRuleView["kind"],
    displayName: "Custom Governance Rule",
    description: "Custom anomaly governance threshold",
    governanceIssueId: ANOMALY_RELEASE_SIGN_OFF_ISSUE_ID,
    enabled: true,
    thresholdValue: "7",
    thresholdComparator: "LTE" as AnomalyRuleView["thresholdComparator"],
    evaluationWindowDays: "7",
    slaMinutes: "240",
    severity: "WARNING" as AnomalyRuleView["severity"]
  });
  let anomalyRuleSaving = $state(false);

  let auditFilters = $state({
    actorId: "",
    action: "ALL" as "ALL" | AuditAction,
    entityType: "ALL" as "ALL" | AuditEntityType,
    entityId: "",
    correlationId: "",
    occurredFromEpochDay: "",
    occurredToEpochDay: ""
  });
  let auditLoading = $state(false);
  let auditError = $state<string | null>(null);
  let auditEvidenceItems = $state<AuditEvidenceView[]>([]);
  let auditResponsibilityItems = $state<AuditResponsibilityView[]>([]);

  let analyticsFilters = $state({
    fromEpochDay: "",
    toEpochDay: ""
  });
  let analyticsLoading = $state(false);
  let analyticsError = $state<string | null>(null);
  let analyticsDashboard = $state<OperationsAnalyticsView | null>(null);

  const sectionTitle = $derived(sectionTitleById[sectionId] ?? sectionId);
  const sectionDescription = $derived(sectionDescriptionById[sectionId] ?? "");

  const selectedVendor = $derived.by(
    () => vendors.find((vendor) => vendor.vendorId === selectedVendorId) ?? null
  );
  const selectedAlert = $derived.by(
    () => anomalyAlerts.find((alert) => alert.alertId === selectedAlertId) ?? null
  );
  const pendingVendorCount = $derived(
    vendors.filter((vendor) => vendor.status === "PENDING_REVIEW").length
  );
  const suspendedVendorCount = $derived(vendors.filter((vendor) => vendor.status === "SUSPENDED").length);
  const openAnomalyCount = $derived(anomalyAlerts.filter((alert) => alert.status !== "CLOSED").length);
  const breachedAnomalyCount = $derived(
    anomalyAlerts.filter((alert) => alert.slaStatus === "BREACHED").length
  );

  const settlementExceptionRecords = $derived.by(() => {
    const items = settlementPage?.items ?? [];
    if (settlementExceptionClassFilter === "ALL") {
      return items.filter((item) => settlementRecordIsException(item));
    }
    return items.filter((item) => mapSettlementExceptionClass(item) === settlementExceptionClassFilter);
  });

  const analyticsMetricKeys = $derived.by(() => {
    if (!analyticsDashboard) {
      return [] as string[];
    }
    return analyticsDashboard.metricDefinitions.map((definition) => definition.key);
  });

  onMount(() => {
    void bootstrapPortal();
  });

  onDestroy(() => {
    for (const timeout of notificationTimeouts) {
      clearTimeout(timeout);
    }
    notificationTimeouts.clear();
  });

  $effect(() => {
    if (!selectedVendorId && vendors.length > 0) {
      selectedVendorId = vendors[0].vendorId;
      return;
    }

    if (selectedVendorId && !vendors.some((vendor) => vendor.vendorId === selectedVendorId)) {
      selectedVendorId = vendors[0]?.vendorId ?? null;
    }
  });

  $effect(() => {
    if (!selectedAlertId && anomalyAlerts.length > 0) {
      selectedAlertId = anomalyAlerts[0].alertId;
      return;
    }

    if (selectedAlertId && !anomalyAlerts.some((alert) => alert.alertId === selectedAlertId)) {
      selectedAlertId = anomalyAlerts[0]?.alertId ?? null;
    }
  });

  $effect(() => {
    if (anomalyEvaluationDraft.defaultOwnerActorId.trim().length === 0) {
      anomalyEvaluationDraft.defaultOwnerActorId = actorId;
    }
  });

  async function bootstrapPortal() {
    try {
      ensureApiClientConfigured(apiBearerToken);
    } catch (error) {
      const failure = normalizeApiFailure(error);
      vendorError = failure.localizedMessage;
      templateError = failure.localizedMessage;
      mappingError = failure.localizedMessage;
      settlementError = failure.localizedMessage;
      anomalyAlertsError = failure.localizedMessage;
      anomalyRulesError = failure.localizedMessage;
      auditError = failure.localizedMessage;
      analyticsError = failure.localizedMessage;
      pushNotification("error", failure.localizedMessage);
      return;
    }

    await Promise.all([
      refreshVendors(false),
      refreshTemplates(false),
      refreshMappings(false),
      refreshAnomalyAlerts(false),
      refreshAnomalyRules(false),
      refreshAuditViews(false),
      refreshAnalyticsDashboard(false)
    ]);
  }

  async function refreshVendors(notifyOnError: boolean) {
    if (vendorLoading) {
      return;
    }

    vendorLoading = true;
    vendorError = null;
    try {
      const page = await apiClient.admin.listAdminVendors(
        1,
        200,
        vendorSortBy,
        vendorSortOrder,
        vendorStatusFilter === "ALL" ? undefined : vendorStatusFilter
      );
      vendors = page.items;
      vendorPageMeta = page.page;
    } catch (error) {
      const failure = normalizeApiFailure(error);
      vendorError = failure.localizedMessage;
      if (notifyOnError) {
        pushNotification("error", failure.localizedMessage);
      }
    } finally {
      vendorLoading = false;
    }
  }

  async function submitVendorReview() {
    if (reviewSubmitting) {
      return;
    }
    if (!selectedVendorId) {
      pushNotification("error", "請先選擇要審核的商家。");
      return;
    }

    const comment = reviewComment.trim();
    if (comment.length < 5) {
      pushNotification("error", "審核意見至少需 5 個字元。");
      return;
    }

    reviewSubmitting = true;
    try {
      const updated = await apiClient.admin.reviewVendorApplication(selectedVendorId, {
        decision: reviewDecision,
        comment
      });
      pushNotification("success", `商家 ${updated.vendorId} 審核更新為 ${updated.status}。`);
      await refreshVendors(false);
    } catch (error) {
      pushNotification("error", normalizeApiFailure(error).localizedMessage);
    } finally {
      reviewSubmitting = false;
    }
  }

  async function refreshTemplates(notifyOnError: boolean) {
    if (templateLoading) {
      return;
    }

    templateLoading = true;
    templateError = null;
    try {
      const page = await apiClient.admin.listComplianceDocumentTemplates(
        templateCategoryFilter === "ALL" ? undefined : templateCategoryFilter
      );
      templateItems = page.items;
      templatePageMeta = page.page;
    } catch (error) {
      const failure = normalizeApiFailure(error);
      templateError = failure.localizedMessage;
      if (notifyOnError) {
        pushNotification("error", failure.localizedMessage);
      }
    } finally {
      templateLoading = false;
    }
  }

  async function submitTemplateDraft() {
    if (templateSaving) {
      return;
    }

    const templateId = templateDraft.templateId.trim();
    const displayName = templateDraft.displayName.trim();
    if (templateId.length === 0 || displayName.length === 0) {
      pushNotification("error", "templateId 與 displayName 不可為空。");
      return;
    }

    const reminderDaysBeforeExpiry = templateDraft.reminderDaysBeforeExpiryCsv
      .split(",")
      .map((entry) => entry.trim())
      .filter((entry) => entry.length > 0)
      .map((entry) => {
        const parsed = Number(entry);
        if (!Number.isInteger(parsed) || parsed < 0) {
          throw new Error(`提醒天數 \`${entry}\` 無效`);
        }
        return parsed;
      });

    templateSaving = true;
    try {
      await apiClient.admin.upsertComplianceDocumentTemplate(
        templateDraft.vendorCategory,
        templateId,
        {
          displayName,
          required: templateDraft.required,
          maxValidityDays: templateDraft.maxValidityDays,
          reminderDaysBeforeExpiry,
          suspensionGraceDays: templateDraft.suspensionGraceDays
        }
      );
      pushNotification("success", `文件模板 ${templateId} 已更新。`);
      await refreshTemplates(false);
    } catch (error) {
      pushNotification("error", normalizeApiFailure(error).localizedMessage);
    } finally {
      templateSaving = false;
    }
  }

  async function runComplianceLifecycle() {
    if (lifecycleRunning) {
      return;
    }

    lifecycleRunning = true;
    try {
      lifecycleResult = await apiClient.admin.runVendorComplianceLifecycle({
        runDate: lifecycleRunDate,
        dryRun: lifecycleDryRun
      });
      pushNotification(
        "success",
        `生命週期執行完成：提醒 ${lifecycleResult.reminderCount}、停權 ${lifecycleResult.suspensionCount}、復權 ${lifecycleResult.reinstatementCount}。`
      );
      await refreshVendors(false);
    } catch (error) {
      pushNotification("error", normalizeApiFailure(error).localizedMessage);
    } finally {
      lifecycleRunning = false;
    }
  }

  async function refreshMappings(notifyOnError: boolean) {
    if (mappingLoading) {
      return;
    }

    mappingLoading = true;
    mappingError = null;
    try {
      const page = await apiClient.admin.listVendorPlantDeliveryMappings(
        normalizeOptional(mappingVendorFilter) ?? undefined,
        normalizeOptional(mappingPlantFilter) ?? undefined,
        normalizeOptional(mappingActiveAt) ?? undefined,
        1,
        200
      );
      mappings = page.items;
      mappingAuditTrail = page.auditTrail;
      mappingPageMeta = page.page;
    } catch (error) {
      const failure = normalizeApiFailure(error);
      mappingError = failure.localizedMessage;
      if (notifyOnError) {
        pushNotification("error", failure.localizedMessage);
      }
    } finally {
      mappingLoading = false;
    }
  }

  async function submitMappingDraft() {
    if (mappingSaving) {
      return;
    }

    const vendorId = mappingDraft.vendorId.trim();
    const mappingId = mappingDraft.mappingId.trim();
    const plantId = mappingDraft.plantId.trim();
    if (vendorId.length === 0 || mappingId.length === 0 || plantId.length === 0) {
      pushNotification("error", "vendorId、mappingId、plantId 皆為必填。");
      return;
    }

    let startsAt: string;
    let endsAt: string;
    try {
      startsAt = toTaipeiDateTime(mappingDraft.serviceWindowStartsAtLocal);
      endsAt = toTaipeiDateTime(mappingDraft.serviceWindowEndsAtLocal);
    } catch (error) {
      pushNotification("error", error instanceof Error ? error.message : "服務時段格式無效");
      return;
    }

    if (Date.parse(startsAt) >= Date.parse(endsAt)) {
      pushNotification("error", "服務時段結束時間必須晚於開始時間。");
      return;
    }

    mappingSaving = true;
    try {
      await apiClient.admin.upsertVendorPlantDeliveryMapping(vendorId, mappingId, {
        plantId,
        effect: mappingDraft.effect,
        precedence: mappingDraft.precedence,
        serviceWindow: {
          startsAt,
          endsAt
        }
      });
      pushNotification("success", `配送映射 ${mappingId} 已更新。`);
      await refreshMappings(false);
    } catch (error) {
      pushNotification("error", normalizeApiFailure(error).localizedMessage);
    } finally {
      mappingSaving = false;
    }
  }

  async function deleteMapping(vendorId: string, mappingId: string) {
    const key = `${vendorId}:${mappingId}`;
    if (deletingMappingById[key]) {
      return;
    }

    deletingMappingById = {
      ...deletingMappingById,
      [key]: true
    };

    try {
      await apiClient.admin.deleteVendorPlantDeliveryMapping(vendorId, mappingId);
      pushNotification("success", `配送映射 ${mappingId} 已刪除。`);
      await refreshMappings(false);
    } catch (error) {
      pushNotification("error", normalizeApiFailure(error).localizedMessage);
    } finally {
      deletingMappingById = {
        ...deletingMappingById,
        [key]: false
      };
    }
  }

  async function createObjectStorageAccessLink() {
    if (objectStorageLinkLoading) {
      return;
    }

    const objectRef = objectStorageRef.trim();
    if (objectRef.length === 0) {
      pushNotification("error", "請先填寫 objectRef。");
      return;
    }

    objectStorageLinkLoading = true;
    objectStorageLinkError = null;
    objectStorageLinkResult = null;
    try {
      objectStorageLinkResult = await apiClient.admin.createAdminObjectStorageAccessLink({
        objectRef,
        locale: "zh-TW"
      });
      pushNotification("success", "已生成文件存取連結。");
    } catch (error) {
      const failure = normalizeApiFailure(error);
      objectStorageLinkError = failure.localizedMessage;
      pushNotification("error", failure.localizedMessage);
    } finally {
      objectStorageLinkLoading = false;
    }
  }

  async function closeMonthlySettlement() {
    if (settlementClosing) {
      return;
    }

    const checklist = parseIssueChecklist(settlementIssueChecklistRaw);
    if (!isIssueSignOffConfirmed(checklist, SETTLEMENT_RELEASE_SIGN_OFF_ISSUE_ID)) {
      pushNotification(
        "error",
        `結算發佈簽核前必須先套用 ${SETTLEMENT_RELEASE_SIGN_OFF_ISSUE_ID}。`
      );
      return;
    }

    settlementClosing = true;
    settlementError = null;
    try {
      if (
        !Number.isInteger(settlementCloseDraft.pageSize) ||
        settlementCloseDraft.pageSize < 1 ||
        settlementCloseDraft.pageSize > 200
      ) {
        pushNotification("error", "pageSize 必須是 1 到 200 的整數。");
        return;
      }
      const page = await apiClient.admin.closePayrollMonthlySettlement({
        cycleKey: normalizeOptional(settlementCloseDraft.cycleKey) ?? undefined,
        issueChecklist: checklist,
        page: settlementCloseDraft.page,
        pageSize: settlementCloseDraft.pageSize,
        sortBy: settlementCloseDraft.sortBy,
        sortOrder: settlementCloseDraft.sortOrder
      });
      settlementPage = page;
      settlementLockDraft.cycleKey = page.exchangeBatch.cycleKey;
      pushNotification(
        "success",
        `月結關帳完成，批次 ${page.exchangeBatch.batchId} 已建立。`
      );
    } catch (error) {
      const failure = normalizeApiFailure(error);
      settlementError = failure.localizedMessage;
      pushNotification("error", failure.localizedMessage);
    } finally {
      settlementClosing = false;
    }
  }

  async function lockSettlementCycle() {
    if (settlementLocking) {
      return;
    }

    const cycleKey = settlementLockDraft.cycleKey.trim();
    const reason = settlementLockDraft.lockReason.trim();
    if (cycleKey.length === 0 || reason.length === 0) {
      pushNotification("error", "鎖帳 cycleKey 與 reason 皆為必填。");
      return;
    }

    settlementLocking = true;
    try {
      const response = await apiClient.admin.lockPayrollSettlementCycle(cycleKey, {
        reason
      });
      settlementLockState = response.settlementCycle;
      pushNotification("success", `結算週期 ${cycleKey} 已鎖定。`);
    } catch (error) {
      pushNotification("error", normalizeApiFailure(error).localizedMessage);
    } finally {
      settlementLocking = false;
    }
  }

  async function unlockSettlementCycle() {
    if (settlementUnlocking) {
      return;
    }

    const cycleKey = settlementLockDraft.cycleKey.trim();
    const reason = settlementLockDraft.unlockReason.trim();
    if (cycleKey.length === 0 || reason.length === 0) {
      pushNotification("error", "解鎖 cycleKey 與 reason 皆為必填。");
      return;
    }

    settlementUnlocking = true;
    try {
      const response = await apiClient.admin.unlockPayrollSettlementCycle(cycleKey, {
        reason
      });
      settlementLockState = response.settlementCycle;
      pushNotification("success", `結算週期 ${cycleKey} 已解鎖。`);
    } catch (error) {
      pushNotification("error", normalizeApiFailure(error).localizedMessage);
    } finally {
      settlementUnlocking = false;
    }
  }

  async function submitDisputeWorkflow() {
    if (disputeSubmitting) {
      return;
    }

    const disputeId = disputeDraft.disputeId.trim();
    if (disputeId.length === 0) {
      pushNotification("error", "disputeId 為必填。");
      return;
    }

    let payload: Parameters<typeof apiClient.admin.updateAdminPayrollDispute>[1];
    if (disputeDraft.operation === "ASSIGN_OWNER") {
      const ownerActorId = disputeDraft.ownerActorId.trim();
      if (ownerActorId.length === 0) {
        pushNotification("error", "ASSIGN_OWNER 需要 ownerActorId。");
        return;
      }
      payload = {
        operation: "ASSIGN_OWNER",
        ownerActorId,
        note: normalizeOptional(disputeDraft.note) ?? undefined
      };
    } else if (disputeDraft.operation === "RESOLVE_REFUND") {
      const note = disputeDraft.note.trim();
      if (note.length === 0) {
        pushNotification("error", "RESOLVE_REFUND 需要 note。");
        return;
      }
      const refundAmountMinor = parseOptionalNumber(disputeDraft.refundAmountMinor);
      if (refundAmountMinor !== undefined && (!Number.isInteger(refundAmountMinor) || refundAmountMinor < 1)) {
        pushNotification("error", "refundAmountMinor 必須是大於等於 1 的整數。");
        return;
      }
      payload = {
        operation: "RESOLVE_REFUND",
        note,
        refundAmountMinor: refundAmountMinor === undefined ? undefined : Number(refundAmountMinor)
      };
    } else {
      const note = disputeDraft.note.trim();
      if (note.length === 0) {
        pushNotification("error", "RESOLVE_REJECTED 需要 note。");
        return;
      }
      payload = {
        operation: "RESOLVE_REJECTED",
        note
      };
    }

    disputeSubmitting = true;
    try {
      disputeResult = await apiClient.admin.updateAdminPayrollDispute(disputeId, payload);
      pushNotification("success", `爭議 ${disputeId} 已更新為 ${disputeResult.status}。`);
    } catch (error) {
      pushNotification("error", normalizeApiFailure(error).localizedMessage);
    } finally {
      disputeSubmitting = false;
    }
  }

  async function refreshAnomalyAlerts(notifyOnError: boolean) {
    if (anomalyAlertsLoading) {
      return;
    }

    anomalyAlertsLoading = true;
    anomalyAlertsError = null;
    try {
      const alerts = await apiClient.admin.listAnomalyAlerts(
        normalizeOptional(anomalyFilters.vendorId) ?? undefined,
        normalizeOptional(anomalyFilters.ownerActorId) ?? undefined,
        anomalyFilters.status === "ALL" ? undefined : anomalyFilters.status,
        parseBooleanFlag(anomalyFilters.escalatedOnly),
        anomalyFilters.slaStatus === "ALL" ? undefined : anomalyFilters.slaStatus,
        parseOptionalEpochDay(anomalyFilters.asOfEpochDay),
        parseOptionalMinuteOfDay(anomalyFilters.asOfMinuteOfDay)
      );
      anomalyAlerts = alerts.items;
    } catch (error) {
      const failure = normalizeApiFailure(error);
      anomalyAlertsError = failure.localizedMessage;
      if (notifyOnError) {
        pushNotification("error", failure.localizedMessage);
      }
    } finally {
      anomalyAlertsLoading = false;
    }
  }

  async function patchAnomalyAlert() {
    if (anomalyPatchSubmitting) {
      return;
    }

    const alertId = selectedAlertId;
    if (!alertId) {
      pushNotification("error", "請先選擇異常告警。");
      return;
    }

    let payload: Parameters<typeof apiClient.admin.updateAdminAnomalyAlert>[1];

    if (anomalyPatchDraft.operation === "ASSIGN_OWNER") {
      const ownerActorId = anomalyPatchDraft.ownerActorId.trim();
      if (ownerActorId.length === 0) {
        pushNotification("error", "ASSIGN_OWNER 需要 ownerActorId。");
        return;
      }
      payload = {
        operation: "ASSIGN_OWNER",
        ownerActorId,
        note: normalizeOptional(anomalyPatchDraft.note) ?? undefined
      };
    } else if (anomalyPatchDraft.operation === "CLOSE") {
      const checklist = parseIssueChecklist(anomalyIssueChecklistRaw);
      if (!isIssueSignOffConfirmed(checklist, ANOMALY_RELEASE_SIGN_OFF_ISSUE_ID)) {
        pushNotification(
          "error",
          `異常發佈簽核前必須先套用 ${ANOMALY_RELEASE_SIGN_OFF_ISSUE_ID}。`
        );
        return;
      }
      const closureNote = anomalyPatchDraft.closureNote.trim();
      if (closureNote.length === 0) {
        pushNotification("error", "CLOSE 操作需要 closureNote。");
        return;
      }
      const evidenceRefs = parseEvidenceRefsInput(anomalyPatchDraft.closureEvidenceRefsCsv);
      if (evidenceRefs.length === 0) {
        pushNotification("error", "CLOSE 操作至少需要一個 closureEvidenceRef。");
        return;
      }
      payload = {
        operation: "CLOSE",
        issueChecklist: checklist,
        note: normalizeOptional(anomalyPatchDraft.note) ?? undefined,
        closureNote,
        closureEvidenceRefs: evidenceRefs,
        ticketReference: normalizeOptional(anomalyPatchDraft.ticketReference) ?? undefined
      };
    } else {
      payload = {
        operation: anomalyPatchDraft.operation,
        note: normalizeOptional(anomalyPatchDraft.note) ?? undefined
      };
    }

    anomalyPatchSubmitting = true;
    try {
      const updated = await apiClient.admin.updateAdminAnomalyAlert(alertId, payload);
      pushNotification("success", `異常告警 ${updated.alertId} 已更新為 ${updated.status}。`);
      await refreshAnomalyAlerts(false);
    } catch (error) {
      pushNotification("error", normalizeApiFailure(error).localizedMessage);
    } finally {
      anomalyPatchSubmitting = false;
    }
  }

  async function evaluateAnomalyAlerts() {
    if (anomalyEvaluating) {
      return;
    }

    const vendorId = anomalyEvaluationDraft.vendorId.trim();
    if (vendorId.length === 0) {
      pushNotification("error", "評估異常前請輸入 vendorId。");
      return;
    }

    anomalyEvaluating = true;
    try {
      const result = await apiClient.admin.evaluateAnomalyAlerts({
        vendorId,
        defaultOwnerActorId: normalizeOptional(anomalyEvaluationDraft.defaultOwnerActorId) ?? undefined,
        daysUntilExpiry: parseOptionalNumber(anomalyEvaluationDraft.daysUntilExpiry),
        onTimeRate: parseOptionalNumber(anomalyEvaluationDraft.onTimeRate),
        satisfactionScore: parseOptionalNumber(anomalyEvaluationDraft.satisfactionScore),
        complaintCount: parseOptionalNumber(anomalyEvaluationDraft.complaintCount),
        observedAtEpochDay: parseOptionalEpochDay(anomalyEvaluationDraft.observedAtEpochDay),
        observedAtMinuteOfDay: parseOptionalMinuteOfDay(anomalyEvaluationDraft.observedAtMinuteOfDay)
      });
      anomalyEvaluationResult = result.triggeredAlerts;
      pushNotification("success", `異常評估完成，觸發 ${result.triggeredAlerts.length} 筆告警。`);
      await refreshAnomalyAlerts(false);
    } catch (error) {
      pushNotification("error", normalizeApiFailure(error).localizedMessage);
    } finally {
      anomalyEvaluating = false;
    }
  }

  async function refreshAnomalyRules(notifyOnError: boolean) {
    if (anomalyRulesLoading) {
      return;
    }

    anomalyRulesLoading = true;
    anomalyRulesError = null;
    try {
      const rules = await apiClient.admin.listAnomalyRules();
      anomalyRules = rules.items;
    } catch (error) {
      const failure = normalizeApiFailure(error);
      anomalyRulesError = failure.localizedMessage;
      if (notifyOnError) {
        pushNotification("error", failure.localizedMessage);
      }
    } finally {
      anomalyRulesLoading = false;
    }
  }

  async function submitAnomalyRuleDraft() {
    if (anomalyRuleSaving) {
      return;
    }

    const ruleId = anomalyRuleDraft.ruleId.trim();
    const displayName = anomalyRuleDraft.displayName.trim();
    const description = anomalyRuleDraft.description.trim();
    const governanceIssueId = anomalyRuleDraft.governanceIssueId.trim();
    if (ruleId.length === 0 || displayName.length === 0 || description.length === 0 || governanceIssueId.length === 0) {
      pushNotification("error", "ruleId / displayName / description / governanceIssueId 不可為空。");
      return;
    }

    let thresholdValue: number;
    let evaluationWindowDays: number;
    let slaMinutes: number;
    try {
      thresholdValue = parseRequiredNumber(anomalyRuleDraft.thresholdValue, "thresholdValue");
      evaluationWindowDays = parseRequiredNumber(
        anomalyRuleDraft.evaluationWindowDays,
        "evaluationWindowDays"
      );
      slaMinutes = parseRequiredNumber(anomalyRuleDraft.slaMinutes, "slaMinutes");
    } catch (error) {
      pushNotification("error", error instanceof Error ? error.message : "異常規則參數格式無效");
      return;
    }

    if (!Number.isInteger(evaluationWindowDays) || evaluationWindowDays <= 0) {
      pushNotification("error", "evaluationWindowDays 必須是正整數。");
      return;
    }
    if (!Number.isInteger(slaMinutes) || slaMinutes <= 0) {
      pushNotification("error", "slaMinutes 必須是正整數。");
      return;
    }

    anomalyRuleSaving = true;
    try {
      await apiClient.admin.upsertAnomalyRule(ruleId, {
        kind: anomalyRuleDraft.kind,
        displayName,
        description,
        governanceIssueId,
        enabled: anomalyRuleDraft.enabled,
        thresholdValue,
        thresholdComparator: anomalyRuleDraft.thresholdComparator,
        evaluationWindowDays,
        slaMinutes,
        severity: anomalyRuleDraft.severity
      });
      pushNotification("success", `異常規則 ${ruleId} 已更新。`);
      await refreshAnomalyRules(false);
    } catch (error) {
      pushNotification("error", normalizeApiFailure(error).localizedMessage);
    } finally {
      anomalyRuleSaving = false;
    }
  }

  async function refreshAuditViews(notifyOnError: boolean) {
    if (auditLoading) {
      return;
    }

    let occurredFromEpochDay: number | undefined;
    let occurredToEpochDay: number | undefined;
    try {
      occurredFromEpochDay = parseOptionalEpochDay(auditFilters.occurredFromEpochDay);
      occurredToEpochDay = parseOptionalEpochDay(auditFilters.occurredToEpochDay);
    } catch (error) {
      pushNotification("error", error instanceof Error ? error.message : "稽核日期參數無效");
      return;
    }

    auditLoading = true;
    auditError = null;
    try {
      const [investigations, responsibilities] = await Promise.all([
        apiClient.admin.queryAuditInvestigations(
          normalizeOptional(auditFilters.actorId) ?? undefined,
          auditFilters.action === "ALL" ? undefined : auditFilters.action,
          auditFilters.entityType === "ALL" ? undefined : auditFilters.entityType,
          normalizeOptional(auditFilters.entityId) ?? undefined,
          occurredFromEpochDay,
          occurredToEpochDay,
          normalizeOptional(auditFilters.correlationId) ?? undefined
        ),
        apiClient.admin.queryAuditResponsibilities(
          normalizeOptional(auditFilters.actorId) ?? undefined,
          auditFilters.action === "ALL" ? undefined : auditFilters.action,
          auditFilters.entityType === "ALL" ? undefined : auditFilters.entityType,
          normalizeOptional(auditFilters.entityId) ?? undefined,
          occurredFromEpochDay,
          occurredToEpochDay,
          normalizeOptional(auditFilters.correlationId) ?? undefined
        )
      ]);
      auditEvidenceItems = investigations.items;
      auditResponsibilityItems = responsibilities.items;
    } catch (error) {
      const failure = normalizeApiFailure(error);
      auditError = failure.localizedMessage;
      if (notifyOnError) {
        pushNotification("error", failure.localizedMessage);
      }
    } finally {
      auditLoading = false;
    }
  }

  async function refreshAnalyticsDashboard(notifyOnError: boolean) {
    if (analyticsLoading) {
      return;
    }

    analyticsLoading = true;
    analyticsError = null;
    try {
      const dashboard = await apiClient.admin.getAdminOperationsAnalyticsDashboard(
        parseOptionalEpochDay(analyticsFilters.fromEpochDay),
        parseOptionalEpochDay(analyticsFilters.toEpochDay)
      );
      analyticsDashboard = dashboard;
    } catch (error) {
      const failure = normalizeApiFailure(error);
      analyticsError = failure.localizedMessage;
      if (notifyOnError) {
        pushNotification("error", failure.localizedMessage);
      }
    } finally {
      analyticsLoading = false;
    }
  }

  function mapSettlementExceptionClass(
    record: SettlementRecord
  ): (typeof settlementExceptionClassOptions)[number] | null {
    if (record.status === "DISPUTED") {
      return "DISPUTED";
    }
    if (record.status === "DEDUCTION_FAILED") {
      return "DEDUCTION_FAILED";
    }
    if (record.status === "EMPLOYEE_TERMINATED") {
      return "EMPLOYEE_TERMINATED";
    }
    if (record.status === "REFUNDED") {
      return "REFUNDED";
    }
    return null;
  }

  function settlementRecordIsException(record: SettlementRecord): boolean {
    return mapSettlementExceptionClass(record) !== null;
  }

  function formatIssueChecklistLabel(value: string): string {
    const normalized = parseIssueChecklist(value);
    if (normalized.length === 0) {
      return "尚未填寫";
    }
    return normalized.join("、");
  }

  function pushNotification(kind: PortalNotification["kind"], message: string) {
    const id = nextNotificationId;
    nextNotificationId += 1;
    notifications = [...notifications, { id, kind, message }];

    const timeout = setTimeout(() => {
      notifications = notifications.filter((notification) => notification.id !== id);
      notificationTimeouts.delete(timeout);
    }, 5200);

    notificationTimeouts.add(timeout);
  }

  function normalizeOptional(value: string): string | null {
    const trimmed = value.trim();
    return trimmed.length === 0 ? null : trimmed;
  }

  function formatMetricValue(metricKey: string, value: number): string {
    if (metricKey.endsWith("_rate")) {
      return `${(value * 100).toFixed(2)}%`;
    }
    if (metricKey.endsWith("_score")) {
      return value.toFixed(2);
    }
    return Number.isInteger(value) ? `${value}` : value.toFixed(2);
  }

  function epochSecondsToTaipeiDateTime(epochSeconds: number): string {
    return formatTaipeiDateTime(new Date(epochSeconds * 1000).toISOString());
  }

  function sectionVisible(label: "compliance" | "settlement" | "anomaly" | "audit" | "analytics"): boolean {
    if (sectionId === "overview") {
      return true;
    }

    if (label === "compliance") {
      return sectionId === "vendors";
    }
    if (label === "settlement") {
      return sectionId === "settlement";
    }
    if (label === "anomaly") {
      return sectionId === "anomalies";
    }
    if (label === "audit") {
      return sectionId === "audit";
    }
    return sectionId === "analytics";
  }
</script>

<section class="grid gap-6">
  <header class="rounded-xl border border-indigo-100 bg-indigo-50/60 p-4">
    <p class="text-xs font-semibold tracking-[0.14em] text-indigo-800">福委會 Admin Portal MVP</p>
    <h2 class="mt-1 text-xl font-bold text-slate-950">{sectionTitle}</h2>
    <p class="mt-2 text-sm text-slate-700">{sectionDescription}</p>
  </header>

  <div class="grid gap-3 md:grid-cols-2 xl:grid-cols-4">
    <article class="rounded-xl border border-slate-200 bg-white p-4 shadow-sm">
      <h3 class="text-xs font-semibold tracking-[0.08em] text-slate-500">執行身份</h3>
      <p class="mt-2 text-sm text-slate-800">{actorDisplayName} ({actorId})</p>
      <p class="mt-1 text-xs text-slate-600">{provider}</p>
    </article>
    <article class="rounded-xl border border-slate-200 bg-white p-4 shadow-sm">
      <h3 class="text-xs font-semibold tracking-[0.08em] text-slate-500">待審核商家</h3>
      <p class="mt-2 text-2xl font-bold text-amber-700">{pendingVendorCount}</p>
      <p class="text-xs text-slate-600">停權中 {suspendedVendorCount}</p>
    </article>
    <article class="rounded-xl border border-slate-200 bg-white p-4 shadow-sm">
      <h3 class="text-xs font-semibold tracking-[0.08em] text-slate-500">異常告警</h3>
      <p class="mt-2 text-2xl font-bold text-rose-700">{openAnomalyCount}</p>
      <p class="text-xs text-slate-600">SLA breach {breachedAnomalyCount}</p>
    </article>
    <article class="rounded-xl border border-slate-200 bg-white p-4 shadow-sm">
      <h3 class="text-xs font-semibold tracking-[0.08em] text-slate-500">結算例外</h3>
      <p class="mt-2 text-2xl font-bold text-cyan-700">{settlementExceptionRecords.length}</p>
      <p class="text-xs text-slate-600">最近關帳批次例外筆數</p>
    </article>
  </div>

  {#if notifications.length > 0}
    <div class="grid gap-2" role="status" aria-live="polite">
      {#each notifications as notification (notification.id)}
        <p
          class={`rounded-lg border px-3 py-2 text-sm ${notification.kind === "success" ? "border-emerald-200 bg-emerald-50 text-emerald-900" : notification.kind === "error" ? "border-rose-200 bg-rose-50 text-rose-900" : "border-sky-200 bg-sky-50 text-sky-900"}`}
        >
          {notification.message}
        </p>
      {/each}
    </div>
  {/if}

  {#if sectionVisible("compliance")}
    <section class="grid gap-4 rounded-xl border border-slate-200 bg-white p-4 shadow-sm">
      <header>
        <h3 class="text-lg font-semibold text-slate-900">商家審核、文件生命週期與廠區映射</h3>
        <p class="text-sm text-slate-600">
          覆蓋審核決策、文件模板策略、生命週期執行、配送映射與文件 objectRef 存取。
        </p>
      </header>

      <div class="grid gap-4 lg:grid-cols-2">
        <article class="grid gap-3 rounded-lg border border-slate-200 p-3">
          <h4 class="text-sm font-semibold text-slate-800">商家清單與審核決策</h4>
          <div class="grid gap-2 md:grid-cols-3">
            <label class="grid gap-1 text-xs text-slate-600">
              狀態篩選
              <select bind:value={vendorStatusFilter} class="rounded-md border border-slate-300 px-2 py-2 text-sm text-slate-800">
                <option value="ALL">ALL</option>
                {#each vendorStatusOptions as status}
                  <option value={status}>{status}</option>
                {/each}
              </select>
            </label>
            <label class="grid gap-1 text-xs text-slate-600">
              排序欄位
              <select bind:value={vendorSortBy} class="rounded-md border border-slate-300 px-2 py-2 text-sm text-slate-800">
                {#each vendorSortFieldOptions as option}
                  <option value={option}>{option}</option>
                {/each}
              </select>
            </label>
            <label class="grid gap-1 text-xs text-slate-600">
              排序方向
              <select bind:value={vendorSortOrder} class="rounded-md border border-slate-300 px-2 py-2 text-sm text-slate-800">
                <option value="asc">asc</option>
                <option value="desc">desc</option>
              </select>
            </label>
          </div>
          <div class="flex flex-wrap gap-2">
            <button type="button" class="rounded-md bg-indigo-700 px-3 py-2 text-xs font-semibold text-white" disabled={vendorLoading} onclick={() => refreshVendors(true)}>
              {vendorLoading ? "載入中..." : "重新載入商家"}
            </button>
            {#if vendorPageMeta}
              <p class="self-center text-xs text-slate-500">
                page {vendorPageMeta.page}/{vendorPageMeta.totalPages}，共 {vendorPageMeta.totalItems} 筆
              </p>
            {/if}
          </div>
          {#if vendorError}
            <p class="text-xs text-rose-700">{vendorError}</p>
          {/if}

          <label class="grid gap-1 text-xs text-slate-600">
            目標商家
            <select bind:value={selectedVendorId} class="rounded-md border border-slate-300 px-2 py-2 text-sm text-slate-800">
              {#if vendors.length === 0}
                <option value={null}>目前無資料</option>
              {/if}
              {#each vendors as vendor}
                <option value={vendor.vendorId}>
                  {vendor.vendorId} | {vendor.displayName} | {vendor.status}
                </option>
              {/each}
            </select>
          </label>

          {#if selectedVendor}
            <div class="grid gap-1 rounded-md border border-slate-200 bg-slate-50 p-3 text-xs text-slate-700">
              <p><span class="font-semibold">Category:</span> {selectedVendor.vendorCategory}</p>
              <p><span class="font-semibold">Updated:</span> {formatTaipeiDateTime(selectedVendor.updatedAt)}</p>
              <p><span class="font-semibold">Documents:</span> {selectedVendor.compliance.documents.length}</p>
              <p><span class="font-semibold">Lifecycle History:</span> {selectedVendor.compliance.lifecycleHistory.length}</p>
              <p>
                <span class="font-semibold">Retention:</span>
                review {selectedVendor.compliance.retentionPolicy.reviewHistoryDays}d /
                lifecycle {selectedVendor.compliance.retentionPolicy.lifecycleHistoryDays}d
              </p>
            </div>

            <div class="grid gap-2">
              <h5 class="text-xs font-semibold tracking-[0.08em] text-slate-500">審核歷程</h5>
              <div class="max-h-36 overflow-auto rounded-md border border-slate-200">
                <table class="min-w-full text-left text-xs">
                  <thead class="bg-slate-50 text-slate-600">
                    <tr>
                      <th class="px-2 py-1">Time</th>
                      <th class="px-2 py-1">Decision</th>
                      <th class="px-2 py-1">Actor</th>
                      <th class="px-2 py-1">Comment</th>
                    </tr>
                  </thead>
                  <tbody>
                    {#if selectedVendor.reviewHistory.length === 0}
                      <tr>
                        <td colspan="4" class="px-2 py-2 text-slate-500">尚無審核歷程</td>
                      </tr>
                    {:else}
                      {#each selectedVendor.reviewHistory as history}
                        <tr class="border-t border-slate-100">
                          <td class="px-2 py-1">{formatTaipeiDateTime(history.decidedAt)}</td>
                          <td class="px-2 py-1">{history.decision}</td>
                          <td class="px-2 py-1">{history.decidedByActorId}</td>
                          <td class="px-2 py-1">{history.comment}</td>
                        </tr>
                      {/each}
                    {/if}
                  </tbody>
                </table>
              </div>
            </div>

            <div class="grid gap-2 rounded-md border border-slate-200 p-3">
              <h5 class="text-xs font-semibold tracking-[0.08em] text-slate-500">提交審核決策</h5>
              <div class="grid gap-2 md:grid-cols-2">
                <label class="grid gap-1 text-xs text-slate-600">
                  Decision
                  <select bind:value={reviewDecision} class="rounded-md border border-slate-300 px-2 py-2 text-sm text-slate-800">
                    {#each vendorReviewDecisionOptions as decision}
                      <option value={decision}>{decision}</option>
                    {/each}
                  </select>
                </label>
                <label class="grid gap-1 text-xs text-slate-600 md:col-span-1">
                  Comment
                  <input bind:value={reviewComment} class="rounded-md border border-slate-300 px-2 py-2 text-sm text-slate-800" />
                </label>
              </div>
              <button type="button" class="rounded-md bg-emerald-700 px-3 py-2 text-xs font-semibold text-white" disabled={reviewSubmitting} onclick={submitVendorReview}>
                {reviewSubmitting ? "提交中..." : "提交審核決策"}
              </button>
            </div>

            <div class="grid gap-2">
              <h5 class="text-xs font-semibold tracking-[0.08em] text-slate-500">文件與停權訊號</h5>
              <div class="max-h-36 overflow-auto rounded-md border border-slate-200">
                <table class="min-w-full text-left text-xs">
                  <thead class="bg-slate-50 text-slate-600">
                    <tr>
                      <th class="px-2 py-1">Template</th>
                      <th class="px-2 py-1">Status</th>
                      <th class="px-2 py-1">Expires</th>
                      <th class="px-2 py-1">Object Ref</th>
                    </tr>
                  </thead>
                  <tbody>
                    {#if selectedVendor.compliance.documents.length === 0}
                      <tr>
                        <td colspan="4" class="px-2 py-2 text-slate-500">目前沒有文件紀錄</td>
                      </tr>
                    {:else}
                      {#each selectedVendor.compliance.documents as document}
                        <tr class="border-t border-slate-100">
                          <td class="px-2 py-1">{document.templateId}</td>
                          <td class="px-2 py-1">{document.status}</td>
                          <td class="px-2 py-1">{formatTaipeiDateTime(document.expiresOn)}</td>
                          <td class="px-2 py-1 break-all">{document.documentRef}</td>
                        </tr>
                      {/each}
                    {/if}
                  </tbody>
                </table>
              </div>
            </div>
          {/if}
        </article>

        <article class="grid gap-3 rounded-lg border border-slate-200 p-3">
          <h4 class="text-sm font-semibold text-slate-800">文件模板、生命週期與映射維護</h4>

          <div class="grid gap-2 rounded-md border border-slate-200 p-3">
            <h5 class="text-xs font-semibold tracking-[0.08em] text-slate-500">文件模板策略</h5>
            <div class="grid gap-2 md:grid-cols-2">
              <label class="grid gap-1 text-xs text-slate-600">
                Category Filter
                <select bind:value={templateCategoryFilter} class="rounded-md border border-slate-300 px-2 py-2 text-sm text-slate-800">
                  <option value="ALL">ALL</option>
                  {#each vendorCategoryOptions as category}
                    <option value={category}>{category}</option>
                  {/each}
                </select>
              </label>
              <div class="flex items-end">
                <button type="button" class="rounded-md bg-indigo-700 px-3 py-2 text-xs font-semibold text-white" disabled={templateLoading} onclick={() => refreshTemplates(true)}>
                  {templateLoading ? "載入中..." : "重新載入模板"}
                </button>
              </div>
            </div>
            {#if templateError}
              <p class="text-xs text-rose-700">{templateError}</p>
            {/if}
            <div class="max-h-28 overflow-auto rounded-md border border-slate-200">
              <table class="min-w-full text-left text-xs">
                <thead class="bg-slate-50 text-slate-600">
                  <tr>
                    <th class="px-2 py-1">Template</th>
                    <th class="px-2 py-1">Category</th>
                    <th class="px-2 py-1">Required</th>
                    <th class="px-2 py-1">Validity</th>
                  </tr>
                </thead>
                <tbody>
                  {#if templateItems.length === 0}
                    <tr>
                      <td colspan="4" class="px-2 py-2 text-slate-500">尚無模板資料</td>
                    </tr>
                  {:else}
                    {#each templateItems as template}
                      <tr class="border-t border-slate-100">
                        <td class="px-2 py-1">{template.templateId}</td>
                        <td class="px-2 py-1">{template.vendorCategory}</td>
                        <td class="px-2 py-1">{template.required ? "YES" : "NO"}</td>
                        <td class="px-2 py-1">{template.maxValidityDays}d</td>
                      </tr>
                    {/each}
                  {/if}
                </tbody>
              </table>
            </div>
            {#if templatePageMeta}
              <p class="text-xs text-slate-500">
                page {templatePageMeta.page}/{templatePageMeta.totalPages}，共 {templatePageMeta.totalItems} 筆
              </p>
            {/if}

            <div class="grid gap-2 rounded-md border border-slate-200 p-3">
              <div class="grid gap-2 md:grid-cols-2">
                <label class="grid gap-1 text-xs text-slate-600">
                  Vendor Category
                  <select bind:value={templateDraft.vendorCategory} class="rounded-md border border-slate-300 px-2 py-2 text-sm text-slate-800">
                    {#each vendorCategoryOptions as category}
                      <option value={category}>{category}</option>
                    {/each}
                  </select>
                </label>
                <label class="grid gap-1 text-xs text-slate-600">
                  Template ID
                  <input bind:value={templateDraft.templateId} class="rounded-md border border-slate-300 px-2 py-2 text-sm text-slate-800" />
                </label>
                <label class="grid gap-1 text-xs text-slate-600 md:col-span-2">
                  Display Name
                  <input bind:value={templateDraft.displayName} class="rounded-md border border-slate-300 px-2 py-2 text-sm text-slate-800" />
                </label>
                <label class="grid gap-1 text-xs text-slate-600">
                  Max Validity Days
                  <input type="number" bind:value={templateDraft.maxValidityDays} min="1" class="rounded-md border border-slate-300 px-2 py-2 text-sm text-slate-800" />
                </label>
                <label class="grid gap-1 text-xs text-slate-600">
                  Suspension Grace Days
                  <input type="number" bind:value={templateDraft.suspensionGraceDays} min="0" class="rounded-md border border-slate-300 px-2 py-2 text-sm text-slate-800" />
                </label>
                <label class="grid gap-1 text-xs text-slate-600 md:col-span-2">
                  Reminder Days (CSV)
                  <input bind:value={templateDraft.reminderDaysBeforeExpiryCsv} class="rounded-md border border-slate-300 px-2 py-2 text-sm text-slate-800" />
                </label>
              </div>
              <label class="inline-flex items-center gap-2 text-xs text-slate-700">
                <input type="checkbox" bind:checked={templateDraft.required} />
                Required document
              </label>
              <button type="button" class="rounded-md bg-emerald-700 px-3 py-2 text-xs font-semibold text-white" disabled={templateSaving} onclick={submitTemplateDraft}>
                {templateSaving ? "儲存中..." : "儲存模板策略"}
              </button>
            </div>
          </div>

          <div class="grid gap-2 rounded-md border border-slate-200 p-3">
            <h5 class="text-xs font-semibold tracking-[0.08em] text-slate-500">生命週期執行</h5>
            <div class="grid gap-2 md:grid-cols-2">
              <label class="grid gap-1 text-xs text-slate-600">
                Run Date
                <input type="date" bind:value={lifecycleRunDate} class="rounded-md border border-slate-300 px-2 py-2 text-sm text-slate-800" />
              </label>
              <label class="inline-flex items-center gap-2 text-xs text-slate-700 md:mt-6">
                <input type="checkbox" bind:checked={lifecycleDryRun} />
                Dry run
              </label>
            </div>
            <button type="button" class="rounded-md bg-indigo-700 px-3 py-2 text-xs font-semibold text-white" disabled={lifecycleRunning} onclick={runComplianceLifecycle}>
              {lifecycleRunning ? "執行中..." : "執行生命週期"}
            </button>
            {#if lifecycleResult}
              <p class="text-xs text-slate-700">
                runDate {lifecycleResult.runDate}｜提醒 {lifecycleResult.reminderCount}｜停權 {lifecycleResult.suspensionCount}｜復權 {lifecycleResult.reinstatementCount}
              </p>
            {/if}
          </div>

          <div class="grid gap-2 rounded-md border border-slate-200 p-3">
            <h5 class="text-xs font-semibold tracking-[0.08em] text-slate-500">商家 x 廠區映射</h5>
            <div class="grid gap-2 md:grid-cols-3">
              <label class="grid gap-1 text-xs text-slate-600">
                vendorId filter
                <input bind:value={mappingVendorFilter} class="rounded-md border border-slate-300 px-2 py-2 text-sm text-slate-800" />
              </label>
              <label class="grid gap-1 text-xs text-slate-600">
                plantId filter
                <input bind:value={mappingPlantFilter} class="rounded-md border border-slate-300 px-2 py-2 text-sm text-slate-800" />
              </label>
              <label class="grid gap-1 text-xs text-slate-600">
                activeAt (+08:00)
                <input bind:value={mappingActiveAt} placeholder="2026-04-17T12:00:00+08:00" class="rounded-md border border-slate-300 px-2 py-2 text-sm text-slate-800" />
              </label>
            </div>
            <div class="flex flex-wrap gap-2">
              <button type="button" class="rounded-md bg-indigo-700 px-3 py-2 text-xs font-semibold text-white" disabled={mappingLoading} onclick={() => refreshMappings(true)}>
                {mappingLoading ? "載入中..." : "重新載入映射"}
              </button>
              {#if mappingPageMeta}
                <p class="self-center text-xs text-slate-500">
                  page {mappingPageMeta.page}/{mappingPageMeta.totalPages}，共 {mappingPageMeta.totalItems} 筆
                </p>
              {/if}
            </div>
            {#if mappingError}
              <p class="text-xs text-rose-700">{mappingError}</p>
            {/if}
            <div class="max-h-32 overflow-auto rounded-md border border-slate-200">
              <table class="min-w-full text-left text-xs">
                <thead class="bg-slate-50 text-slate-600">
                  <tr>
                    <th class="px-2 py-1">mappingId</th>
                    <th class="px-2 py-1">vendor</th>
                    <th class="px-2 py-1">plant</th>
                    <th class="px-2 py-1">effect</th>
                    <th class="px-2 py-1">precedence</th>
                    <th class="px-2 py-1">window</th>
                    <th class="px-2 py-1">action</th>
                  </tr>
                </thead>
                <tbody>
                  {#if mappings.length === 0}
                    <tr>
                      <td colspan="7" class="px-2 py-2 text-slate-500">尚無映射資料</td>
                    </tr>
                  {:else}
                    {#each mappings as mapping}
                      {@const deleteKey = `${mapping.vendorId}:${mapping.mappingId}`}
                      <tr class="border-t border-slate-100">
                        <td class="px-2 py-1">{mapping.mappingId}</td>
                        <td class="px-2 py-1">{mapping.vendorId}</td>
                        <td class="px-2 py-1">{mapping.plantId}</td>
                        <td class="px-2 py-1">{mapping.effect}</td>
                        <td class="px-2 py-1">{mapping.precedence}</td>
                        <td class="px-2 py-1">
                          {formatTaipeiDateTime(mapping.serviceWindow.startsAt)} ~ {formatTaipeiDateTime(mapping.serviceWindow.endsAt)}
                        </td>
                        <td class="px-2 py-1">
                          <button type="button" class="rounded border border-rose-300 px-2 py-1 text-[11px] font-semibold text-rose-700" disabled={deletingMappingById[deleteKey]} onclick={() => deleteMapping(mapping.vendorId, mapping.mappingId)}>
                            {deletingMappingById[deleteKey] ? "刪除中" : "刪除"}
                          </button>
                        </td>
                      </tr>
                    {/each}
                  {/if}
                </tbody>
              </table>
            </div>

            <div class="grid gap-2 rounded-md border border-slate-200 p-3">
              <div class="grid gap-2 md:grid-cols-2">
                <label class="grid gap-1 text-xs text-slate-600">
                  vendorId
                  <input bind:value={mappingDraft.vendorId} class="rounded-md border border-slate-300 px-2 py-2 text-sm text-slate-800" />
                </label>
                <label class="grid gap-1 text-xs text-slate-600">
                  mappingId
                  <input bind:value={mappingDraft.mappingId} class="rounded-md border border-slate-300 px-2 py-2 text-sm text-slate-800" />
                </label>
                <label class="grid gap-1 text-xs text-slate-600">
                  plantId
                  <input bind:value={mappingDraft.plantId} class="rounded-md border border-slate-300 px-2 py-2 text-sm text-slate-800" />
                </label>
                <label class="grid gap-1 text-xs text-slate-600">
                  effect
                  <select bind:value={mappingDraft.effect} class="rounded-md border border-slate-300 px-2 py-2 text-sm text-slate-800">
                    {#each mappingEffectOptions as effect}
                      <option value={effect}>{effect}</option>
                    {/each}
                  </select>
                </label>
                <label class="grid gap-1 text-xs text-slate-600">
                  precedence
                  <input type="number" bind:value={mappingDraft.precedence} min="0" class="rounded-md border border-slate-300 px-2 py-2 text-sm text-slate-800" />
                </label>
                <label class="grid gap-1 text-xs text-slate-600">
                  serviceWindow startsAt
                  <input type="datetime-local" bind:value={mappingDraft.serviceWindowStartsAtLocal} class="rounded-md border border-slate-300 px-2 py-2 text-sm text-slate-800" />
                </label>
                <label class="grid gap-1 text-xs text-slate-600 md:col-span-2">
                  serviceWindow endsAt
                  <input type="datetime-local" bind:value={mappingDraft.serviceWindowEndsAtLocal} class="rounded-md border border-slate-300 px-2 py-2 text-sm text-slate-800" />
                </label>
              </div>
              <button type="button" class="rounded-md bg-emerald-700 px-3 py-2 text-xs font-semibold text-white" disabled={mappingSaving} onclick={submitMappingDraft}>
                {mappingSaving ? "儲存中..." : "儲存映射"}
              </button>
            </div>

            <div class="grid gap-2 rounded-md border border-slate-200 p-3">
              <h5 class="text-xs font-semibold tracking-[0.08em] text-slate-500">文件 objectRef 取用</h5>
              <label class="grid gap-1 text-xs text-slate-600">
                objectRef
                <input bind:value={objectStorageRef} placeholder="s3://..." class="rounded-md border border-slate-300 px-2 py-2 text-sm text-slate-800" />
              </label>
              <button type="button" class="rounded-md bg-indigo-700 px-3 py-2 text-xs font-semibold text-white" disabled={objectStorageLinkLoading} onclick={createObjectStorageAccessLink}>
                {objectStorageLinkLoading ? "產生中..." : "產生下載連結"}
              </button>
              {#if objectStorageLinkError}
                <p class="text-xs text-rose-700">{objectStorageLinkError}</p>
              {/if}
              {#if objectStorageLinkResult}
                <p class="text-xs text-slate-700 break-all">
                  {objectStorageLinkResult.objectRef}<br />
                  expires: {epochSecondsToTaipeiDateTime(objectStorageLinkResult.downloadExpiresAtEpochSeconds)}
                </p>
                <a class="text-xs font-semibold text-indigo-700 underline" target="_blank" rel="noreferrer" href={objectStorageLinkResult.downloadUrl}>
                  開啟下載連結
                </a>
              {/if}
            </div>

            {#if mappingAuditTrail.length > 0}
              <div class="grid gap-1">
                <h5 class="text-xs font-semibold tracking-[0.08em] text-slate-500">映射稽核軌跡（最新）</h5>
                <div class="max-h-28 overflow-auto rounded-md border border-slate-200">
                  <table class="min-w-full text-left text-xs">
                    <thead class="bg-slate-50 text-slate-600">
                      <tr>
                        <th class="px-2 py-1">Time</th>
                        <th class="px-2 py-1">Event</th>
                        <th class="px-2 py-1">Actor</th>
                        <th class="px-2 py-1">Mapping</th>
                      </tr>
                    </thead>
                    <tbody>
                      {#each mappingAuditTrail as auditEntry}
                        <tr class="border-t border-slate-100">
                          <td class="px-2 py-1">{formatTaipeiDateTime(auditEntry.occurredAt)}</td>
                          <td class="px-2 py-1">{auditEntry.eventType}</td>
                          <td class="px-2 py-1">{auditEntry.actorId} ({auditEntry.actorRole})</td>
                          <td class="px-2 py-1">{auditEntry.mapping.mappingId}</td>
                        </tr>
                      {/each}
                    </tbody>
                  </table>
                </div>
              </div>
            {/if}
          </div>
        </article>
      </div>
    </section>
  {/if}

  {#if sectionVisible("settlement")}
    <section class="grid gap-4 rounded-xl border border-slate-200 bg-white p-4 shadow-sm">
      <header>
        <h3 class="text-lg font-semibold text-slate-900">月結鎖帳、例外處理與爭議流程</h3>
        <p class="text-sm text-slate-600">
          關帳簽核需先套用決策議題 `{SETTLEMENT_RELEASE_SIGN_OFF_ISSUE_ID}`，再執行 lock/unlock/exception/dispute 操作。
        </p>
      </header>

      <div class="grid gap-4 lg:grid-cols-2">
        <article class="grid gap-3 rounded-lg border border-slate-200 p-3">
          <h4 class="text-sm font-semibold text-slate-800">關帳簽核與例外檢視</h4>
          <label class="grid gap-1 text-xs text-slate-600">
            套用議題清單（CSV 或換行）
            <textarea rows="2" bind:value={settlementIssueChecklistRaw} placeholder="ISS-003" class="rounded-md border border-slate-300 px-2 py-2 text-sm text-slate-800"></textarea>
          </label>
          <p class="text-xs text-slate-500">目前簽核議題：{formatIssueChecklistLabel(settlementIssueChecklistRaw)}</p>

          <div class="grid gap-2 md:grid-cols-2">
            <label class="grid gap-1 text-xs text-slate-600">
              cycleKey（選填）
              <input bind:value={settlementCloseDraft.cycleKey} placeholder="monthly-2026-03" class="rounded-md border border-slate-300 px-2 py-2 text-sm text-slate-800" />
            </label>
            <label class="grid gap-1 text-xs text-slate-600">
              pageSize
              <input type="number" bind:value={settlementCloseDraft.pageSize} min="1" max="200" class="rounded-md border border-slate-300 px-2 py-2 text-sm text-slate-800" />
            </label>
            <label class="grid gap-1 text-xs text-slate-600">
              sortBy
              <select bind:value={settlementCloseDraft.sortBy} class="rounded-md border border-slate-300 px-2 py-2 text-sm text-slate-800">
                {#each settlementSortFieldOptions as sortBy}
                  <option value={sortBy}>{sortBy}</option>
                {/each}
              </select>
            </label>
            <label class="grid gap-1 text-xs text-slate-600">
              sortOrder
              <select bind:value={settlementCloseDraft.sortOrder} class="rounded-md border border-slate-300 px-2 py-2 text-sm text-slate-800">
                <option value="asc">asc</option>
                <option value="desc">desc</option>
              </select>
            </label>
          </div>

          <button type="button" class="rounded-md bg-emerald-700 px-3 py-2 text-xs font-semibold text-white" disabled={settlementClosing} onclick={closeMonthlySettlement}>
            {settlementClosing ? "關帳中..." : "執行月結關帳"}
          </button>

          {#if settlementError}
            <p class="text-xs text-rose-700">{settlementError}</p>
          {/if}

          {#if settlementPage}
            <div class="grid gap-1 rounded-md border border-slate-200 bg-slate-50 p-3 text-xs text-slate-700">
              <p><span class="font-semibold">Batch:</span> {settlementPage.exchangeBatch.batchId}</p>
              <p><span class="font-semibold">Cycle:</span> {settlementPage.exchangeBatch.cycleKey}</p>
              <p><span class="font-semibold">Pay Period:</span> {settlementPage.exchangeBatch.payPeriod}</p>
              <p>
                <span class="font-semibold">Records:</span>
                total {settlementPage.exchangeBatch.reconciliation.totalRecords} /
                disputed {settlementPage.exchangeBatch.reconciliation.disputedRecords} /
                failed {settlementPage.exchangeBatch.reconciliation.deductionFailedRecords}
              </p>
              <p>
                <span class="font-semibold">Required Exceptions:</span>
                {settlementPage.exchangeBatch.reconciliation.requiredExceptionClasses.join(", ")}
              </p>
              <p>
                <span class="font-semibold">Present Exceptions:</span>
                {settlementPage.exchangeBatch.reconciliation.presentExceptionClasses.join(", ") || "none"}
              </p>
            </div>

            <label class="grid gap-1 text-xs text-slate-600">
              例外類型篩選
              <select bind:value={settlementExceptionClassFilter} class="rounded-md border border-slate-300 px-2 py-2 text-sm text-slate-800">
                <option value="ALL">ALL</option>
                {#each settlementExceptionClassOptions as exceptionClass}
                  <option value={exceptionClass}>{exceptionClass}</option>
                {/each}
              </select>
            </label>

            <div class="max-h-52 overflow-auto rounded-md border border-slate-200">
              <table class="min-w-full text-left text-xs">
                <thead class="bg-slate-50 text-slate-600">
                  <tr>
                    <th class="px-2 py-1">status</th>
                    <th class="px-2 py-1">dispute</th>
                    <th class="px-2 py-1">deliveryDate</th>
                    <th class="px-2 py-1">cipher(order/employee/amount)</th>
                  </tr>
                </thead>
                <tbody>
                  {#if settlementExceptionRecords.length === 0}
                    <tr>
                      <td colspan="4" class="px-2 py-2 text-slate-500">目前沒有符合條件的例外資料</td>
                    </tr>
                  {:else}
                    {#each settlementExceptionRecords as record}
                      <tr class="border-t border-slate-100">
                        <td class="px-2 py-1">{record.status}</td>
                        <td class="px-2 py-1">{record.disputeStatus ?? "-"}</td>
                        <td class="px-2 py-1">{record.deliveryDate}</td>
                        <td class="px-2 py-1">
                          <p class="truncate max-w-[24rem]">ord: {record.orderIdCiphertext}</p>
                          <p class="truncate max-w-[24rem]">emp: {record.employeeActorCiphertext}</p>
                          <p class="truncate max-w-[24rem]">amt: {record.amountCiphertext}</p>
                        </td>
                      </tr>
                    {/each}
                  {/if}
                </tbody>
              </table>
            </div>
          {/if}
        </article>

        <article class="grid gap-3 rounded-lg border border-slate-200 p-3">
          <h4 class="text-sm font-semibold text-slate-800">鎖帳切換與爭議處置</h4>
          <div class="grid gap-2 rounded-md border border-slate-200 p-3">
            <label class="grid gap-1 text-xs text-slate-600">
              cycleKey
              <input bind:value={settlementLockDraft.cycleKey} class="rounded-md border border-slate-300 px-2 py-2 text-sm text-slate-800" />
            </label>
            <label class="grid gap-1 text-xs text-slate-600">
              lock reason
              <input bind:value={settlementLockDraft.lockReason} class="rounded-md border border-slate-300 px-2 py-2 text-sm text-slate-800" />
            </label>
            <button type="button" class="rounded-md bg-indigo-700 px-3 py-2 text-xs font-semibold text-white" disabled={settlementLocking} onclick={lockSettlementCycle}>
              {settlementLocking ? "處理中..." : "鎖定週期"}
            </button>
            <label class="grid gap-1 text-xs text-slate-600">
              unlock reason
              <input bind:value={settlementLockDraft.unlockReason} class="rounded-md border border-slate-300 px-2 py-2 text-sm text-slate-800" />
            </label>
            <button type="button" class="rounded-md bg-amber-600 px-3 py-2 text-xs font-semibold text-white" disabled={settlementUnlocking} onclick={unlockSettlementCycle}>
              {settlementUnlocking ? "處理中..." : "解鎖週期"}
            </button>
            {#if settlementLockState}
              <p class="text-xs text-slate-700">
                {settlementLockState.cycleKey} | {settlementLockState.lockState} | by {settlementLockState.actorId}
                @ {formatTaipeiDateTime(settlementLockState.changedAt)}
              </p>
              <p class="text-xs text-slate-600">reason: {settlementLockState.reason}</p>
            {/if}
          </div>

          <div class="grid gap-2 rounded-md border border-slate-200 p-3">
            <h5 class="text-xs font-semibold tracking-[0.08em] text-slate-500">爭議處置命令</h5>
            <div class="grid gap-2 md:grid-cols-2">
              <label class="grid gap-1 text-xs text-slate-600">
                disputeId
                <input bind:value={disputeDraft.disputeId} placeholder="dsp-..." class="rounded-md border border-slate-300 px-2 py-2 text-sm text-slate-800" />
              </label>
              <label class="grid gap-1 text-xs text-slate-600">
                operation
                <select bind:value={disputeDraft.operation} class="rounded-md border border-slate-300 px-2 py-2 text-sm text-slate-800">
                  {#each payrollDisputeOperationOptions as operation}
                    <option value={operation}>{operation}</option>
                  {/each}
                </select>
              </label>
              {#if disputeDraft.operation === "ASSIGN_OWNER"}
                <label class="grid gap-1 text-xs text-slate-600 md:col-span-2">
                  ownerActorId
                  <input bind:value={disputeDraft.ownerActorId} class="rounded-md border border-slate-300 px-2 py-2 text-sm text-slate-800" />
                </label>
              {/if}
              {#if disputeDraft.operation === "RESOLVE_REFUND"}
                <label class="grid gap-1 text-xs text-slate-600 md:col-span-2">
                  refundAmountMinor（選填）
                  <input bind:value={disputeDraft.refundAmountMinor} placeholder="1" class="rounded-md border border-slate-300 px-2 py-2 text-sm text-slate-800" />
                </label>
              {/if}
              <label class="grid gap-1 text-xs text-slate-600 md:col-span-2">
                note
                <input bind:value={disputeDraft.note} class="rounded-md border border-slate-300 px-2 py-2 text-sm text-slate-800" />
              </label>
            </div>
            <button type="button" class="rounded-md bg-emerald-700 px-3 py-2 text-xs font-semibold text-white" disabled={disputeSubmitting} onclick={submitDisputeWorkflow}>
              {disputeSubmitting ? "提交中..." : "提交爭議處置"}
            </button>

            {#if disputeResult}
              <div class="grid gap-1 rounded-md border border-slate-200 bg-slate-50 p-3 text-xs text-slate-700">
                <p><span class="font-semibold">Dispute:</span> {disputeResult.disputeId}</p>
                <p><span class="font-semibold">Status:</span> {disputeResult.status}</p>
                <p><span class="font-semibold">Owner:</span> {disputeResult.ownerActorId}</p>
                <p><span class="font-semibold">Updated:</span> {formatTaipeiDateTime(disputeResult.updatedAt)}</p>
                <div class="max-h-32 overflow-auto rounded border border-slate-200 bg-white">
                  <table class="min-w-full text-left text-xs">
                    <thead class="bg-slate-50 text-slate-600">
                      <tr>
                        <th class="px-2 py-1">Time</th>
                        <th class="px-2 py-1">Event</th>
                        <th class="px-2 py-1">Status</th>
                        <th class="px-2 py-1">Note</th>
                      </tr>
                    </thead>
                    <tbody>
                      {#each disputeResult.trace as traceEvent}
                        <tr class="border-t border-slate-100">
                          <td class="px-2 py-1">{formatTaipeiDateTime(traceEvent.occurredAt)}</td>
                          <td class="px-2 py-1">{traceEvent.eventType}</td>
                          <td class="px-2 py-1">{traceEvent.status}</td>
                          <td class="px-2 py-1">{traceEvent.note ?? "-"}</td>
                        </tr>
                      {/each}
                    </tbody>
                  </table>
                </div>
              </div>
            {/if}
          </div>
        </article>
      </div>
    </section>
  {/if}

  {#if sectionVisible("anomaly")}
    <section class="grid gap-4 rounded-xl border border-slate-200 bg-white p-4 shadow-sm">
      <header>
        <h3 class="text-lg font-semibold text-slate-900">異常治理與規則管理</h3>
        <p class="text-sm text-slate-600">
          告警查詢、規則評估、生命周期更新，且 CLOSE 簽核前必須套用 `{ANOMALY_RELEASE_SIGN_OFF_ISSUE_ID}`。
        </p>
      </header>

      <div class="grid gap-4 lg:grid-cols-2">
        <article class="grid gap-3 rounded-lg border border-slate-200 p-3">
          <h4 class="text-sm font-semibold text-slate-800">告警查詢與生命周期</h4>
          <div class="grid gap-2 md:grid-cols-2">
            <label class="grid gap-1 text-xs text-slate-600">
              vendorId
              <input bind:value={anomalyFilters.vendorId} class="rounded-md border border-slate-300 px-2 py-2 text-sm text-slate-800" />
            </label>
            <label class="grid gap-1 text-xs text-slate-600">
              ownerActorId
              <input bind:value={anomalyFilters.ownerActorId} class="rounded-md border border-slate-300 px-2 py-2 text-sm text-slate-800" />
            </label>
            <label class="grid gap-1 text-xs text-slate-600">
              status
              <select bind:value={anomalyFilters.status} class="rounded-md border border-slate-300 px-2 py-2 text-sm text-slate-800">
                <option value="ALL">ALL</option>
                {#each anomalyStatusOptions as status}
                  <option value={status}>{status}</option>
                {/each}
              </select>
            </label>
            <label class="grid gap-1 text-xs text-slate-600">
              escalatedOnly
              <select bind:value={anomalyFilters.escalatedOnly} class="rounded-md border border-slate-300 px-2 py-2 text-sm text-slate-800">
                <option value="ALL">ALL</option>
                <option value="TRUE">TRUE</option>
                <option value="FALSE">FALSE</option>
              </select>
            </label>
            <label class="grid gap-1 text-xs text-slate-600">
              slaStatus
              <select bind:value={anomalyFilters.slaStatus} class="rounded-md border border-slate-300 px-2 py-2 text-sm text-slate-800">
                <option value="ALL">ALL</option>
                {#each anomalySlaStatusOptions as status}
                  <option value={status}>{status}</option>
                {/each}
              </select>
            </label>
            <label class="grid gap-1 text-xs text-slate-600">
              asOfEpochDay / asOfMinuteOfDay
              <div class="grid grid-cols-2 gap-2">
                <input bind:value={anomalyFilters.asOfEpochDay} placeholder="epochDay" class="rounded-md border border-slate-300 px-2 py-2 text-sm text-slate-800" />
                <input bind:value={anomalyFilters.asOfMinuteOfDay} placeholder="minute" class="rounded-md border border-slate-300 px-2 py-2 text-sm text-slate-800" />
              </div>
            </label>
          </div>

          <button type="button" class="rounded-md bg-indigo-700 px-3 py-2 text-xs font-semibold text-white" disabled={anomalyAlertsLoading} onclick={() => refreshAnomalyAlerts(true)}>
            {anomalyAlertsLoading ? "查詢中..." : "查詢異常告警"}
          </button>

          {#if anomalyAlertsError}
            <p class="text-xs text-rose-700">{anomalyAlertsError}</p>
          {/if}

          <label class="grid gap-1 text-xs text-slate-600">
            告警選擇
            <select bind:value={selectedAlertId} class="rounded-md border border-slate-300 px-2 py-2 text-sm text-slate-800">
              {#if anomalyAlerts.length === 0}
                <option value={null}>目前無告警</option>
              {/if}
              {#each anomalyAlerts as alert}
                <option value={alert.alertId}>
                  {alert.alertId} | {alert.status} | {alert.slaStatus} | {alert.vendorId}
                </option>
              {/each}
            </select>
          </label>

          {#if selectedAlert}
            <div class="grid gap-1 rounded-md border border-slate-200 bg-slate-50 p-3 text-xs text-slate-700">
              <p><span class="font-semibold">Rule:</span> {selectedAlert.ruleId} ({selectedAlert.ruleKind})</p>
              <p><span class="font-semibold">Severity:</span> {selectedAlert.severity}</p>
              <p><span class="font-semibold">SLA:</span> {selectedAlert.slaStatus} | due {formatTaipeiDateTime(selectedAlert.slaDueAt)}</p>
              <p><span class="font-semibold">Governance Issue:</span> {selectedAlert.governanceIssueId}</p>
            </div>
          {/if}

          <div class="grid gap-2 rounded-md border border-slate-200 p-3">
            <h5 class="text-xs font-semibold tracking-[0.08em] text-slate-500">告警生命周期命令</h5>
            <label class="grid gap-1 text-xs text-slate-600">
              operation
              <select bind:value={anomalyPatchDraft.operation} class="rounded-md border border-slate-300 px-2 py-2 text-sm text-slate-800">
                {#each anomalyPatchOperationOptions as operation}
                  <option value={operation}>{operation}</option>
                {/each}
              </select>
            </label>

            {#if anomalyPatchDraft.operation === "ASSIGN_OWNER"}
              <label class="grid gap-1 text-xs text-slate-600">
                ownerActorId
                <input bind:value={anomalyPatchDraft.ownerActorId} class="rounded-md border border-slate-300 px-2 py-2 text-sm text-slate-800" />
              </label>
            {/if}

            <label class="grid gap-1 text-xs text-slate-600">
              note（選填）
              <input bind:value={anomalyPatchDraft.note} class="rounded-md border border-slate-300 px-2 py-2 text-sm text-slate-800" />
            </label>

            {#if anomalyPatchDraft.operation === "CLOSE"}
              <label class="grid gap-1 text-xs text-slate-600">
                closureNote
                <input bind:value={anomalyPatchDraft.closureNote} class="rounded-md border border-slate-300 px-2 py-2 text-sm text-slate-800" />
              </label>
              <label class="grid gap-1 text-xs text-slate-600">
                closureEvidenceRefs（CSV / 換行）
                <textarea rows="2" bind:value={anomalyPatchDraft.closureEvidenceRefsCsv} class="rounded-md border border-slate-300 px-2 py-2 text-sm text-slate-800"></textarea>
              </label>
              <label class="grid gap-1 text-xs text-slate-600">
                ticketReference（選填）
                <input bind:value={anomalyPatchDraft.ticketReference} class="rounded-md border border-slate-300 px-2 py-2 text-sm text-slate-800" />
              </label>
              <label class="grid gap-1 text-xs text-slate-600">
                套用議題清單（CSV / 換行）
                <textarea rows="2" bind:value={anomalyIssueChecklistRaw} placeholder="ISS-007" class="rounded-md border border-slate-300 px-2 py-2 text-sm text-slate-800"></textarea>
              </label>
              <p class="text-xs text-slate-500">目前簽核議題：{formatIssueChecklistLabel(anomalyIssueChecklistRaw)}</p>
            {/if}

            <button type="button" class="rounded-md bg-emerald-700 px-3 py-2 text-xs font-semibold text-white" disabled={anomalyPatchSubmitting} onclick={patchAnomalyAlert}>
              {anomalyPatchSubmitting ? "提交中..." : "提交告警命令"}
            </button>
          </div>

          {#if selectedAlert}
            <div class="max-h-36 overflow-auto rounded-md border border-slate-200">
              <table class="min-w-full text-left text-xs">
                <thead class="bg-slate-50 text-slate-600">
                  <tr>
                    <th class="px-2 py-1">Time</th>
                    <th class="px-2 py-1">Event</th>
                    <th class="px-2 py-1">Status</th>
                    <th class="px-2 py-1">Note</th>
                  </tr>
                </thead>
                <tbody>
                  {#each selectedAlert.trace as traceEvent}
                    <tr class="border-t border-slate-100">
                      <td class="px-2 py-1">{formatTaipeiDateTime(traceEvent.occurredAt)}</td>
                      <td class="px-2 py-1">{traceEvent.eventType}</td>
                      <td class="px-2 py-1">{traceEvent.status}</td>
                      <td class="px-2 py-1">{traceEvent.note ?? "-"}</td>
                    </tr>
                  {/each}
                </tbody>
              </table>
            </div>
          {/if}
        </article>

        <article class="grid gap-3 rounded-lg border border-slate-200 p-3">
          <h4 class="text-sm font-semibold text-slate-800">異常評估與規則設定</h4>

          <div class="grid gap-2 rounded-md border border-slate-200 p-3">
            <h5 class="text-xs font-semibold tracking-[0.08em] text-slate-500">觸發評估</h5>
            <div class="grid gap-2 md:grid-cols-2">
              <label class="grid gap-1 text-xs text-slate-600">
                vendorId
                <input bind:value={anomalyEvaluationDraft.vendorId} class="rounded-md border border-slate-300 px-2 py-2 text-sm text-slate-800" />
              </label>
              <label class="grid gap-1 text-xs text-slate-600">
                defaultOwnerActorId
                <input bind:value={anomalyEvaluationDraft.defaultOwnerActorId} class="rounded-md border border-slate-300 px-2 py-2 text-sm text-slate-800" />
              </label>
              <label class="grid gap-1 text-xs text-slate-600">
                daysUntilExpiry
                <input bind:value={anomalyEvaluationDraft.daysUntilExpiry} class="rounded-md border border-slate-300 px-2 py-2 text-sm text-slate-800" />
              </label>
              <label class="grid gap-1 text-xs text-slate-600">
                onTimeRate
                <input bind:value={anomalyEvaluationDraft.onTimeRate} class="rounded-md border border-slate-300 px-2 py-2 text-sm text-slate-800" />
              </label>
              <label class="grid gap-1 text-xs text-slate-600">
                satisfactionScore
                <input bind:value={anomalyEvaluationDraft.satisfactionScore} class="rounded-md border border-slate-300 px-2 py-2 text-sm text-slate-800" />
              </label>
              <label class="grid gap-1 text-xs text-slate-600">
                complaintCount
                <input bind:value={anomalyEvaluationDraft.complaintCount} class="rounded-md border border-slate-300 px-2 py-2 text-sm text-slate-800" />
              </label>
              <label class="grid gap-1 text-xs text-slate-600">
                observedAtEpochDay
                <input bind:value={anomalyEvaluationDraft.observedAtEpochDay} class="rounded-md border border-slate-300 px-2 py-2 text-sm text-slate-800" />
              </label>
              <label class="grid gap-1 text-xs text-slate-600">
                observedAtMinuteOfDay
                <input bind:value={anomalyEvaluationDraft.observedAtMinuteOfDay} class="rounded-md border border-slate-300 px-2 py-2 text-sm text-slate-800" />
              </label>
            </div>
            <button type="button" class="rounded-md bg-indigo-700 px-3 py-2 text-xs font-semibold text-white" disabled={anomalyEvaluating} onclick={evaluateAnomalyAlerts}>
              {anomalyEvaluating ? "評估中..." : "執行異常評估"}
            </button>
            <p class="text-xs text-slate-500">本次評估觸發 {anomalyEvaluationResult.length} 筆</p>
            {#if anomalyEvaluationResult.length > 0}
              <div class="max-h-24 overflow-auto rounded-md border border-slate-200">
                <table class="min-w-full text-left text-xs">
                  <thead class="bg-slate-50 text-slate-600">
                    <tr>
                      <th class="px-2 py-1">alertId</th>
                      <th class="px-2 py-1">status</th>
                      <th class="px-2 py-1">rule</th>
                      <th class="px-2 py-1">value</th>
                    </tr>
                  </thead>
                  <tbody>
                    {#each anomalyEvaluationResult as alert}
                      <tr class="border-t border-slate-100">
                        <td class="px-2 py-1">{alert.alertId}</td>
                        <td class="px-2 py-1">{alert.status}</td>
                        <td class="px-2 py-1">{alert.ruleId}</td>
                        <td class="px-2 py-1">{alert.observedValue}</td>
                      </tr>
                    {/each}
                  </tbody>
                </table>
              </div>
            {/if}
          </div>

          <div class="grid gap-2 rounded-md border border-slate-200 p-3">
            <h5 class="text-xs font-semibold tracking-[0.08em] text-slate-500">規則清單與 upsert</h5>
            <div class="flex flex-wrap gap-2">
              <button type="button" class="rounded-md bg-indigo-700 px-3 py-2 text-xs font-semibold text-white" disabled={anomalyRulesLoading} onclick={() => refreshAnomalyRules(true)}>
                {anomalyRulesLoading ? "載入中..." : "重新載入規則"}
              </button>
              {#if anomalyRulesError}
                <p class="self-center text-xs text-rose-700">{anomalyRulesError}</p>
              {/if}
            </div>
            <div class="max-h-28 overflow-auto rounded-md border border-slate-200">
              <table class="min-w-full text-left text-xs">
                <thead class="bg-slate-50 text-slate-600">
                  <tr>
                    <th class="px-2 py-1">ruleId</th>
                    <th class="px-2 py-1">kind</th>
                    <th class="px-2 py-1">enabled</th>
                    <th class="px-2 py-1">SLA(min)</th>
                    <th class="px-2 py-1">issue</th>
                  </tr>
                </thead>
                <tbody>
                  {#if anomalyRules.length === 0}
                    <tr>
                      <td colspan="5" class="px-2 py-2 text-slate-500">尚無異常規則</td>
                    </tr>
                  {:else}
                    {#each anomalyRules as rule}
                      <tr class="border-t border-slate-100">
                        <td class="px-2 py-1">{rule.ruleId}</td>
                        <td class="px-2 py-1">{rule.kind}</td>
                        <td class="px-2 py-1">{rule.enabled ? "YES" : "NO"}</td>
                        <td class="px-2 py-1">{rule.slaMinutes}</td>
                        <td class="px-2 py-1">{rule.governanceIssueId}</td>
                      </tr>
                    {/each}
                  {/if}
                </tbody>
              </table>
            </div>

            <div class="grid gap-2 rounded-md border border-slate-200 p-3">
              <div class="grid gap-2 md:grid-cols-2">
                <label class="grid gap-1 text-xs text-slate-600">
                  ruleId
                  <input bind:value={anomalyRuleDraft.ruleId} class="rounded-md border border-slate-300 px-2 py-2 text-sm text-slate-800" />
                </label>
                <label class="grid gap-1 text-xs text-slate-600">
                  kind
                  <select bind:value={anomalyRuleDraft.kind} class="rounded-md border border-slate-300 px-2 py-2 text-sm text-slate-800">
                    {#each anomalyRuleKindOptions as option}
                      <option value={option}>{option}</option>
                    {/each}
                  </select>
                </label>
                <label class="grid gap-1 text-xs text-slate-600 md:col-span-2">
                  displayName
                  <input bind:value={anomalyRuleDraft.displayName} class="rounded-md border border-slate-300 px-2 py-2 text-sm text-slate-800" />
                </label>
                <label class="grid gap-1 text-xs text-slate-600 md:col-span-2">
                  description
                  <input bind:value={anomalyRuleDraft.description} class="rounded-md border border-slate-300 px-2 py-2 text-sm text-slate-800" />
                </label>
                <label class="grid gap-1 text-xs text-slate-600 md:col-span-2">
                  governanceIssueId
                  <input bind:value={anomalyRuleDraft.governanceIssueId} class="rounded-md border border-slate-300 px-2 py-2 text-sm text-slate-800" />
                </label>
                <label class="grid gap-1 text-xs text-slate-600">
                  thresholdValue
                  <input bind:value={anomalyRuleDraft.thresholdValue} class="rounded-md border border-slate-300 px-2 py-2 text-sm text-slate-800" />
                </label>
                <label class="grid gap-1 text-xs text-slate-600">
                  thresholdComparator
                  <select bind:value={anomalyRuleDraft.thresholdComparator} class="rounded-md border border-slate-300 px-2 py-2 text-sm text-slate-800">
                    {#each anomalyComparatorOptions as option}
                      <option value={option}>{option}</option>
                    {/each}
                  </select>
                </label>
                <label class="grid gap-1 text-xs text-slate-600">
                  evaluationWindowDays
                  <input bind:value={anomalyRuleDraft.evaluationWindowDays} class="rounded-md border border-slate-300 px-2 py-2 text-sm text-slate-800" />
                </label>
                <label class="grid gap-1 text-xs text-slate-600">
                  slaMinutes
                  <input bind:value={anomalyRuleDraft.slaMinutes} class="rounded-md border border-slate-300 px-2 py-2 text-sm text-slate-800" />
                </label>
                <label class="grid gap-1 text-xs text-slate-600">
                  severity
                  <select bind:value={anomalyRuleDraft.severity} class="rounded-md border border-slate-300 px-2 py-2 text-sm text-slate-800">
                    {#each anomalySeverityOptions as option}
                      <option value={option}>{option}</option>
                    {/each}
                  </select>
                </label>
                <label class="inline-flex items-center gap-2 text-xs text-slate-700 md:mt-6">
                  <input type="checkbox" bind:checked={anomalyRuleDraft.enabled} />
                  enabled
                </label>
              </div>
              <button type="button" class="rounded-md bg-emerald-700 px-3 py-2 text-xs font-semibold text-white" disabled={anomalyRuleSaving} onclick={submitAnomalyRuleDraft}>
                {anomalyRuleSaving ? "儲存中..." : "儲存規則"}
              </button>
            </div>
          </div>
        </article>
      </div>
    </section>
  {/if}

  {#if sectionVisible("audit")}
    <section class="grid gap-4 rounded-xl border border-slate-200 bg-white p-4 shadow-sm">
      <header>
        <h3 class="text-lg font-semibold text-slate-900">稽核調查與責任歸屬</h3>
        <p class="text-sm text-slate-600">同一組條件同步查詢 immutable evidence 與 responsibility attribution。</p>
      </header>

      <div class="grid gap-2 md:grid-cols-3">
        <label class="grid gap-1 text-xs text-slate-600">
          actorId
          <input bind:value={auditFilters.actorId} class="rounded-md border border-slate-300 px-2 py-2 text-sm text-slate-800" />
        </label>
        <label class="grid gap-1 text-xs text-slate-600">
          action
          <select bind:value={auditFilters.action} class="rounded-md border border-slate-300 px-2 py-2 text-sm text-slate-800">
            <option value="ALL">ALL</option>
            {#each auditActionOptions as action}
              <option value={action}>{action}</option>
            {/each}
          </select>
        </label>
        <label class="grid gap-1 text-xs text-slate-600">
          entityType
          <select bind:value={auditFilters.entityType} class="rounded-md border border-slate-300 px-2 py-2 text-sm text-slate-800">
            <option value="ALL">ALL</option>
            {#each auditEntityTypeOptions as entityType}
              <option value={entityType}>{entityType}</option>
            {/each}
          </select>
        </label>
        <label class="grid gap-1 text-xs text-slate-600">
          entityId
          <input bind:value={auditFilters.entityId} class="rounded-md border border-slate-300 px-2 py-2 text-sm text-slate-800" />
        </label>
        <label class="grid gap-1 text-xs text-slate-600">
          correlationId
          <input bind:value={auditFilters.correlationId} class="rounded-md border border-slate-300 px-2 py-2 text-sm text-slate-800" />
        </label>
        <label class="grid gap-1 text-xs text-slate-600">
          occurredFrom / occurredTo (epoch day)
          <div class="grid grid-cols-2 gap-2">
            <input bind:value={auditFilters.occurredFromEpochDay} class="rounded-md border border-slate-300 px-2 py-2 text-sm text-slate-800" />
            <input bind:value={auditFilters.occurredToEpochDay} class="rounded-md border border-slate-300 px-2 py-2 text-sm text-slate-800" />
          </div>
        </label>
      </div>

      <button type="button" class="w-fit rounded-md bg-indigo-700 px-3 py-2 text-xs font-semibold text-white" disabled={auditLoading} onclick={() => refreshAuditViews(true)}>
        {auditLoading ? "查詢中..." : "查詢稽核與責任資料"}
      </button>

      {#if auditError}
        <p class="text-xs text-rose-700">{auditError}</p>
      {/if}

      <div class="grid gap-4 lg:grid-cols-2">
        <article class="grid gap-2 rounded-lg border border-slate-200 p-3">
          <h4 class="text-sm font-semibold text-slate-800">Investigation Evidence ({auditEvidenceItems.length})</h4>
          <div class="max-h-64 overflow-auto rounded-md border border-slate-200">
            <table class="min-w-full text-left text-xs">
              <thead class="bg-slate-50 text-slate-600">
                <tr>
                  <th class="px-2 py-1">occurredAt</th>
                  <th class="px-2 py-1">action</th>
                  <th class="px-2 py-1">entity</th>
                  <th class="px-2 py-1">actor</th>
                  <th class="px-2 py-1">reason</th>
                </tr>
              </thead>
              <tbody>
                {#if auditEvidenceItems.length === 0}
                  <tr>
                    <td colspan="5" class="px-2 py-2 text-slate-500">尚無符合條件資料</td>
                  </tr>
                {:else}
                  {#each auditEvidenceItems as evidence}
                    <tr class="border-t border-slate-100">
                      <td class="px-2 py-1">{formatTaipeiDateTime(evidence.occurredAt)}</td>
                      <td class="px-2 py-1">{evidence.action}</td>
                      <td class="px-2 py-1">{evidence.entityType}:{evidence.entityId}</td>
                      <td class="px-2 py-1">{evidence.actorId} ({evidence.actorRole})</td>
                      <td class="px-2 py-1">{evidence.reason}</td>
                    </tr>
                  {/each}
                {/if}
              </tbody>
            </table>
          </div>
        </article>

        <article class="grid gap-2 rounded-lg border border-slate-200 p-3">
          <h4 class="text-sm font-semibold text-slate-800">Responsibility Attribution ({auditResponsibilityItems.length})</h4>
          <div class="max-h-64 overflow-auto rounded-md border border-slate-200">
            <table class="min-w-full text-left text-xs">
              <thead class="bg-slate-50 text-slate-600">
                <tr>
                  <th class="px-2 py-1">actor</th>
                  <th class="px-2 py-1">role</th>
                  <th class="px-2 py-1">eventCount</th>
                  <th class="px-2 py-1">actions</th>
                  <th class="px-2 py-1">entities</th>
                </tr>
              </thead>
              <tbody>
                {#if auditResponsibilityItems.length === 0}
                  <tr>
                    <td colspan="5" class="px-2 py-2 text-slate-500">尚無符合條件資料</td>
                  </tr>
                {:else}
                  {#each auditResponsibilityItems as attribution}
                    <tr class="border-t border-slate-100 align-top">
                      <td class="px-2 py-1">{attribution.actorId}</td>
                      <td class="px-2 py-1">{attribution.role}</td>
                      <td class="px-2 py-1">{attribution.eventCount}</td>
                      <td class="px-2 py-1">{attribution.actions.join(", ")}</td>
                      <td class="px-2 py-1">
                        {attribution.entities
                          .map((entity) => `${entity.entityType}:${entity.entityId}`)
                          .join(" | ")}
                      </td>
                    </tr>
                  {/each}
                {/if}
              </tbody>
            </table>
          </div>
        </article>
      </div>
    </section>
  {/if}

  {#if sectionVisible("analytics")}
    <section class="grid gap-4 rounded-xl border border-slate-200 bg-white p-4 shadow-sm">
      <header>
        <h3 class="text-lg font-semibold text-slate-900">營運分析儀表板</h3>
        <p class="text-sm text-slate-600">支援 from/to epoch-day 範圍篩選，輸出 metric catalog 與 vendor/plant/time breakdown。</p>
      </header>

      <div class="grid gap-2 md:grid-cols-3">
        <label class="grid gap-1 text-xs text-slate-600">
          fromEpochDay
          <input bind:value={analyticsFilters.fromEpochDay} placeholder="例如 19830" class="rounded-md border border-slate-300 px-2 py-2 text-sm text-slate-800" />
        </label>
        <label class="grid gap-1 text-xs text-slate-600">
          toEpochDay
          <input bind:value={analyticsFilters.toEpochDay} placeholder="例如 19836" class="rounded-md border border-slate-300 px-2 py-2 text-sm text-slate-800" />
        </label>
        <div class="flex items-end">
          <button type="button" class="rounded-md bg-indigo-700 px-3 py-2 text-xs font-semibold text-white" disabled={analyticsLoading} onclick={() => refreshAnalyticsDashboard(true)}>
            {analyticsLoading ? "查詢中..." : "查詢分析儀表板"}
          </button>
        </div>
      </div>

      {#if analyticsError}
        <p class="text-xs text-rose-700">{analyticsError}</p>
      {/if}

      {#if analyticsDashboard}
        <div class="grid gap-1 rounded-md border border-slate-200 bg-slate-50 p-3 text-xs text-slate-700">
          <p>
            schema {analyticsDashboard.metricSchemaVersion}｜generated {formatTaipeiDateTime(analyticsDashboard.generatedAt)}
          </p>
          <p>
            range {analyticsDashboard.fromEpochDay} ~ {analyticsDashboard.toEpochDay}
          </p>
        </div>

        <div class="grid gap-4 lg:grid-cols-2">
          <article class="grid gap-2 rounded-lg border border-slate-200 p-3">
            <h4 class="text-sm font-semibold text-slate-800">Metric Definitions ({analyticsDashboard.metricDefinitions.length})</h4>
            <div class="max-h-52 overflow-auto rounded-md border border-slate-200">
              <table class="min-w-full text-left text-xs">
                <thead class="bg-slate-50 text-slate-600">
                  <tr>
                    <th class="px-2 py-1">key</th>
                    <th class="px-2 py-1">displayName</th>
                    <th class="px-2 py-1">formula</th>
                    <th class="px-2 py-1">unit</th>
                  </tr>
                </thead>
                <tbody>
                  {#each analyticsDashboard.metricDefinitions as definition}
                    <tr class="border-t border-slate-100">
                      <td class="px-2 py-1">{definition.key}</td>
                      <td class="px-2 py-1">{definition.displayName}</td>
                      <td class="px-2 py-1">{definition.formula}</td>
                      <td class="px-2 py-1">{definition.unit}</td>
                    </tr>
                  {/each}
                </tbody>
              </table>
            </div>
          </article>

          <article class="grid gap-2 rounded-lg border border-slate-200 p-3">
            <h4 class="text-sm font-semibold text-slate-800">Time Breakdown ({analyticsDashboard.timeBreakdown.length})</h4>
            <div class="max-h-52 overflow-auto rounded-md border border-slate-200">
              <table class="min-w-full text-left text-xs">
                <thead class="bg-slate-50 text-slate-600">
                  <tr>
                    <th class="px-2 py-1">date</th>
                    {#each analyticsMetricKeys as metricKey}
                      <th class="px-2 py-1">{metricKey}</th>
                    {/each}
                  </tr>
                </thead>
                <tbody>
                  {#each analyticsDashboard.timeBreakdown as row}
                    <tr class="border-t border-slate-100">
                      <td class="px-2 py-1">{row.date}</td>
                      {#each analyticsMetricKeys as metricKey}
                        {@const metric = row.metrics.find((value) => value.metricKey === metricKey)}
                        <td class="px-2 py-1">{metric ? formatMetricValue(metric.metricKey, metric.value) : "-"}</td>
                      {/each}
                    </tr>
                  {/each}
                </tbody>
              </table>
            </div>
          </article>

          <article class="grid gap-2 rounded-lg border border-slate-200 p-3">
            <h4 class="text-sm font-semibold text-slate-800">Vendor Breakdown ({analyticsDashboard.vendorBreakdown.length})</h4>
            <div class="max-h-52 overflow-auto rounded-md border border-slate-200">
              <table class="min-w-full text-left text-xs">
                <thead class="bg-slate-50 text-slate-600">
                  <tr>
                    <th class="px-2 py-1">vendorId</th>
                    {#each analyticsMetricKeys as metricKey}
                      <th class="px-2 py-1">{metricKey}</th>
                    {/each}
                  </tr>
                </thead>
                <tbody>
                  {#each analyticsDashboard.vendorBreakdown as row}
                    <tr class="border-t border-slate-100">
                      <td class="px-2 py-1">{row.vendorId}</td>
                      {#each analyticsMetricKeys as metricKey}
                        {@const metric = row.metrics.find((value) => value.metricKey === metricKey)}
                        <td class="px-2 py-1">{metric ? formatMetricValue(metric.metricKey, metric.value) : "-"}</td>
                      {/each}
                    </tr>
                  {/each}
                </tbody>
              </table>
            </div>
          </article>

          <article class="grid gap-2 rounded-lg border border-slate-200 p-3">
            <h4 class="text-sm font-semibold text-slate-800">Plant Breakdown ({analyticsDashboard.plantBreakdown.length})</h4>
            <div class="max-h-52 overflow-auto rounded-md border border-slate-200">
              <table class="min-w-full text-left text-xs">
                <thead class="bg-slate-50 text-slate-600">
                  <tr>
                    <th class="px-2 py-1">plantId</th>
                    {#each analyticsMetricKeys as metricKey}
                      <th class="px-2 py-1">{metricKey}</th>
                    {/each}
                  </tr>
                </thead>
                <tbody>
                  {#each analyticsDashboard.plantBreakdown as row}
                    <tr class="border-t border-slate-100">
                      <td class="px-2 py-1">{row.plantId}</td>
                      {#each analyticsMetricKeys as metricKey}
                        {@const metric = row.metrics.find((value) => value.metricKey === metricKey)}
                        <td class="px-2 py-1">{metric ? formatMetricValue(metric.metricKey, metric.value) : "-"}</td>
                      {/each}
                    </tr>
                  {/each}
                </tbody>
              </table>
            </div>
          </article>
        </div>
      {/if}
    </section>
  {/if}
</section>
