<script lang="ts">
  import { PageHeader, Card, StateTag, EmptyState, Icon } from "@tbite/ui";

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

  const totalNet = $derived(data.entries.reduce((s, e) => s + e.net_minor, 0));
</script>

<a
  href="/orders"
  class="mb-3 inline-flex items-center gap-1 text-xs font-semibold text-tb-slate-500 hover:text-tb-slate-900"
>
  <Icon name="chevron" class="h-3.5 w-3.5 rotate-90" />返回訂單列表
</a>

<PageHeader
  eyebrow="Payroll · 薪資代扣明細"
  title="薪資代扣明細"
  subtitle="查詢每月由薪資代扣的餐費、退款與淨額。"
/>

{#if data.entries.length === 0}
  <EmptyState icon="wallet" title="尚無代扣紀錄" hint="完成取餐並月結後，扣款明細會顯示於此。" />
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
          <th class="px-5 py-3">代扣期間</th>
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
