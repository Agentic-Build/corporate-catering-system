<script lang="ts">
  import { StatCard } from "@tbite/ui";
  let { data } = $props();
</script>

<section class="space-y-6">
  <header>
    <h1 class="text-2xl font-black text-tb-slate-900">商家後台</h1>
    <p class="mt-1 text-sm text-tb-slate-500">{data.user.display_name} · 今日 {data.today}</p>
  </header>

  <div class="grid grid-cols-1 gap-4 sm:grid-cols-3">
    <StatCard eyebrow="Today Capacity" value={data.totalCapacity} suffix="份" />
    <StatCard eyebrow="Today Sold"     value={data.totalSold}     suffix="份" />
    <StatCard eyebrow="Active Items"   value={data.activeItems}   suffix="款" />
  </div>

  <div class="flex flex-wrap gap-3 text-sm">
    <a href="/menus" class="rounded-lg bg-tb-red-600 px-3.5 py-2 font-semibold text-white hover:bg-tb-red-700">管理菜單 →</a>
    <a href="/supply" class="rounded-lg border border-tb-slate-300 px-3.5 py-2 font-semibold text-tb-slate-800 hover:border-tb-slate-500">設定份數 →</a>
  </div>
</section>
