<script lang="ts">
  import { onMount } from "svelte";

  import {
    PageHeader,
    Card,
    Button,
    DataTable,
    EmptyState,
    StateTag
  } from "$lib/components/ui";
  import { formatTaipeiDateTime } from "$lib/admin/portal";
  import { configureAdminApi, describeApiError } from "$lib/admin/api";
  import { apiClient } from "$lib/platform/api";
  import { maskIdentifier } from "$lib/platform/labels";

  type PayrollSettlementCycleSummary = Awaited<
    ReturnType<typeof apiClient.admin.listPayrollSettlementCycles>
  >["items"][number];

  import type { PageData } from "./$types";

  let { data }: { data: PageData } = $props();

  let cycles = $state<PayrollSettlementCycleSummary[]>([]);
  let totalItems = $state(0);
  let loading = $state(true);
  let loadError = $state<string | null>(null);

  const columns = [
    { id: "cycleKey", label: "週期", width: "14%" },
    { id: "payPeriod", label: "薪資月份", width: "12%" },
    { id: "batchId", label: "批次", width: "20%" },
    { id: "lockState", label: "鎖定狀態", width: "10%" },
    { id: "generatedAt", label: "關帳時間", width: "18%" },
    { id: "totals", label: "總數 / 例外", width: "18%" },
    { id: "action", label: "動作", width: "8%" }
  ];

  onMount(() => {
    void refresh();
  });

  async function refresh() {
    loading = true;
    loadError = null;
    try {
      configureAdminApi(data.auth.apiBearerToken);
      const response = await apiClient.admin.listPayrollSettlementCycles(1, 50);
      cycles = [...response.items];
      totalItems = response.totalItems;
    } catch (error) {
      loadError = describeApiError(error);
    } finally {
      loading = false;
    }
  }
</script>

<PageHeader
  eyebrow="月結作業"
  title="結算週期"
  description="顯示所有已執行關帳的週期（由伺服器回傳，換瀏覽器也能看到）。"
  breadcrumbs={data.breadcrumbs}
>
  {#snippet actions()}
    <Button variant="primary" href="/admin/settlement/close">執行關帳</Button>
  {/snippet}
</PageHeader>

{#if loadError}
  <Card variant="danger" title="載入失敗">
    <p class="text-sm text-rose-900">{loadError}</p>
  </Card>
{:else if loading}
  <Card title="同步中">
    <p class="text-sm text-slate-600">載入週期中...</p>
  </Card>
{:else if cycles.length === 0}
  <Card title="結算週期">
    <EmptyState
      title="尚無已關帳週期"
      description="尚未執行過月結關帳。請先依『執行關帳』引導完成本月結算。"
    >
      {#snippet actions()}
        <Button href="/admin/settlement/close" variant="primary">執行本月關帳</Button>
      {/snippet}
    </EmptyState>
  </Card>
{:else}
  <Card title={`已關帳週期（共 ${totalItems} 筆）`}>
    <DataTable rows={cycles} {columns}>
      {#snippet row(entry: PayrollSettlementCycleSummary)}
        <tr class="hover:bg-slate-50">
          <td class="px-3 py-2">
            <a
              class="font-semibold text-cyan-700 hover:text-cyan-900"
              href={`/admin/settlement/cycles/${encodeURIComponent(entry.cycleKey)}`}
            >
              {entry.cycleKey}
            </a>
          </td>
          <td class="px-3 py-2 text-sm font-mono tabular-nums">{entry.payPeriod}</td>
          <td class="px-3 py-2 font-mono text-xs" title={entry.batchId}>
            {maskIdentifier(entry.batchId, 8)}
          </td>
          <td class="px-3 py-2">
            <StateTag
              label={entry.lockState === "LOCKED" ? "已鎖定" : "未鎖定"}
              tone={entry.lockState === "LOCKED" ? "danger" : "success"}
            />
          </td>
          <td class="px-3 py-2 text-xs text-slate-600">
            {formatTaipeiDateTime(entry.generatedAt)}
          </td>
          <td class="px-3 py-2 text-xs text-slate-700">
            {entry.totalRecords}
            <span class="text-slate-400"> / </span>
            爭議 <span class="text-rose-700 font-semibold">{entry.disputedRecords}</span>、
            失敗 <span class="text-amber-700 font-semibold">{entry.deductionFailedRecords}</span>
          </td>
          <td class="px-3 py-2">
            <Button
              variant="ghost"
              size="sm"
              href={`/admin/settlement/cycles/${encodeURIComponent(entry.cycleKey)}`}
            >
              詳情
            </Button>
          </td>
        </tr>
      {/snippet}
    </DataTable>
  </Card>
{/if}
