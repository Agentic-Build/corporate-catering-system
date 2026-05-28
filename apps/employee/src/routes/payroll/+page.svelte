<script lang="ts">
  import { PageHeader, Card, StateTag, EmptyState, Icon } from "@tbite/ui";
  import PayrollEntrySheet, { type PayrollLine } from "$lib/components/PayrollEntrySheet.svelte";

  let { data } = $props();

  const statusTone: Record<string, "info" | "neutral" | "warning" | "success"> = {
    draft: "neutral",
    locked: "warning",
    exported: "info",
    closed: "success",
  };
  const statusLabel: Record<string, string> = {
    draft: "彙整中",
    locked: "已鎖定",
    exported: "已送 HR",
    closed: "已結帳",
  };

  const lineStatusTone: Record<string, "info" | "neutral" | "warning" | "danger" | "success"> = {
    charged: "info",
    reversed: "neutral",
    no_show: "danger",
  };
  const lineStatusLabel: Record<string, string> = {
    charged: "已扣",
    reversed: "已沖銷",
    no_show: "未領",
  };

  const totalNet = $derived(data.entries.reduce((s, e) => s + e.net_minor, 0));
  const currentLines = $derived((data.currentLines as PayrollLine[]) ?? []);

  let sheetOpen = $state(false);
  let activeLine = $state<PayrollLine | null>(null);
  function openSheet(line: PayrollLine) {
    activeLine = line;
    sheetOpen = true;
  }
</script>

<a
  href="/orders"
  class="mb-3 inline-flex items-center gap-1 text-xs font-semibold text-tb-slate-500 hover:text-tb-slate-900"
>
  <Icon name="chevron" class="h-3.5 w-3.5 rotate-90" />返回訂單列表
</a>

<PageHeader
  eyebrow="Payroll · 月結明細"
  title="月結明細"
  subtitle="查詢每月月結的餐費、退款與淨額。"
/>

<!-- 本月進行中 — accumulating, not-yet-settled period -->
<section class="mb-6">
  <h2 class="mb-2 text-base font-extrabold tracking-tight text-tb-slate-900">本月進行中</h2>
  <div class="mb-3">
    <Card>
      <div class="flex items-end justify-between">
        <span class="text-sm text-tb-slate-600">本月即時累計扣款</span>
        <span class="font-jetbrains-mono text-2xl font-black tabular-nums text-tb-slate-900">
          ${data.currentTotalMinor.toLocaleString()}
        </span>
      </div>
    </Card>
  </div>

  {#if currentLines.length === 0}
    <div
      class="rounded-tb-2xl border border-dashed border-tb-slate-300 bg-white p-6 text-center text-sm text-tb-slate-500"
    >
      本月尚無月結訂單。完成取餐後，扣款明細會即時顯示於此。
    </div>
  {:else}
    <ul class="grid gap-2">
      {#each currentLines as line (line.order_id)}
        {@const reversed = line.status === "reversed"}
        <li>
          <button
            type="button"
            onclick={() => openSheet(line)}
            class="flex w-full items-center justify-between gap-3 rounded-tb-2xl border border-tb-slate-200 bg-white p-3.5 text-left shadow-tb-sm transition hover:border-tb-slate-400 hover:shadow-tb-md"
          >
            <div class="min-w-0">
              <p class="truncate text-sm font-bold text-tb-slate-900">{line.vendor_name}</p>
              <p class="mt-0.5 truncate text-xs text-tb-slate-500">{line.items_summary}</p>
              <p class="mt-0.5 font-jetbrains-mono text-[11px] text-tb-slate-400">
                {line.supply_date}
              </p>
            </div>
            <div class="flex shrink-0 flex-col items-end gap-1">
              <span
                class="font-jetbrains-mono text-sm font-black tabular-nums {reversed
                  ? 'text-tb-rose-700 line-through'
                  : 'text-tb-slate-900'}"
              >
                {reversed ? "-" : ""}${line.amount_minor.toLocaleString()}
              </span>
              <StateTag tone={lineStatusTone[line.status] ?? "neutral"}>
                {lineStatusLabel[line.status] ?? line.status}
              </StateTag>
            </div>
          </button>
        </li>
      {/each}
    </ul>
    <p class="mt-2 text-xs text-tb-slate-500">點選任一筆訂單可評分或回報問題。</p>
  {/if}
</section>

<h2 class="mb-2 text-base font-extrabold tracking-tight text-tb-slate-900">月結批次</h2>
{#if data.entries.length === 0}
  <EmptyState icon="wallet" title="尚無月結紀錄" hint="完成取餐並月結後，扣款明細會顯示於此。" />
{:else}
  <div class="mb-4">
    <Card>
      <div class="flex items-end justify-between">
        <span class="text-sm text-tb-slate-600">累計淨扣款</span>
        <span class="font-jetbrains-mono text-2xl font-black tabular-nums text-tb-slate-900">
          ${totalNet.toLocaleString()}
        </span>
      </div>
    </Card>
  </div>

  <div class="overflow-hidden rounded-tb-2xl border border-tb-slate-200 bg-white shadow-tb-sm">
    <table class="w-full text-sm">
      <thead
        class="bg-tb-slate-50/60 text-left text-[11px] font-bold uppercase tracking-eyebrow text-tb-slate-500"
      >
        <tr>
          <th class="px-5 py-3">月結期間</th>
          <th class="px-3 py-3 text-right">訂單數</th>
          <th class="px-3 py-3 text-right">餐費</th>
          <th class="px-3 py-3 text-right">退款</th>
          <th class="px-3 py-3 text-right">淨額</th>
          <th class="px-5 py-3">狀態</th>
        </tr>
      </thead>
      <tbody class="divide-y divide-tb-slate-100">
        {#each data.entries as e (e.entry_id)}
          <tr class="hover:bg-tb-slate-50/60">
            <td class="px-5 py-3 font-jetbrains-mono text-xs text-tb-slate-700">
              {e.period_start} ~ {e.period_end}
            </td>
            <td class="px-3 py-3 text-right font-jetbrains-mono tabular-nums">
              {e.order_count}
            </td>
            <td class="px-3 py-3 text-right font-jetbrains-mono tabular-nums">
              ${e.amount_minor.toLocaleString()}
            </td>
            <td class="px-3 py-3 text-right font-jetbrains-mono tabular-nums text-tb-rose-700">
              {e.refunded_minor > 0 ? "-$" + e.refunded_minor.toLocaleString() : "—"}
            </td>
            <td class="px-3 py-3 text-right font-jetbrains-mono font-bold tabular-nums">
              ${e.net_minor.toLocaleString()}
            </td>
            <td class="px-5 py-3">
              <StateTag tone={statusTone[e.batch_status] ?? "neutral"}>
                {statusLabel[e.batch_status] ?? e.batch_status}
              </StateTag>
            </td>
          </tr>
        {/each}
      </tbody>
    </table>
  </div>
  <p class="mt-3 text-xs text-tb-slate-500">
    對扣款金額有疑問?可於「我的訂單」對個別訂單提出申訴。
  </p>
{/if}

<PayrollEntrySheet open={sheetOpen} line={activeLine} onClose={() => (sheetOpen = false)} />
