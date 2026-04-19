<script lang="ts">
  import { onMount } from "svelte";

  import { PageHeader, Card, Button, EmptyState } from "$lib/components/ui";
  import { formatTaipeiDateTime } from "$lib/admin/portal";
  import { readRecentSettlements, type RecentSettlementEntry } from "$lib/admin/api";

  import type { PageData } from "./$types";

  let { data }: { data: PageData } = $props();

  let recent = $state<RecentSettlementEntry | null>(null);

  onMount(() => {
    recent = readRecentSettlements()[0] ?? null;
  });

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

<Card title="最近一次月結摘要" description="由本瀏覽器最近一次關帳產生。">
  {#if !recent}
    <EmptyState
      title="尚未在此瀏覽器關帳"
      description="前往「執行關帳」後，摘要會顯示於此。"
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
        <dt class="text-xs text-slate-500">批次</dt>
        <dd class="font-mono text-xs">{recent.batchId}</dd>
      </div>
      <div>
        <dt class="text-xs text-slate-500">關帳時間</dt>
        <dd class="font-medium">{formatTaipeiDateTime(new Date(recent.closedAtEpochMs).toISOString())}</dd>
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
      <div>
        <dt class="text-xs text-slate-500">退款</dt>
        <dd class="font-medium">{recent.refundedRecords}</dd>
      </div>
      <div>
        <dt class="text-xs text-slate-500">例外筆數</dt>
        <dd class="font-medium">{recent.exceptions.length}</dd>
      </div>
    </dl>
  {/if}
</Card>
