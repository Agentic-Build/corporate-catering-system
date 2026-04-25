<script lang="ts">
  import { onMount } from "svelte";

  import {
    PageHeader,
    Card,
    Button,
    FormField,
    EmptyState,
    DateInput,
    Sparkline,
    BarChart
  } from "$lib/components/ui";
  import { formatTaipeiDateTime } from "$lib/admin/portal";
  import { apiClient } from "$lib/platform/api";
  import {
    configureAdminApi,
    describeApiError,
    formatMetricValue,
    type OperationsAnalyticsView
  } from "$lib/admin/api";
  import { epochDayToIsoDate, isoDateToEpochDay, todayIsoDate } from "$lib/platform/time-formats";

  import type { PageData } from "./$types";

  let { data }: { data: PageData } = $props();

  let loading = $state(true);
  let loadError = $state<string | null>(null);
  let dashboard = $state<OperationsAnalyticsView | null>(null);

  // UI binds to ISO date strings; we convert to epoch day at request time.
  let fromDate = $state<string>("");
  let toDate = $state<string>("");

  let activeTab = $state<"vendor" | "plant" | "time">("vendor");

  const metricDefinitions = $derived(dashboard?.metricDefinitions ?? []);
  const metricKeys = $derived(metricDefinitions.map((d) => d.key));

  onMount(() => {
    applyPreset("last30");
  });

  function applyPreset(preset: "last7" | "last30" | "thisMonth" | "lastMonth") {
    const today = todayIsoDate();
    const todayEpoch = isoDateToEpochDay(today);
    if (preset === "last7") {
      fromDate = epochDayToIsoDate(todayEpoch - 6);
      toDate = today;
    } else if (preset === "last30") {
      fromDate = epochDayToIsoDate(todayEpoch - 29);
      toDate = today;
    } else if (preset === "thisMonth") {
      const [y, m] = today.split("-");
      fromDate = `${y}-${m}-01`;
      toDate = today;
    } else {
      const [y, m] = today.split("-");
      const thisMonthStart = isoDateToEpochDay(`${y}-${m}-01`);
      fromDate = epochDayToIsoDate(thisMonthStart - 1).slice(0, 7) + "-01";
      toDate = epochDayToIsoDate(thisMonthStart - 1);
    }
    void refresh();
  }

  async function refresh() {
    loading = true;
    loadError = null;
    try {
      configureAdminApi(data.auth.apiBearerToken);
      const fromEpochDay = fromDate ? isoDateToEpochDay(fromDate) : undefined;
      const toEpochDay = toDate ? isoDateToEpochDay(toDate) : undefined;
      dashboard = await apiClient.admin.getAdminOperationsAnalyticsDashboard(
        fromEpochDay,
        toEpochDay
      );
    } catch (error) {
      loadError = describeApiError(error);
    } finally {
      loading = false;
    }
  }

  function metricValueFor(
    metrics: Array<{ metricKey: string; value: number }>,
    key: string
  ): { raw: number; formatted: string } {
    const entry = metrics.find((m) => m.metricKey === key);
    const raw = entry?.value ?? 0;
    return { raw, formatted: entry ? formatMetricValue(key, raw) : "-" };
  }

  /** Total for a metric across the whole time breakdown. */
  function totalForMetric(key: string): number {
    if (!dashboard) return 0;
    return dashboard.timeBreakdown.reduce((sum, entry) => {
      const m = entry.metrics.find((x) => x.metricKey === key);
      return sum + (m?.value ?? 0);
    }, 0);
  }

  /** Series over time (sorted by epochDay ascending) for sparkline rendering. */
  function seriesForMetric(key: string): number[] {
    if (!dashboard) return [];
    return [...dashboard.timeBreakdown]
      .sort((a, b) => a.epochDay - b.epochDay)
      .map((entry) => entry.metrics.find((m) => m.metricKey === key)?.value ?? 0);
  }

  /** Top vendors (or plants) ranked by the "primary" metric of this key. */
  function topBreakdown(
    entries: Array<{ key: string; metrics: Array<{ metricKey: string; value: number }> }>,
    metricKey: string,
    limit = 8
  ): Array<{ label: string; value: number }> {
    return [...entries]
      .map((e) => ({
        label: e.key,
        value: e.metrics.find((m) => m.metricKey === metricKey)?.value ?? 0
      }))
      .sort((a, b) => b.value - a.value)
      .slice(0, limit);
  }

  function exportCsv() {
    if (!dashboard) return;
    const rows: string[] = [];
    const header = ["date", "epochDay", ...metricKeys];
    rows.push(header.join(","));
    for (const entry of dashboard.timeBreakdown) {
      rows.push(
        [entry.date, entry.epochDay, ...metricKeys.map((k) => metricValueFor(entry.metrics, k).raw)].join(",")
      );
    }
    const blob = new Blob([rows.join("\n")], { type: "text/csv;charset=utf-8" });
    const href = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = href;
    a.download = `analytics-${fromDate}_${toDate}.csv`;
    a.click();
    URL.revokeObjectURL(href);
  }
</script>

<PageHeader
  eyebrow="營運分析"
  title="營運分析儀表板"
  description="跨商家、廠區、時間的營運指標；趨勢線與 Top N 一眼可讀。"
  breadcrumbs={data.breadcrumbs}
>
  {#snippet actions()}
    <Button variant="secondary" size="sm" onclick={exportCsv} disabled={!dashboard}>
      匯出 CSV
    </Button>
  {/snippet}
</PageHeader>

<Card title="篩選">
  <div class="grid gap-3 md:grid-cols-[1fr_1fr_auto]">
    <FormField label="起始日">
      <DateInput bind:value={fromDate} onchange={() => void refresh()} />
    </FormField>
    <FormField label="結束日">
      <DateInput bind:value={toDate} onchange={() => void refresh()} />
    </FormField>
    <div class="flex flex-wrap items-end gap-2">
      <Button variant="ghost" size="sm" onclick={() => applyPreset("last7")}>近 7 天</Button>
      <Button variant="ghost" size="sm" onclick={() => applyPreset("last30")}>近 30 天</Button>
      <Button variant="ghost" size="sm" onclick={() => applyPreset("thisMonth")}>本月</Button>
      <Button variant="ghost" size="sm" onclick={() => applyPreset("lastMonth")}>上月</Button>
    </div>
  </div>
</Card>

{#if loadError}
  <Card variant="danger" title="載入失敗">
    <p class="text-sm text-rose-900">{loadError}</p>
  </Card>
{:else if !dashboard}
  <Card title="同步中">
    <p class="text-sm text-slate-600">載入儀表板中...</p>
  </Card>
{:else}
  <!-- Metric cards with sparkline -->
  <div class="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
    {#each metricDefinitions as definition (definition.key)}
      {@const total = totalForMetric(definition.key)}
      {@const series = seriesForMetric(definition.key)}
      <Card title={definition.displayName} description={definition.unit}>
        <div class="flex items-end justify-between gap-3">
          <p class="text-3xl font-bold tabular-nums text-slate-900">
            {formatMetricValue(definition.key, total)}
          </p>
          <Sparkline values={series} aria-label={`${definition.displayName} 趨勢`} />
        </div>
        <p class="text-[11px] text-slate-500">公式：{definition.formula}</p>
      </Card>
    {/each}
  </div>

  <Card
    title="分解視圖"
    description={`資料區間 ${dashboard.fromEpochDay ? epochDayToIsoDate(dashboard.fromEpochDay) : "-"} ~ ${dashboard.toEpochDay ? epochDayToIsoDate(dashboard.toEpochDay) : "-"}，生成於 ${formatTaipeiDateTime(dashboard.generatedAt)}`}
  >
    <div class="flex flex-wrap gap-1 border-b border-slate-200 pb-2">
      <button
        type="button"
        class={`rounded-t-lg px-3 py-2 text-sm font-medium transition ${activeTab === "vendor" ? "bg-cyan-50 font-semibold text-cyan-800" : "text-slate-600 hover:text-slate-900"}`}
        onclick={() => (activeTab = "vendor")}
      >
        商家 ({dashboard.vendorBreakdown.length})
      </button>
      <button
        type="button"
        class={`rounded-t-lg px-3 py-2 text-sm font-medium transition ${activeTab === "plant" ? "bg-cyan-50 font-semibold text-cyan-800" : "text-slate-600 hover:text-slate-900"}`}
        onclick={() => (activeTab = "plant")}
      >
        廠區 ({dashboard.plantBreakdown.length})
      </button>
      <button
        type="button"
        class={`rounded-t-lg px-3 py-2 text-sm font-medium transition ${activeTab === "time" ? "bg-cyan-50 font-semibold text-cyan-800" : "text-slate-600 hover:text-slate-900"}`}
        onclick={() => (activeTab = "time")}
      >
        時間 ({dashboard.timeBreakdown.length})
      </button>
    </div>

    {#if activeTab === "vendor"}
      {#if dashboard.vendorBreakdown.length === 0}
        <EmptyState title="區間內沒有商家分解資料" />
      {:else}
        {#each metricDefinitions as def (def.key)}
          <div class="mt-3">
            <p class="mb-2 text-xs font-semibold text-slate-700">Top 商家 — {def.displayName}</p>
            <BarChart
              items={topBreakdown(
                dashboard.vendorBreakdown.map((v) => ({ key: v.vendorId, metrics: v.metrics })),
                def.key
              )}
              format={(v) => formatMetricValue(def.key, v)}
            />
          </div>
        {/each}
      {/if}
    {:else if activeTab === "plant"}
      {#if dashboard.plantBreakdown.length === 0}
        <EmptyState title="區間內沒有廠區分解資料" />
      {:else}
        {#each metricDefinitions as def (def.key)}
          <div class="mt-3">
            <p class="mb-2 text-xs font-semibold text-slate-700">Top 廠區 — {def.displayName}</p>
            <BarChart
              items={topBreakdown(
                dashboard.plantBreakdown.map((v) => ({ key: v.plantId, metrics: v.metrics })),
                def.key
              )}
              tone="emerald"
              format={(v) => formatMetricValue(def.key, v)}
            />
          </div>
        {/each}
      {/if}
    {:else if dashboard.timeBreakdown.length === 0}
      <EmptyState title="區間內沒有時間分解資料" />
    {:else}
      <div class="overflow-x-auto">
        <table class="min-w-full divide-y divide-slate-200 text-xs">
          <thead class="bg-slate-50 text-slate-700">
            <tr>
              <th class="px-2 py-1 text-left">日期</th>
              {#each metricKeys as key}
                <th class="px-2 py-1 text-right">
                  {metricDefinitions.find((d) => d.key === key)?.displayName ?? key}
                </th>
              {/each}
            </tr>
          </thead>
          <tbody class="divide-y divide-slate-100 bg-white">
            {#each [...dashboard.timeBreakdown].sort((a, b) => a.epochDay - b.epochDay) as entry (entry.epochDay)}
              <tr>
                <td class="px-2 py-1 font-mono">{entry.date}</td>
                {#each metricKeys as key}
                  <td class="px-2 py-1 text-right tabular-nums">
                    {metricValueFor(entry.metrics, key).formatted}
                  </td>
                {/each}
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    {/if}
  </Card>
{/if}
