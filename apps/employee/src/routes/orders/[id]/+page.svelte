<script lang="ts">
  // 訂單詳情 — design-language pass. PageHeader + Card + StateTag, with the
  // detail card mirroring the reference OrderCard footer.
  import { PageHeader, Card, StateTag, Button, Icon } from "@tbite/ui";

  let { data, form } = $props();
  const o = $derived(data.order);

  const statusTone: Record<string, "info" | "neutral" | "warning" | "danger" | "success"> = {
    draft: "neutral",
    placed: "info",
    cutoff: "warning",
    cancelled: "neutral",
    ready: "success",
    picked_up: "success",
    no_show: "danger",
    refunded: "warning",
  };
  const statusLabel: Record<string, string> = {
    draft: "草稿",
    placed: "已預訂",
    cutoff: "已截單",
    cancelled: "已取消",
    ready: "備餐完成",
    picked_up: "已領取",
    no_show: "未領取",
    refunded: "已退款",
  };

  function fmt(iso: string | undefined): string {
    return iso ? iso.slice(0, 16).replace("T", " ") : "-";
  }
</script>

<a
  href="/orders"
  class="mb-3 inline-flex items-center gap-1 text-xs font-semibold text-tb-slate-500 hover:text-tb-slate-900"
>
  <Icon name="chevron" class="h-3.5 w-3.5 rotate-90" />返回訂單列表
</a>

<PageHeader eyebrow="Order · 訂單詳情" title={o.supply_date}>
  {#snippet actions()}
    <StateTag tone={statusTone[o.status] ?? "neutral"}>
      {statusLabel[o.status] ?? o.status}
    </StateTag>
  {/snippet}
</PageHeader>

<div class="max-w-xl space-y-4">
  {#if form?.error}
    <p class="rounded-tb-xl bg-tb-rose-50 px-3 py-2 text-sm text-tb-rose-700">{form.error}</p>
  {/if}

  <Card>
    <p class="font-jetbrains-mono text-[11px] text-tb-slate-500">{o.id}</p>
    <dl class="mt-3 grid grid-cols-2 gap-y-2.5 text-sm">
      <dt class="text-tb-slate-500">取餐日</dt>
      <dd class="font-jetbrains-mono">{o.supply_date}</dd>
      <dt class="text-tb-slate-500">取餐區</dt>
      <dd class="flex items-center gap-1.5">
        <Icon name="pin" class="h-3.5 w-3.5 text-tb-red-600" />{o.plant}
      </dd>
      <dt class="text-tb-slate-500">截單時間</dt>
      <dd class="font-jetbrains-mono">{fmt(o.cutoff_at)}</dd>
    </dl>
    <div class="mt-3 flex items-end justify-between border-t border-tb-slate-100 pt-3">
      <span class="text-sm text-tb-slate-600">合計（薪資代扣）</span>
      <span class="font-jetbrains-mono text-2xl font-black tabular-nums text-tb-slate-900">
        ${o.total_price_minor.toLocaleString()}
      </span>
    </div>
  </Card>

  <Card title="訂購項目">
    <ul class="divide-y divide-tb-slate-100 text-sm">
      {#each o.items as it (it.id)}
        <li class="flex items-center justify-between gap-3 py-3">
          <span class="font-jetbrains-mono text-xs text-tb-slate-600">
            {it.menu_item_id.slice(0, 8)}
          </span>
          <span class="font-jetbrains-mono tabular-nums">
            × {it.qty} · ${(it.unit_price_minor * it.qty).toLocaleString()}
          </span>
        </li>
      {/each}
    </ul>
  </Card>

  <div class="flex flex-wrap items-center gap-2">
    {#if o.status === "ready"}
      <a
        href={`/orders/${o.id}/pickup`}
        class="inline-flex items-center gap-2 rounded-lg bg-tb-red-600 px-3.5 py-2 text-sm font-semibold text-white transition hover:bg-tb-red-700"
      >
        <Icon name="qr" class="h-4 w-4" />出示領餐碼
      </a>
    {/if}
    {#if o.status === "placed"}
      <form method="POST" action="?/cancel">
        <Button variant="danger" size="md" type="submit">取消訂單</Button>
      </form>
    {/if}
    {#if o.status === "picked_up" || o.status === "no_show"}
      <a
        href={`/orders/${o.id}/dispute`}
        class="inline-flex items-center gap-2 rounded-lg border border-tb-slate-300 px-3.5 py-2 text-sm font-semibold text-tb-slate-800 transition hover:border-tb-slate-500"
      >
        <Icon name="alert" class="h-4 w-4 text-tb-amber-600" />提出申訴
      </a>
    {/if}
  </div>
</div>
