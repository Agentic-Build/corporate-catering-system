<script lang="ts">
  import { StatCard, Button, Icon } from "@tbite/ui";
  import PlantAggCard from "$lib/components/PlantAggCard.svelte";

  let { data } = $props();

  const todayDay = $derived(data.days[0]);
  const dashboardSub = $derived(
    `${data.today.replace(/-/g, " / ")} · ${todayDay.weekday} · 三廠區合計 ${data.stats.totalCapacity} 份`,
  );
  const onTimeRate = $derived(
    data.stats.todayOrderCount > 0
      ? Math.round((data.stats.pickedUp / data.stats.todayOrderCount) * 100)
      : 0,
  );
</script>

<!-- Today operational dashboard -->
<section class="mb-6">
  <div class="text-[11px] font-bold uppercase tracking-eyebrow text-tb-red-600">
    {data.today.replace(/-/g, " / ")} · {todayDay.weekday} · 今日營運
  </div>
  <h1 class="mt-1 text-3xl font-black tracking-tight text-tb-slate-900">
    今日備餐儀表板
  </h1>
  <p class="mt-1 text-sm text-tb-slate-500">{dashboardSub}</p>
</section>

<section class="mb-8 grid grid-cols-2 gap-3 md:grid-cols-4">
  <StatCard label="今日份數" value={data.stats.totalCapacity} suffix="份" />
  <StatCard label="準時率" value={`${onTimeRate}%`} hint={`${data.stats.pickedUp} 筆已領取`} />
  <StatCard label="待備餐" value={data.stats.pendingPrep} suffix="筆" />
  <StatCard label="今日營收" value={`$${data.stats.revenue.toLocaleString()}`} />
</section>

<!-- Per-plant prep & delivery aggregation (today) -->
<section class="mb-10">
  <div class="mb-3 flex items-baseline justify-between">
    <div>
      <h2 class="text-lg font-bold text-tb-slate-900">備餐與配送彙總</h2>
      <p class="text-sm text-tb-slate-500">
        {data.today.replace(/-/g, "/")}（{todayDay.weekday.slice(1)}） · 依廠區分組
      </p>
    </div>
    <a href="/orders">
      <Button variant="secondary" size="sm">
        <Icon name="download" class="h-4 w-4" />下載今日總表
      </Button>
    </a>
  </div>
  {#if data.plants.length === 0}
    <div
      class="grid place-items-center rounded-tb-2xl border border-dashed border-tb-slate-300 bg-white py-14 text-center"
    >
      <Icon name="doc" class="h-9 w-9 text-tb-slate-300" />
      <p class="mt-2 text-sm font-bold text-tb-slate-700">今日尚無訂單</p>
      <p class="mt-1 text-xs text-tb-slate-500">員工下單後，將依廠區彙總顯示於此。</p>
    </div>
  {:else}
    <div class="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
      {#each data.plants as block (block.plant)}
        <PlantAggCard
          plant={block.plant}
          total={block.total}
          items={block.items}
          orderCount={block.orderCount}
        />
      {/each}
    </div>
  {/if}
</section>
