<script lang="ts">
  import { StateTag } from "@tbite/ui";
  let { data } = $props();
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

<section class="space-y-4">
  <h1 class="text-2xl font-black text-tb-slate-900">我的訂單</h1>
  {#if data.orders.length === 0}
    <p
      class="rounded-tb-2xl border border-tb-slate-200 bg-white p-6 text-center text-sm text-tb-slate-500"
    >
      尚無訂單
    </p>
  {:else}
    <div class="space-y-2">
      {#each data.orders as o (o.id)}
        <a
          href={`/orders/${o.id}`}
          class="block rounded-tb-2xl border border-tb-slate-200 bg-white p-4 shadow-tb-sm hover:shadow-tb-md"
        >
          <div class="flex items-center justify-between">
            <div>
              <p class="text-xs text-tb-slate-500 font-jetbrains-mono">
                {o.supply_date} · {o.plant}
              </p>
              <p class="mt-1 text-sm font-semibold text-tb-slate-900">
                {o.items.length} 件 · ${o.total_price_minor.toLocaleString()}
              </p>
            </div>
            <StateTag tone={statusTone[o.status] ?? "neutral"}
              >{statusLabel[o.status] ?? o.status}</StateTag
            >
          </div>
        </a>
      {/each}
    </div>
  {/if}
</section>
