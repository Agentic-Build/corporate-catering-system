<script lang="ts">
  import { onMount } from "svelte";

  import {
    Button,
    Card,
    FormField,
    PageHeader,
    DateInput,
    Sparkline,
    BarChart,
    toasts
  } from "$lib/components/ui";
  import { zhTW } from "$lib/i18n/zh-tw";
  import { apiClient, ensureApiClientConfigured } from "$lib/platform/api";
  import { normalizeApiFailure } from "$lib/platform/api/failure";
  import { epochDayToIsoDate, isoDateToEpochDay, todayIsoDate } from "$lib/platform/time-formats";

  import type { PageData } from "./$types";

  let { data }: { data: PageData } = $props();

  type Dashboard = Awaited<ReturnType<typeof apiClient.vendor.getVendorOperationsAnalyticsDashboard>>;

  let dashboard = $state<Dashboard | null>(null);
  let loading = $state(false);
  let errorMessage = $state<string | null>(null);
  let fromDate = $state("");
  let toDate = $state("");

  onMount(async () => {
    try {
      ensureApiClientConfigured(data.auth.apiBearerToken);
    } catch (error) {
      errorMessage = normalizeApiFailure(error).localizedMessage;
      return;
    }
    applyPreset("last30");
  });

  function applyPreset(preset: "last7" | "last30" | "thisMonth") {
    const today = todayIsoDate();
    const todayEpoch = isoDateToEpochDay(today);
    if (preset === "last7") {
      fromDate = epochDayToIsoDate(todayEpoch - 6);
    } else if (preset === "last30") {
      fromDate = epochDayToIsoDate(todayEpoch - 29);
    } else {
      const [y, m] = today.split("-");
      fromDate = `${y}-${m}-01`;
    }
    toDate = today;
    void refresh();
  }

  async function refresh() {
    if (loading) return;
    loading = true;
    errorMessage = null;
    try {
      const fromEpochDay = fromDate ? isoDateToEpochDay(fromDate) : undefined;
      const toEpochDay = toDate ? isoDateToEpochDay(toDate) : undefined;
      dashboard = await apiClient.vendor.getVendorOperationsAnalyticsDashboard(
        fromEpochDay,
        toEpochDay
      );
    } catch (error) {
      const failure = normalizeApiFailure(error);
      errorMessage = failure.localizedMessage;
      toasts.error(failure.localizedMessage);
    } finally {
      loading = false;
    }
  }

  function metricValueFor(
    metrics: readonly { metricKey: string; value: number }[],
    key: string
  ): number {
    const entry = metrics.find((m) => m.metricKey === key);
    return entry?.value ?? 0;
  }

  function totalForMetric(key: string): number {
    if (!dashboard) return 0;
    return dashboard.timeBreakdown.reduce(
      (sum, entry) => sum + metricValueFor(entry.metrics, key),
      0
    );
  }

  function seriesForMetric(key: string): number[] {
    if (!dashboard) return [];
    return [...dashboard.timeBreakdown]
      .sort((a, b) => a.epochDay - b.epochDay)
      .map((entry) => metricValueFor(entry.metrics, key));
  }

  function topPlants(
    metricKey: string
  ): Array<{ label: string; value: number }> {
    if (!dashboard) return [];
    return [...dashboard.plantBreakdown]
      .map((plant) => ({ label: plant.plantId, value: metricValueFor(plant.metrics, metricKey) }))
      .sort((a, b) => b.value - a.value)
      .slice(0, 8);
  }
</script>

<PageHeader
  title={zhTW.vendor.insights.title}
  description={zhTW.vendor.insights.description}
  breadcrumbs={data.breadcrumbs}
/>

{#if errorMessage}
  <div class="mb-4 rounded-lg border border-rose-200 bg-rose-50 px-3 py-2 text-sm text-rose-900">
    {errorMessage}
  </div>
{/if}

<Card title="日期範圍">
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
    </div>
  </div>
</Card>

{#if dashboard}
  <div class="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
    {#each dashboard.metricDefinitions as def (def.key)}
      {@const total = totalForMetric(def.key)}
      <Card title={def.displayName} description={def.unit}>
        <div class="flex items-end justify-between gap-3">
          <p class="text-3xl font-bold tabular-nums text-slate-900">
            {total.toLocaleString()}
          </p>
          <Sparkline values={seriesForMetric(def.key)} aria-label={`${def.displayName} 趨勢`} />
        </div>
        <p class="text-[11px] text-slate-500">公式：{def.formula}</p>
      </Card>
    {/each}
  </div>

  {#if dashboard.plantBreakdown.length > 0}
    <Card title="Top 廠區">
      <div class="grid gap-4 md:grid-cols-2">
        {#each dashboard.metricDefinitions as def (def.key)}
          <div>
            <p class="mb-2 text-xs font-semibold text-slate-700">{def.displayName}</p>
            <BarChart items={topPlants(def.key)} tone="cyan" />
          </div>
        {/each}
      </div>
    </Card>
  {/if}

  <Card title="時間序列">
    {#if dashboard.timeBreakdown.length === 0}
      <p class="text-sm text-slate-500">此區間沒有時間序列。</p>
    {:else}
      <div class="overflow-x-auto rounded-lg border border-slate-200">
        <table class="min-w-full text-sm">
          <thead class="bg-slate-50 text-left text-xs font-semibold text-slate-600">
            <tr>
              <th class="px-3 py-2">日期</th>
              {#each dashboard.metricDefinitions as def (def.key)}
                <th class="px-3 py-2 text-right">{def.displayName}</th>
              {/each}
            </tr>
          </thead>
          <tbody>
            {#each [...dashboard.timeBreakdown].sort((a, b) => a.epochDay - b.epochDay) as entry (entry.epochDay)}
              <tr class="border-t border-slate-100">
                <td class="px-3 py-2 font-mono tabular-nums">{entry.date}</td>
                {#each dashboard.metricDefinitions as def (def.key)}
                  <td class="px-3 py-2 text-right tabular-nums">
                    {metricValueFor(entry.metrics, def.key).toLocaleString()}
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
