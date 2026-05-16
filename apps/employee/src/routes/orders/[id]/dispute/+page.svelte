<script lang="ts">
  // 提出申訴 — design-language pass. PageHeader + Card + StateTag.
  import { PageHeader, Card, StateTag, Button, Icon } from "@tbite/ui";

  let { data, form } = $props();
  const o = $derived(data.order);

  const statusLabel: Record<string, string> = {
    picked_up: "已領取",
    no_show: "未領取",
  };
</script>

<a
  href={`/orders/${o.id}`}
  class="mb-3 inline-flex items-center gap-1 text-xs font-semibold text-tb-slate-500 hover:text-tb-slate-900"
>
  <Icon name="chevron" class="h-3.5 w-3.5 rotate-90" />返回訂單
</a>

<PageHeader
  eyebrow="Dispute · 申訴"
  title="提出申訴"
  subtitle="餐點未送達、品質異常或無法領取時，可在領餐日後提出申訴。"
/>

<div class="max-w-xl space-y-4">
  <Card>
    <p class="font-jetbrains-mono text-[11px] text-tb-slate-500">{o.id}</p>
    <dl class="mt-3 grid grid-cols-2 gap-y-2.5 text-sm">
      <dt class="text-tb-slate-500">取餐日</dt>
      <dd class="font-jetbrains-mono">{o.supply_date}</dd>
      <dt class="text-tb-slate-500">取餐區</dt>
      <dd class="flex items-center gap-1.5">
        <Icon name="pin" class="h-3.5 w-3.5 text-tb-red-600" />{o.plant}
      </dd>
      <dt class="text-tb-slate-500">狀態</dt>
      <dd>
        <StateTag tone={o.status === "no_show" ? "danger" : "success"}>
          {statusLabel[o.status] ?? o.status}
        </StateTag>
      </dd>
      <dt class="text-tb-slate-500">總金額</dt>
      <dd class="font-jetbrains-mono font-bold tabular-nums">
        ${o.total_price_minor.toLocaleString()}
      </dd>
    </dl>
  </Card>

  {#if !data.disputable}
    <Card tone="danger">
      <p class="text-sm text-tb-rose-700">
        此訂單狀態為「{o.status}」，無法提出申訴。可申訴狀態：已領取、未領取。
      </p>
      <a
        href="/disputes"
        class="mt-2 inline-block text-sm font-semibold text-tb-red-600 hover:text-tb-red-700"
        >查看申訴記錄 →</a
      >
    </Card>
  {:else}
    {#if form?.error}
      <p class="rounded-tb-xl bg-tb-rose-50 px-3 py-2 text-sm text-tb-rose-700">{form.error}</p>
    {/if}
    <Card title="申訴內容">
      <form method="POST" class="space-y-3">
        <label class="flex flex-col gap-1.5 text-sm">
          <span class="text-[11px] font-bold uppercase tracking-eyebrow text-tb-slate-500">
            申訴原因
          </span>
          <textarea
            name="reason"
            rows="5"
            required
            minlength="1"
            placeholder="請描述異常狀況（例如：餐點未送達、品質異常、商家未開放領取等）"
            class="rounded-tb-lg border border-tb-slate-300 px-3 py-2 text-sm transition focus:border-tb-red-500 focus:outline-none focus:ring-4 focus:ring-tb-red-100"
          ></textarea>
        </label>
        <div class="flex flex-wrap items-center gap-3">
          <Button variant="primary" size="md" type="submit">送出申訴</Button>
          <a
            href="/disputes"
            class="text-sm font-semibold text-tb-slate-600 hover:text-tb-slate-900"
            >查看申訴記錄 →</a
          >
        </div>
      </form>
    </Card>
  {/if}
</div>
