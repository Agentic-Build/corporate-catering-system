<script lang="ts">
  // 申訴記錄 — design-language pass. PageHeader + Card + StateTag.
  import { PageHeader, Card, StateTag, EmptyState } from "@tbite/ui";

  let { data } = $props();

  const statusTone: Record<string, "info" | "neutral" | "warning" | "danger" | "success"> = {
    open: "warning",
    resolved_refund: "success",
    resolved_reject: "neutral",
    cancelled: "neutral",
  };
  const statusLabel: Record<string, string> = {
    open: "處理中",
    resolved_refund: "已退款",
    resolved_reject: "已駁回",
    cancelled: "已取消",
  };
</script>

<PageHeader
  eyebrow="Disputes · 申訴記錄"
  title="我的申訴"
  subtitle="申訴將由福委會審核，處理結果與退款金額會顯示於此。"
/>

{#if data.disputes.length === 0}
  <EmptyState icon="doc" title="尚無申訴記錄" hint="領餐日後若有異常，可從訂單頁提出申訴。" />
{:else}
  <div class="grid gap-3">
    {#each data.disputes as d (d.id)}
      <Card>
        <div class="flex flex-wrap items-center justify-between gap-3">
          <a
            href={`/orders/${d.order_id}`}
            class="font-jetbrains-mono text-xs text-tb-slate-500 hover:text-tb-slate-900"
          >
            訂單 {d.order_id.slice(0, 8)}
          </a>
          <StateTag tone={statusTone[d.status] ?? "neutral"}>
            {statusLabel[d.status] ?? d.status}
          </StateTag>
        </div>
        <p class="mt-2 text-sm text-tb-slate-900">{d.reason}</p>
        {#if d.status !== "open" && d.resolution}
          <div class="mt-3 rounded-tb-xl bg-tb-slate-50 p-3 text-xs text-tb-slate-700">
            <span class="font-semibold">處理結果：</span>{d.resolution}
            {#if Number(d.refund_minor) > 0}
              <span class="ml-1 font-jetbrains-mono font-bold tabular-nums text-tb-emerald-700">
                · 退款 ${Number(d.refund_minor).toLocaleString()}
              </span>
            {/if}
          </div>
        {/if}
      </Card>
    {/each}
  </div>
{/if}
