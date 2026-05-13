<script lang="ts">
  import { Card, StateTag } from "@tbite/ui";
  let { data } = $props();

  const statusTone = {
    open: "warning",
    resolved_refund: "success",
    resolved_reject: "neutral",
    cancelled: "neutral",
  } as Record<string, "info" | "neutral" | "warning" | "danger" | "success">;
  const statusLabel = {
    open: "處理中",
    resolved_refund: "已退款",
    resolved_reject: "已駁回",
    cancelled: "已取消",
  } as Record<string, string>;
</script>

<section class="space-y-4">
  <h1 class="text-2xl font-black text-tb-slate-900">申訴記錄</h1>

  {#if data.disputes.length === 0}
    <p class="rounded-tb-2xl border border-tb-slate-200 bg-white p-6 text-center text-sm text-tb-slate-500">
      尚無申訴
    </p>
  {:else}
    <div class="space-y-3">
      {#each data.disputes as d (d.id)}
        <Card>
          <div class="flex items-center justify-between gap-3">
            <p class="font-jetbrains-mono text-xs text-tb-slate-500">order #{d.order_id.slice(0, 8)}</p>
            <StateTag tone={statusTone[d.status] ?? "neutral"}>{statusLabel[d.status] ?? d.status}</StateTag>
          </div>
          <p class="mt-2 text-sm text-tb-slate-900">{d.reason}</p>
          {#if d.status !== "open" && d.resolution}
            <p class="mt-2 rounded-lg bg-tb-slate-50 p-2 text-xs text-tb-slate-700">
              <span class="font-semibold">處理結果：</span>{d.resolution}
              {#if Number(d.refund_minor) > 0}
                · 退款 ${Number(d.refund_minor).toLocaleString()}
              {/if}
            </p>
          {/if}
        </Card>
      {/each}
    </div>
  {/if}
</section>
