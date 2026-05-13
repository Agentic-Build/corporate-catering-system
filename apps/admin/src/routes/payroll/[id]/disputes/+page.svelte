<script lang="ts">
  import { Card, StateTag } from "@tbite/ui";
  let { data, form } = $props();

  const filters = [
    { id: "", label: "全部" },
    { id: "open", label: "待處理" },
    { id: "resolved_refund", label: "已退款" },
    { id: "resolved_reject", label: "已駁回" },
    { id: "cancelled", label: "已取消" },
  ];

  const statusTone = {
    open: "warning",
    resolved_refund: "success",
    resolved_reject: "neutral",
    cancelled: "neutral",
  } as Record<string, "info" | "neutral" | "warning" | "danger" | "success">;
  const statusLabel = {
    open: "待處理",
    resolved_refund: "已退款",
    resolved_reject: "已駁回",
    cancelled: "已取消",
  } as Record<string, string>;
</script>

<section class="space-y-4">
  <header>
    <a href="/payroll/{data.batchId}" class="text-xs text-tb-slate-500 hover:text-tb-slate-700">← 返回批次</a>
    <h1 class="mt-1 text-2xl font-black text-tb-slate-900">爭議處理</h1>
  </header>

  <div class="flex flex-wrap gap-1 rounded-full bg-tb-slate-100 p-1">
    {#each filters as f}
      <a href={f.id ? `?status=${f.id}` : "?"}
         class="rounded-full px-3 py-1 text-xs font-semibold {data.status === f.id ? 'bg-tb-slate-900 text-white' : 'text-tb-slate-700'}">
        {f.label}
      </a>
    {/each}
  </div>

  {#if form?.error}
    <p class="rounded-lg bg-tb-rose-50 px-3 py-2 text-sm text-tb-rose-700">{form.error}</p>
  {/if}

  {#if data.disputes.length === 0}
    <p class="rounded-tb-2xl border border-tb-slate-200 bg-white p-6 text-center text-sm text-tb-slate-500">
      尚無爭議
    </p>
  {:else}
    <div class="space-y-3">
      {#each data.disputes as d (d.id)}
        <Card>
          <div class="flex items-start justify-between gap-3">
            <div class="min-w-0">
              <div class="flex items-center gap-2">
                <span class="font-jetbrains-mono text-xs text-tb-slate-500">#{d.id.slice(0, 8)}</span>
                <StateTag tone={statusTone[d.status] ?? "neutral"}>{statusLabel[d.status] ?? d.status}</StateTag>
              </div>
              <p class="mt-2 text-sm text-tb-slate-900">{d.reason}</p>
              <p class="mt-1 text-xs text-tb-slate-500 font-jetbrains-mono">
                order: {d.order_id.slice(0, 8)} · opened_by: {d.opened_by.slice(0, 8)}
              </p>
              {#if d.status !== "open" && d.resolution}
                <p class="mt-2 rounded-lg bg-tb-slate-50 p-2 text-xs text-tb-slate-700">
                  <span class="font-semibold">處理結果：</span>{d.resolution}
                  {#if Number(d.refund_minor) > 0}
                    · 退款 ${Number(d.refund_minor).toLocaleString()}
                  {/if}
                </p>
              {/if}
            </div>
          </div>

          {#if d.status === "open"}
            <div class="mt-4 grid grid-cols-1 gap-3 border-t border-tb-slate-100 pt-3 md:grid-cols-2">
              <form method="POST" action="?/resolveRefund" class="space-y-2 rounded-lg border border-emerald-200 bg-emerald-50/40 p-3">
                <input type="hidden" name="dispute_id" value={d.id} />
                <p class="text-xs font-semibold uppercase tracking-eyebrow text-emerald-700">同意退款</p>
                <label class="flex flex-col gap-1 text-xs text-tb-slate-700">
                  退款金額 (元)
                  <input
                    name="refund_minor"
                    type="number"
                    min="0"
                    required
                    placeholder="例如 120"
                    class="rounded-lg border border-tb-slate-300 px-2 py-1 text-sm"
                  />
                </label>
                <label class="flex flex-col gap-1 text-xs text-tb-slate-700">
                  說明
                  <input
                    name="resolution"
                    type="text"
                    placeholder="退款原因摘要"
                    class="rounded-lg border border-tb-slate-300 px-2 py-1 text-sm"
                  />
                </label>
                <button class="rounded-lg bg-emerald-600 px-3.5 py-1.5 text-sm font-semibold text-white hover:bg-emerald-700">
                  確認退款
                </button>
              </form>

              <form method="POST" action="?/resolveReject" class="space-y-2 rounded-lg border border-tb-slate-300 bg-tb-slate-50/40 p-3">
                <input type="hidden" name="dispute_id" value={d.id} />
                <p class="text-xs font-semibold uppercase tracking-eyebrow text-tb-slate-700">駁回</p>
                <label class="flex flex-col gap-1 text-xs text-tb-slate-700">
                  說明 <span class="text-tb-rose-700">*</span>
                  <input
                    name="resolution"
                    type="text"
                    required
                    placeholder="駁回原因"
                    class="rounded-lg border border-tb-slate-300 px-2 py-1 text-sm"
                  />
                </label>
                <button class="rounded-lg bg-tb-slate-900 px-3.5 py-1.5 text-sm font-semibold text-white hover:bg-tb-slate-700">
                  駁回
                </button>
              </form>
            </div>
          {/if}
        </Card>
      {/each}
    </div>
  {/if}
</section>
