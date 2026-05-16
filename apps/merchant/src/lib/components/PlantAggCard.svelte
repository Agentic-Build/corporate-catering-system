<script lang="ts">
  // Per-plant prep aggregation (today only) — ported from
  // MerchantView.jsx TbPlantAggCard. The reference's 領餐點/特殊備註 fields
  // have no API source, so they are omitted (no fabricated data).
  import { Button, Icon } from "@tbite/ui";

  interface PlantItem {
    name: string;
    qty: number;
  }
  interface Props {
    plant: string;
    total: number;
    items: PlantItem[];
    orderCount: number;
  }
  let { plant, total, items, orderCount }: Props = $props();
</script>

<article
  class="flex flex-col overflow-hidden rounded-tb-2xl border border-tb-slate-200 bg-white shadow-tb-sm"
>
  <header
    class="flex items-center justify-between gap-3 border-b border-tb-slate-100 px-5 py-4"
  >
    <div>
      <h3 class="text-base font-bold text-tb-slate-900">{plant}</h3>
      <p class="text-xs text-tb-slate-500">{orderCount} 筆訂單</p>
    </div>
    <div class="text-right">
      <div
        class="font-jetbrains-mono text-2xl font-extrabold tracking-tight text-tb-red-600 tabular-nums"
      >
        {total}
      </div>
      <div
        class="text-[10px] font-semibold uppercase tracking-eyebrow text-tb-slate-500"
      >
        共 {total} 份
      </div>
    </div>
  </header>
  <ul class="divide-y divide-tb-slate-100 px-5">
    {#each items as it (it.name)}
      <li class="flex items-center justify-between py-2.5 text-sm">
        <span class="text-tb-slate-800">{it.name}</span>
        <span
          class="font-jetbrains-mono text-sm font-bold tabular-nums text-tb-slate-900"
        >
          × {it.qty}
        </span>
      </li>
    {/each}
  </ul>
  <footer
    class="flex items-center justify-end gap-2 border-t border-tb-slate-100 bg-tb-slate-50/60 px-5 py-3"
  >
    <a href="/orders?plant={encodeURIComponent(plant)}">
      <Button variant="secondary" size="sm">分區總表</Button>
    </a>
    <a href="/orders?plant={encodeURIComponent(plant)}">
      <Button variant="primary" size="sm">
        <Icon name="download" class="h-3.5 w-3.5" />下載配送標籤
      </Button>
    </a>
  </footer>
</article>
