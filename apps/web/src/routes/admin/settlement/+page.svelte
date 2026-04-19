<script lang="ts">
  import { onMount } from "svelte";

  import { PageHeader, Card, Button, EmptyState, StateTag } from "$lib/components/ui";
  import { formatTaipeiDateTime } from "$lib/admin/portal";
  import { configureAdminApi, describeApiError } from "$lib/admin/api";
  import { apiClient } from "$lib/platform/api";
  import { maskIdentifier } from "$lib/platform/labels";

  import type { PageData } from "./$types";

  let { data }: { data: PageData } = $props();

  type CycleSummary = Awaited<
    ReturnType<typeof apiClient.admin.listPayrollSettlementCycles>
  >["items"][number];

  let recent = $state<CycleSummary | null>(null);
  let loadError = $state<string | null>(null);
  let loading = $state(true);

  onMount(() => {
    void loadRecent();
  });

  async function loadRecent() {
    loading = true;
    loadError = null;
    try {
      configureAdminApi(data.auth.apiBearerToken);
      const page = await apiClient.admin.listPayrollSettlementCycles(1, 1);
      recent = page.items[0] ?? null;
    } catch (error) {
      loadError = describeApiError(error);
    } finally {
      loading = false;
    }
  }

  const steps = [
    {
      label: "1. 確認例外",
      description: "於爭議列表追蹤仍未解決的項目。",
      href: "/admin/settlement/disputes"
    },
    {
      label: "2. 執行關帳",
      description: "四步 wizard；需 ISS-003 簽核。",
      href: "/admin/settlement/close"
    },
    {
      label: "3. 鎖定週期",
      description: "關帳後檢查週期並視情況鎖定。",
      href: "/admin/settlement/cycles"
    },
    {
      label: "4. 處理爭議",
      description: "對仍有爭議的員工退款或駁回。",
      href: "/admin/settlement/disputes"
    }
  ];
</script>

<PageHeader
  eyebrow="月結作業"
  title="月結作業 Hub"
  description="依序完成 4 個步驟：確認例外 → 關帳 → 鎖定 → 解決爭議。"
  breadcrumbs={data.breadcrumbs}
/>

<Card title="月結流程" description="點選任一步驟前往對應頁面。">
  <ol class="grid gap-3 md:grid-cols-4">
    {#each steps as step, index}
      <li class="grid gap-2 rounded-xl border border-slate-200 bg-white p-4">
        <span class="inline-flex h-7 w-7 items-center justify-center rounded-full bg-cyan-600 text-xs font-semibold text-white">
          {index + 1}
        </span>
        <h3 class="text-sm font-semibold text-slate-900">{step.label}</h3>
        <p class="text-xs text-slate-600">{step.description}</p>
        <Button href={step.href} variant="secondary" size="sm">前往</Button>
      </li>
    {/each}
  </ol>
</Card>

<Card title="最近一次月結摘要" description="由伺服器返回的最新已關帳週期。">
  {#if loading}
    <p class="text-sm text-slate-600">同步中...</p>
  {:else if loadError}
    <p class="text-sm text-rose-700">{loadError}</p>
  {:else if !recent}
    <EmptyState
      title="尚無已關帳週期"
      description="尚未執行過月結關帳。請先依『執行關帳』wizard 完成本月結算。"
    >
      {#snippet actions()}
        <Button href="/admin/settlement/close" variant="primary">執行月結關帳</Button>
      {/snippet}
    </EmptyState>
  {:else}
    <dl class="grid gap-2 text-sm text-slate-700 md:grid-cols-4">
      <div>
        <dt class="text-xs text-slate-500">週期</dt>
        <dd class="font-medium">{recent.cycleKey}</dd>
      </div>
      <div>
        <dt class="text-xs text-slate-500">薪資月份</dt>
        <dd class="font-mono text-sm tabular-nums">{recent.payPeriod}</dd>
      </div>
      <div>
        <dt class="text-xs text-slate-500">批次</dt>
        <dd class="font-mono text-xs" title={recent.batchId}>{maskIdentifier(recent.batchId, 8)}</dd>
      </div>
      <div>
        <dt class="text-xs text-slate-500">鎖定狀態</dt>
        <dd>
          <StateTag
            label={recent.lockState === "LOCKED" ? "已鎖定" : "未鎖定"}
            tone={recent.lockState === "LOCKED" ? "danger" : "success"}
          />
        </dd>
      </div>
      <div>
        <dt class="text-xs text-slate-500">關帳時間</dt>
        <dd class="font-medium">{formatTaipeiDateTime(recent.generatedAt)}</dd>
      </div>
      <div>
        <dt class="text-xs text-slate-500">總筆數</dt>
        <dd class="font-medium">{recent.totalRecords}</dd>
      </div>
      <div>
        <dt class="text-xs text-slate-500">爭議</dt>
        <dd class="font-medium text-amber-700">{recent.disputedRecords}</dd>
      </div>
      <div>
        <dt class="text-xs text-slate-500">扣款失敗</dt>
        <dd class="font-medium text-rose-700">{recent.deductionFailedRecords}</dd>
      </div>
    </dl>
  {/if}
</Card>
