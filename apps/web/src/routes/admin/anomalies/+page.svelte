<script lang="ts">
  import { onMount, untrack } from "svelte";
  import { page } from "$app/state";

  import {
    PageHeader,
    Card,
    Button,
    DataTable,
    FormField,
    StateTag,
    DateInput,
    TimeInput,
    BulkActionBar,
    toasts
  } from "$lib/components/ui";
  import { formatTaipeiDateTime } from "$lib/admin/portal";
  import { apiClient } from "$lib/platform/api";
  import {
    configureAdminApi,
    describeApiError,
    anomalyStatusTone,
    slaStatusTone,
    ANOMALY_STATUS_OPTIONS,
    ANOMALY_SLA_STATUS_OPTIONS,
    normalizeOptional,
    type AnomalyAlertStatusValue,
    type AnomalyAlertView,
    type AnomalySlaStatus
  } from "$lib/admin/api";
  import {
    friendlyAnomalyStatus,
    friendlyAnomalySeverity,
    anomalySeverityTone,
    maskIdentifier
  } from "$lib/platform/labels";
  import { isoDateToEpochDay, timeToMinuteOfDay } from "$lib/platform/time-formats";

  import type { PageData } from "./$types";

  let { data }: { data: PageData } = $props();

  let loading = $state(true);
  let loadError = $state<string | null>(null);
  let alerts = $state<AnomalyAlertView[]>([]);

  const initialSlaStatus = (page.url.searchParams.get("slaStatus") ?? "ALL") as
    | "ALL"
    | AnomalySlaStatus;

  let filters = $state({
    vendorId: "",
    ownerActorId: "",
    status: "ALL" as "ALL" | AnomalyAlertStatusValue,
    escalatedOnly: false,
    slaStatus: initialSlaStatus,
    asOfDate: "",
    asOfTime: 0
  });

  const columns = [
    { id: "select", label: "", width: "3%" },
    { id: "ruleDisplayName", label: "規則", width: "25%" },
    { id: "vendor", label: "商家", width: "12%" },
    { id: "severity", label: "嚴重度", width: "10%" },
    { id: "status", label: "狀態", width: "12%" },
    { id: "sla", label: "SLA", width: "10%" },
    { id: "observedAt", label: "觀測時間", width: "18%" },
    { id: "action", label: "動作", width: "10%" }
  ];

  // Bulk selection state
  let selected = $state<Set<string>>(new Set());
  let bulkProcessing = $state(false);

  function toggleOne(alertId: string) {
    const next = new Set(selected);
    if (next.has(alertId)) next.delete(alertId);
    else next.add(alertId);
    selected = next;
  }

  function allVisibleSelected(): boolean {
    if (alerts.length === 0) return false;
    return alerts.every((a) => selected.has(a.alertId));
  }

  function toggleAll() {
    if (allVisibleSelected()) {
      selected = new Set();
    } else {
      selected = new Set(alerts.map((a) => a.alertId));
    }
  }

  function clearSelection() {
    selected = new Set();
  }

  /**
   * Run a bulk operation by issuing one PATCH per selected alert. Failures are
   * collected and surfaced as a single toast so a partial batch doesn't blow
   * up the UI. Admin can retry on the ones that failed.
   */
  async function runBulk(
    operation: "ACKNOWLEDGE" | "START_REMEDIATION",
    noteLabel: string
  ) {
    if (selected.size === 0 || bulkProcessing) return;
    bulkProcessing = true;
    const ids = Array.from(selected);
    const failures: string[] = [];
    for (const alertId of ids) {
      try {
        await apiClient.admin.updateAdminAnomalyAlert(alertId, {
          operation,
          note: `Bulk ${noteLabel} via admin console`
        });
      } catch {
        failures.push(alertId);
      }
    }
    bulkProcessing = false;
    if (failures.length === 0) {
      toasts.success(`已${noteLabel} ${ids.length} 筆告警`);
    } else {
      toasts.error(`完成 ${ids.length - failures.length} 筆，失敗 ${failures.length} 筆`);
    }
    clearSelection();
    await refresh();
  }

  const statusTabs: Array<{ value: "ALL" | AnomalyAlertStatusValue; label: string }> = [
    { value: "ALL", label: "全部" },
    ...ANOMALY_STATUS_OPTIONS.map((s) => ({ value: s, label: friendlyAnomalyStatus(s) }))
  ];

  let statusCounts = $state<Record<string, number>>({});

  onMount(() => {
    void refresh();
  });

  // Debounced live refetch on filter changes.
  let refetchTimer: ReturnType<typeof setTimeout> | null = null;
  $effect(() => {
    // Track all filter fields so the effect re-runs on any change.
    void filters.vendorId;
    void filters.ownerActorId;
    void filters.status;
    void filters.escalatedOnly;
    void filters.slaStatus;
    void filters.asOfDate;
    void filters.asOfTime;
    untrack(() => {
      if (refetchTimer !== null) clearTimeout(refetchTimer);
      refetchTimer = setTimeout(() => {
        void refresh();
      }, 250);
    });
  });

  async function refresh() {
    loading = true;
    loadError = null;
    try {
      configureAdminApi(data.auth.apiBearerToken);
      const asOfEpochDay = filters.asOfDate.length > 0
        ? isoDateToEpochDay(filters.asOfDate)
        : undefined;
      const asOfMinuteOfDay = filters.asOfDate.length > 0 ? filters.asOfTime : undefined;

      const response = await apiClient.admin.listAnomalyAlerts(
        normalizeOptional(filters.vendorId),
        normalizeOptional(filters.ownerActorId),
        filters.status === "ALL" ? undefined : filters.status,
        filters.escalatedOnly ? true : undefined,
        filters.slaStatus === "ALL" ? undefined : filters.slaStatus,
        asOfEpochDay,
        asOfMinuteOfDay
      );
      alerts = response.items;

      // Build per-status counts from this filtered snapshot so the tab bar
      // reflects "what's here given the current non-status filters".
      const counts: Record<string, number> = { ALL: alerts.length };
      for (const s of ANOMALY_STATUS_OPTIONS) counts[s] = 0;
      for (const alert of alerts) {
        counts[alert.status] = (counts[alert.status] ?? 0) + 1;
      }
      statusCounts = counts;
    } catch (error) {
      loadError = describeApiError(error);
    } finally {
      loading = false;
    }
  }
</script>

<PageHeader
  eyebrow="異常治理"
  title="異常告警"
  description="查詢、推進狀態、結束告警（需 ISS-007 簽核）。"
  breadcrumbs={data.breadcrumbs}
>
  {#snippet actions()}
    <Button variant="secondary" href="/admin/anomalies/rules">管理規則</Button>
    <Button variant="primary" href="/admin/anomalies/evaluate">手動評估</Button>
  {/snippet}
</PageHeader>

<Card title="狀態">
  <div class="flex flex-wrap gap-1" role="tablist" aria-label="告警狀態">
    {#each statusTabs as tab}
      {@const active = filters.status === tab.value}
      {@const count = statusCounts[tab.value] ?? 0}
      <button
        type="button"
        role="tab"
        aria-selected={active}
        class={`inline-flex items-center gap-1.5 rounded-full border px-3 py-1.5 text-sm font-medium transition ${active ? "border-cyan-700 bg-cyan-50 text-cyan-900" : "border-slate-200 bg-white text-slate-700 hover:border-slate-400"}`}
        onclick={() => (filters.status = tab.value)}
      >
        {tab.label}
        <span class={`inline-flex min-w-[1.5rem] justify-center rounded-full px-1.5 text-xs ${active ? "bg-cyan-700 text-white" : "bg-slate-100 text-slate-600"}`}>
          {count}
        </span>
      </button>
    {/each}
  </div>
</Card>

<Card title="篩選">
  <div class="grid gap-3 md:grid-cols-4">
    <FormField label="商家 ID">
      <input
        class="rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm"
        bind:value={filters.vendorId}
        placeholder="選填"
      />
    </FormField>
    <FormField label="負責人 ActorId">
      <input
        class="rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm"
        bind:value={filters.ownerActorId}
        placeholder="選填"
      />
    </FormField>
    <FormField label="SLA 狀態">
      <select
        class="rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm"
        bind:value={filters.slaStatus}
      >
        <option value="ALL">全部</option>
        {#each ANOMALY_SLA_STATUS_OPTIONS as option}
          <option value={option}>{option === "ON_TRACK" ? "進行中" : "已超時"}</option>
        {/each}
      </select>
    </FormField>
    <FormField label="只看已升級">
      <label class="flex items-center gap-2 text-sm text-slate-800">
        <input type="checkbox" bind:checked={filters.escalatedOnly} />
        僅顯示已升級（ESCALATED）
      </label>
    </FormField>
    <FormField label="評估基準日" hint="不填則以目前系統時間為準。">
      <DateInput bind:value={filters.asOfDate} />
    </FormField>
    <FormField label="評估基準時間">
      <TimeInput bind:value={filters.asOfTime} />
    </FormField>
  </div>
</Card>

{#if loadError}
  <Card variant="danger" title="載入失敗">
    <p class="text-sm text-rose-900">{loadError}</p>
  </Card>
{:else}
  <Card title="告警" description={loading ? "載入中..." : `共 ${alerts.length} 筆`}>
    {#if alerts.length > 0}
      <div class="flex flex-wrap items-center gap-2 pb-2 text-xs text-slate-600">
        <label class="inline-flex items-center gap-1.5">
          <input
            type="checkbox"
            checked={allVisibleSelected()}
            onchange={toggleAll}
          />
          全選此頁（{alerts.length}）
        </label>
        {#if selected.size > 0}
          <span class="font-semibold text-cyan-700">已選 {selected.size} 筆</span>
        {/if}
      </div>
    {/if}
    <DataTable
      rows={alerts}
      {columns}
      emptyLabel={loading ? "載入中..." : "沒有符合條件的告警"}
    >
      {#snippet row(alert: AnomalyAlertView)}
        <tr class={`hover:bg-slate-50 ${selected.has(alert.alertId) ? "bg-cyan-50/40" : ""}`}>
          <td class="px-3 py-2 align-top">
            <input
              type="checkbox"
              checked={selected.has(alert.alertId)}
              onchange={() => toggleOne(alert.alertId)}
              aria-label={`選取告警 ${alert.alertId}`}
            />
          </td>
          <td class="px-3 py-2">
            <a
              class="font-semibold text-cyan-700 hover:text-cyan-900"
              href={`/admin/anomalies/${alert.alertId}`}
            >
              {alert.ruleDisplayName}
            </a>
            <p class="font-mono text-[11px] text-slate-500">{maskIdentifier(alert.alertId, 6)}</p>
          </td>
          <td class="px-3 py-2 text-xs font-mono text-slate-700">
            {maskIdentifier(alert.vendorId, 6)}
          </td>
          <td class="px-3 py-2">
            <StateTag
              label={friendlyAnomalySeverity(alert.severity)}
              tone={anomalySeverityTone(alert.severity)}
            />
          </td>
          <td class="px-3 py-2">
            <StateTag
              label={friendlyAnomalyStatus(alert.status)}
              tone={anomalyStatusTone(alert.status)}
            />
          </td>
          <td class="px-3 py-2">
            <StateTag
              label={alert.slaStatus === "BREACHED" ? "超時" : "進行中"}
              tone={slaStatusTone(alert.slaStatus)}
            />
          </td>
          <td class="px-3 py-2 text-xs text-slate-600">
            {formatTaipeiDateTime(alert.observedAt)}
          </td>
          <td class="px-3 py-2">
            <Button variant="ghost" size="sm" href={`/admin/anomalies/${alert.alertId}`}>
              詳情
            </Button>
          </td>
        </tr>
      {/snippet}
    </DataTable>
  </Card>

  <BulkActionBar
    count={selected.size}
    onclear={clearSelection}
    processing={bulkProcessing}
  >
    {#snippet actions()}
      <Button
        variant="secondary"
        size="sm"
        loading={bulkProcessing}
        onclick={() => runBulk("ACKNOWLEDGE", "確認")}
      >
        批次確認
      </Button>
      <Button
        variant="secondary"
        size="sm"
        loading={bulkProcessing}
        onclick={() => runBulk("START_REMEDIATION", "開始處理")}
      >
        批次開始處理
      </Button>
    {/snippet}
  </BulkActionBar>
{/if}
