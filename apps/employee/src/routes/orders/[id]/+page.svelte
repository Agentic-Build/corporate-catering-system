<script lang="ts">
  import { StateTag, Card } from "@tbite/ui";
  let { data, form } = $props();
  const o = data.order;

  const statusTone = {
    draft: "neutral",
    placed: "info",
    cutoff: "warning",
    cancelled: "neutral",
    ready: "success",
    picked_up: "success",
    no_show: "danger",
    refunded: "warning",
  } as Record<string, "info" | "neutral" | "warning" | "danger" | "success">;
  const statusLabel = {
    draft: "草稿",
    placed: "已預訂",
    cutoff: "已截單",
    cancelled: "已取消",
    ready: "備餐完成",
    picked_up: "已領取",
    no_show: "未領取",
    refunded: "已退款",
  } as Record<string, string>;
</script>

<section class="max-w-xl space-y-4">
  <header>
    <a href="/orders" class="text-xs text-tb-slate-500 hover:text-tb-slate-700"
      >← 返回訂單列表</a
    >
    <h1 class="mt-1 text-2xl font-black text-tb-slate-900">訂單詳情</h1>
    <p class="mt-1 text-sm text-tb-slate-500 font-jetbrains-mono">{o.id}</p>
    <div class="mt-2">
      <StateTag tone={statusTone[o.status] ?? "neutral"}
        >{statusLabel[o.status] ?? o.status}</StateTag
      >
    </div>
  </header>

  {#if form?.error}
    <p class="rounded-lg bg-tb-rose-50 px-3 py-2 text-sm text-tb-rose-700">{form.error}</p>
  {/if}

  <Card>
    <dl class="grid grid-cols-2 gap-y-2 text-sm">
      <dt class="text-tb-slate-500">取餐日</dt>
      <dd class="font-jetbrains-mono">{o.supply_date}</dd>
      <dt class="text-tb-slate-500">取餐區</dt>
      <dd>{o.plant}</dd>
      <dt class="text-tb-slate-500">截單時間</dt>
      <dd class="font-jetbrains-mono">
        {o.cutoff_at?.slice(0, 19).replace("T", " ") ?? "-"}
      </dd>
      <dt class="text-tb-slate-500">總金額</dt>
      <dd class="font-jetbrains-mono tabular-nums font-bold">
        ${o.total_price_minor.toLocaleString()}
      </dd>
    </dl>
  </Card>

  <Card>
    <h2 class="text-sm font-bold text-tb-slate-900">訂購項目</h2>
    <ul class="mt-3 divide-y divide-tb-slate-100 text-sm">
      {#each o.items as it (it.id)}
        <li class="flex justify-between py-2">
          <span>{it.menu_item_id.slice(0, 8)}…</span>
          <span class="font-jetbrains-mono tabular-nums"
            >× {it.qty} · ${(it.unit_price_minor * it.qty).toLocaleString()}</span
          >
        </li>
      {/each}
    </ul>
  </Card>

  {#if o.status === "placed"}
    <form method="POST" action="?/cancel">
      <button
        class="rounded-lg border border-tb-rose-300 bg-tb-rose-50 px-3.5 py-2 text-sm font-semibold text-tb-rose-700 hover:border-tb-rose-600"
      >
        取消訂單
      </button>
    </form>
  {/if}

  {#if o.status === "ready"}
    <a
      href={`/orders/${o.id}/pickup`}
      class="inline-flex items-center gap-2 rounded-lg bg-tb-red-600 px-3.5 py-2 text-sm font-semibold text-white hover:bg-tb-red-700"
    >
      出示領餐碼 →
    </a>
  {/if}
</section>
