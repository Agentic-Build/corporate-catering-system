<script lang="ts">
  import { Card, StateTag } from "@tbite/ui";
  let { data, form } = $props();
  const o = data.order;

  const statusLabel: Record<string, string> = {
    picked_up: "已領取",
    no_show: "未領取",
  };
</script>

<section class="max-w-xl space-y-4">
  <header>
    <a href="/orders/{o.id}" class="text-xs text-tb-slate-500 hover:text-tb-slate-700">← 返回訂單</a>
    <h1 class="mt-1 text-2xl font-black text-tb-slate-900">提出申訴</h1>
    <p class="mt-1 text-sm text-tb-slate-500 font-jetbrains-mono">{o.id}</p>
  </header>

  <Card>
    <dl class="grid grid-cols-2 gap-y-2 text-sm">
      <dt class="text-tb-slate-500">取餐日</dt>
      <dd class="font-jetbrains-mono">{o.supply_date}</dd>
      <dt class="text-tb-slate-500">取餐區</dt>
      <dd>{o.plant}</dd>
      <dt class="text-tb-slate-500">狀態</dt>
      <dd><StateTag tone={o.status === "no_show" ? "danger" : "success"}>{statusLabel[o.status] ?? o.status}</StateTag></dd>
      <dt class="text-tb-slate-500">總金額</dt>
      <dd class="font-jetbrains-mono tabular-nums font-bold">${o.total_price_minor.toLocaleString()}</dd>
    </dl>
  </Card>

  {#if !data.disputable}
    <p class="rounded-lg bg-tb-rose-50 px-3 py-2 text-sm text-tb-rose-700">
      此訂單狀態為「{o.status}」，無法提出申訴。可申訴狀態：已領取、未領取。
    </p>
    <a href="/disputes" class="text-sm text-tb-red-600 hover:text-tb-red-700">查看申訴記錄 →</a>
  {:else}
    {#if form?.error}
      <p class="rounded-lg bg-tb-rose-50 px-3 py-2 text-sm text-tb-rose-700">{form.error}</p>
    {/if}
    <Card>
      <form method="POST" class="space-y-3">
        <label class="flex flex-col gap-1 text-sm">
          <span class="text-xs uppercase tracking-eyebrow text-tb-slate-500">申訴原因</span>
          <textarea
            name="reason"
            rows="5"
            required
            minlength="1"
            placeholder="請描述異常狀況（例如：餐點未送達、品質異常、商家未開放領取等）"
            class="rounded-lg border border-tb-slate-300 px-3 py-2 text-sm"
          ></textarea>
        </label>
        <div class="flex flex-wrap items-center gap-3">
          <button class="rounded-lg bg-tb-red-600 px-3.5 py-2 text-sm font-semibold text-white hover:bg-tb-red-700">
            送出申訴
          </button>
          <a href="/disputes" class="text-sm text-tb-slate-600 hover:text-tb-slate-800">查看訴狀 →</a>
        </div>
      </form>
    </Card>
  {/if}
</section>
