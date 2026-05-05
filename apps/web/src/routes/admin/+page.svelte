<script lang="ts">
  import { onMount } from "svelte";

  import { PageHeader, Card, Button, StateTag, EmptyState } from "$lib/components/ui";
  import { apiClient } from "$lib/platform/api";
  import {
    configureAdminApi,
    describeApiError,
    anomalyStatusTone,
    type AnomalyAlertView,
    type VendorView
  } from "$lib/admin/api";
  import { formatTaipeiDateTime } from "$lib/admin/portal";
  import {
    friendlyAnomalyStatus,
    friendlyAnomalySeverity,
    friendlyVendorCategory,
    maskIdentifier
  } from "$lib/platform/labels";



  import type { PageData } from "./$types";

  let { data }: { data: PageData } = $props();

  const actor = $derived(data.actor);

  let loading = $state(true);
  let loadError = $state<string | null>(null);

  let pendingVendors = $state<VendorView[]>([]);
  let openAlerts = $state<AnomalyAlertView[]>([]);
  let breachedAlerts = $state<AnomalyAlertView[]>([]);


  const pendingCount = $derived(pendingVendors.length);
  const openAlertCount = $derived(openAlerts.length);
  const breachedCount = $derived(breachedAlerts.length);


  type QueueTone = "warning" | "danger" | "info" | "pending" | "success" | "neutral";

  type QueueItem =
    | {
        kind: "vendor";
        key: string;
        title: string;
        subtitle: string;
        tone: QueueTone;
        toneLabel: string;
        href: string;
      }
    | {
        kind: "alert";
        key: string;
        title: string;
        subtitle: string;
        tone: QueueTone;
        toneLabel: string;
        href: string;
        breached: boolean;
      }


  const queue = $derived.by<QueueItem[]>(() => {
    const items: QueueItem[] = [];
    // Breached alerts float to the top.
    for (const alert of breachedAlerts) {
      items.push({
        kind: "alert",
        key: `alert-${alert.alertId}`,
        title: alert.ruleDisplayName,
        subtitle: `商家 ${maskIdentifier(alert.vendorId, 6)} · SLA 超時 · ${friendlyAnomalySeverity(alert.severity)}`,
        tone: "danger",
        toneLabel: friendlyAnomalyStatus(alert.status),
        href: `/admin/anomalies/${alert.alertId}`,
        breached: true
      });
    }
    for (const alert of openAlerts) {
      if (breachedAlerts.some((b) => b.alertId === alert.alertId)) continue;
      items.push({
        kind: "alert",
        key: `alert-${alert.alertId}`,
        title: alert.ruleDisplayName,
        subtitle: `商家 ${maskIdentifier(alert.vendorId, 6)} · ${friendlyAnomalySeverity(alert.severity)}`,
        tone: anomalyStatusTone(alert.status),
        toneLabel: friendlyAnomalyStatus(alert.status),
        href: `/admin/anomalies/${alert.alertId}`,
        breached: false
      });
    }
    for (const vendor of pendingVendors) {
      items.push({
        kind: "vendor",
        key: `vendor-${vendor.vendorId}`,
        title: vendor.displayName,
        subtitle: `${friendlyVendorCategory(vendor.vendorCategory)} · ${maskIdentifier(vendor.vendorId, 6)}`,
        tone: "warning",
        toneLabel: "待審",
        href: `/admin/vendors/${vendor.vendorId}`
      });
    }

    return items;
  });

  function iconFor(kind: QueueItem["kind"]): string {
    switch (kind) {
      case "vendor":
        return "🏪";
      case "alert":
        return "⚠️";

    }
  }

  onMount(() => {
    if (actor?.role === "admin") {
      void loadInbox(data.auth.apiBearerToken);
    } else {
      loading = false;
    }
  });

  async function loadInbox(bearerToken: string | null) {
    loading = true;
    loadError = null;
    try {
      configureAdminApi(bearerToken);
      const [pendingPage, openPage, breachedPage] =
        await Promise.allSettled([
          apiClient.admin.listAdminVendors(1, 50, "createdAt", "desc", "PENDING_REVIEW"),
          apiClient.admin.listAnomalyAlerts(undefined, undefined, "OPEN"),
          apiClient.admin.listAnomalyAlerts(undefined, undefined, undefined, undefined, "BREACHED")
        ]);

      pendingVendors = pendingPage.status === "fulfilled" ? pendingPage.value.items : [];
      openAlerts = openPage.status === "fulfilled" ? openPage.value.items : [];
      breachedAlerts = breachedPage.status === "fulfilled" ? breachedPage.value.items : [];
    } catch (error) {
      loadError = describeApiError(error);
    } finally {
      loading = false;
    }
  }
</script>

<PageHeader
  eyebrow="福委會入口"
  title="今天的 Inbox"
  description="待審、告警與爭議收斂在同一佇列；由緊急程度自動排序。"
  breadcrumbs={data.breadcrumbs}
/>

{#if loadError}
  <Card variant="danger" title="載入失敗">
    <p class="text-sm text-rose-900">{loadError}</p>
    <Button variant="secondary" onclick={() => void loadInbox(data.auth.apiBearerToken)}>
      重試
    </Button>
  </Card>
{:else}
  <!-- Compact status ribbon (5 KPI chips) -->
  <section
    class="grid gap-2 rounded-xl border border-slate-200 bg-white p-3 text-sm md:grid-cols-3"
    aria-label="KPI 狀態列"
  >
    <div class="flex items-baseline justify-between gap-2 rounded-lg bg-slate-50 px-3 py-2">
      <span class="text-xs text-slate-500">待審商家</span>
      <span class="text-base font-semibold text-amber-700">
        {loading ? "—" : pendingCount}
      </span>
    </div>
    <div class="flex items-baseline justify-between gap-2 rounded-lg bg-slate-50 px-3 py-2">
      <span class="text-xs text-slate-500">開放告警</span>
      <span class="text-base font-semibold text-rose-700">
        {loading ? "—" : openAlertCount}
      </span>
    </div>
    <div class="flex items-baseline justify-between gap-2 rounded-lg bg-slate-50 px-3 py-2">
      <span class="text-xs text-slate-500">SLA 超時</span>
      <span class="text-base font-semibold text-rose-700">
        {loading ? "—" : breachedCount}
      </span>
    </div>

  </section>

  <Card
    title="今天要處理的項目"
    description={loading
      ? "載入中..."
      : `佇列共 ${queue.length} 項；超時告警置頂。`}
  >
    {#if loading}
      <p class="text-sm text-slate-600">同步 Inbox 資料中...</p>
    {:else if queue.length === 0}
      <EmptyState
        title="今天沒有待辦"
        description="沒有待審商家或開放告警。可前往下方快捷動作維運系統。"
      />
    {:else}
      <ul class="grid gap-2">
        {#each queue as item (item.key)}
          <li
            class={`flex flex-wrap items-center gap-3 rounded-xl border px-3 py-2 ${
              item.kind === "alert" && item.breached
                ? "border-rose-300 bg-rose-50/60"
                : "border-slate-200 bg-white"
            }`}
          >
            <span class="text-xl" aria-hidden="true">{iconFor(item.kind)}</span>
            <div class="min-w-0 flex-1">
              <p class="truncate text-sm font-semibold text-slate-900">{item.title}</p>
              <p class="truncate text-xs text-slate-600">{item.subtitle}</p>
            </div>
            <StateTag label={item.toneLabel} tone={item.tone} />
            <Button variant="primary" size="sm" href={item.href}>處理 →</Button>
          </li>
        {/each}
      </ul>
    {/if}
  </Card>

  <section class="grid gap-3 md:grid-cols-3" aria-label="常用動作">
    <article class="grid gap-2 rounded-xl border border-slate-200 bg-white p-4">
      <h3 class="text-sm font-semibold text-slate-900">匯出額度報表</h3>
      <p class="text-xs text-slate-600">產出並匯出各週期員工使用額度的報表。</p>
      <Button href="/admin/reports" variant="secondary" size="sm">前往</Button>
    </article>
    <article class="grid gap-2 rounded-xl border border-slate-200 bg-white p-4">
      <h3 class="text-sm font-semibold text-slate-900">執行合規生命週期</h3>
      <p class="text-xs text-slate-600">自動發送提醒、停權、復權。</p>
      <Button href="/admin/compliance/lifecycle" variant="secondary" size="sm">前往</Button>
    </article>
    <article class="grid gap-2 rounded-xl border border-slate-200 bg-white p-4">
      <h3 class="text-sm font-semibold text-slate-900">評估異常規則</h3>
      <p class="text-xs text-slate-600">對特定商家手動跑一次規則評估。</p>
      <Button href="/admin/anomalies/evaluate" variant="secondary" size="sm">前往</Button>
    </article>
  </section>


{/if}
