<script lang="ts">
  import { StatCard, Button, Icon } from "@tbite/ui";
  import { enhance } from "$app/forms";
  import ScheduleDayPicker from "$lib/components/ScheduleDayPicker.svelte";
  import ScheduleTable from "$lib/components/ScheduleTable.svelte";
  import MealLibraryDrawer from "$lib/components/MealLibraryDrawer.svelte";

  let { data, form } = $props();

  const todayDay = $derived(data.days[0]);
  const dashboardSub = $derived(
    `${data.today.replace(/-/g, " / ")} · ${todayDay.weekday} · 合計 ${data.stats.totalCapacity} 份`,
  );
  const onTimeRate = $derived(
    data.stats.todayOrderCount > 0
      ? Math.round((data.stats.pickedUp / data.stats.todayOrderCount) * 100)
      : 0,
  );

  // ── Schedule planner state ──
  // Default to tomorrow when present, else today.
  let selectedDay = $state(data.days[1]?.id ?? data.days[0].id);
  let libraryOpen = $state(false);

  const itemById = $derived(
    Object.fromEntries(data.items.map((i: any) => [i.id, i])) as Record<string, any>,
  );
  const selectedDayDef = $derived(data.days.find((d: any) => d.id === selectedDay) ?? data.days[0]);

  /** Enrich a date's supply rows with menu-item detail; drop removed (cap 0). */
  function slotsFor(date: string) {
    const supply = data.supplyByDate[date] ?? [];
    return supply
      .filter((s: any) => s.capacity > 0)
      .map((s: any) => {
        const item = itemById[s.menu_item_id];
        return {
          itemId: s.menu_item_id,
          name: item?.name ?? "未知餐點",
          description: item?.description ?? "",
          image: item?.images?.[0] ?? null,
          price: item?.price_minor ?? 0,
          cap: s.capacity,
          ordered: Math.max(0, s.capacity - s.remain),
          pickupWindow: s.pickup_window ?? "11:50-12:10",
          soldOut: !!s.sold_out,
        };
      });
  }
  const selectedSlots = $derived(slotsFor(selectedDay));
  const scheduledIds = $derived(new Set(selectedSlots.map((s) => s.itemId)));

  // ── Hidden-form plumbing for schedule edits ──
  let capForm = $state<HTMLFormElement>();
  let capItemId = $state("");
  let capDate = $state("");
  let capValue = $state("0");
  let capPickup = $state("11:50-12:10");

  let publishForm = $state<HTMLFormElement>();
  let publishItemId = $state("");

  function submitCap(itemId: string, capacity: number, pickupWindow: string) {
    capItemId = itemId;
    capDate = selectedDay;
    capValue = String(capacity);
    capPickup = pickupWindow;
    queueMicrotask(() => capForm?.requestSubmit());
  }

  // ── Hidden-form plumbing for the sold-out toggle ──
  let soldOutForm = $state<HTMLFormElement>();
  let soldOutItemId = $state("");
  let soldOutDate = $state("");
  let soldOutValue = $state("false");

  function submitSoldOut(itemId: string, soldOut: boolean) {
    soldOutItemId = itemId;
    soldOutDate = selectedDay;
    soldOutValue = String(soldOut);
    queueMicrotask(() => soldOutForm?.requestSubmit());
  }

  /** Library "加入此日" — publish if archived, then schedule a default cap. */
  function addFromLibrary(item: any) {
    if (item.status === "archived") {
      publishItemId = item.id;
      queueMicrotask(() => publishForm?.requestSubmit());
    }
    submitCap(item.id, 50, "11:50-12:10");
  }
</script>

<div class="fade-up">
  <!-- Today operational dashboard -->
  <section class="mb-6">
    <div class="text-[11px] font-bold uppercase tracking-eyebrow text-tb-red-600">
      {data.today.replace(/-/g, " / ")} · {todayDay.weekday} · 今日營運
    </div>
    <h1 class="mt-1 text-3xl font-black tracking-tight text-tb-slate-900">今日備餐儀表板</h1>
    <p class="mt-1 text-sm text-tb-slate-500">{dashboardSub}</p>
  </section>

  {#if form?.error}
    <p class="mb-4 rounded-lg bg-tb-rose-50 px-3 py-2 text-sm text-tb-rose-700">
      {form.error}
    </p>
  {/if}

  <section class="mb-8 grid grid-cols-2 gap-3 md:grid-cols-4">
    <StatCard label="今日份數" value={data.stats.totalCapacity} suffix="份" />
    <StatCard label="準時率" value={`${onTimeRate}%`} hint={`${data.stats.pickedUp} 筆已領取`} />
    <StatCard label="待備餐" value={data.stats.pendingPrep} suffix="筆" />
    <StatCard label="今日營收" value={`$${data.stats.revenue.toLocaleString()}`} />
  </section>

  <!-- 7-day schedule planner -->
  <section>
    <div class="mb-3 flex flex-wrap items-end justify-between gap-3">
      <div>
        <div class="text-[11px] font-bold uppercase tracking-eyebrow text-tb-red-600">
          Schedule · 排程菜單
        </div>
        <h2 class="mt-1 text-2xl font-extrabold tracking-tight text-tb-slate-900">未來 7 天排菜</h2>
        <p class="mt-1 text-sm text-tb-slate-500">
          為每一天預先安排菜色與上限份數。所有菜色從「餐點庫」一鍵取出，照片與描述會保留。
        </p>
      </div>
    </div>

    <div class="mb-4">
      <ScheduleDayPicker
        days={data.days}
        supplyByDate={Object.fromEntries(
          data.days.map((d: any) => [
            d.id,
            slotsFor(d.id).map((s) => ({ cap: s.cap, ordered: s.ordered })),
          ]),
        )}
        selected={selectedDay}
        onSelect={(id) => (selectedDay = id)}
      />
    </div>

    <ScheduleTable
      day={selectedDayDef}
      slots={selectedSlots}
      onOpenLibrary={() => (libraryOpen = true)}
      {submitCap}
      {submitSoldOut}
    />

    <div
      class="mt-3 flex items-start gap-2 rounded-xl bg-tb-amber-50 px-3 py-2.5 text-xs text-tb-amber-900"
    >
      <Icon name="alert" class="mt-0.5 h-3.5 w-3.5 flex-shrink-0" />
      <span>
        每日截單時間 = 取餐日前一日
        17:00；截單後系統會自動鎖定當日菜色與上限。若某菜色已訂購數接近上限，建議提早提高上限或新增其他菜色。
      </span>
    </div>
  </section>
</div>

<!-- Meal-library drawer -->
<MealLibraryDrawer
  open={libraryOpen}
  onClose={() => (libraryOpen = false)}
  library={data.items}
  {scheduledIds}
  onAdd={addFromLibrary}
/>

<!-- Hidden forms — drive schedule edits through +page.server.ts actions. -->
<form bind:this={capForm} method="POST" action="?/setSupply" class="hidden" use:enhance>
  <input type="hidden" name="item_id" value={capItemId} />
  <input type="hidden" name="date" value={capDate} />
  <input type="hidden" name="capacity" value={capValue} />
  <input type="hidden" name="pickup_window" value={capPickup} />
  <input type="hidden" name="cutoff_at" value={`${capDate}T17:00:00Z`} />
</form>
<form bind:this={publishForm} method="POST" action="?/publishItem" class="hidden" use:enhance>
  <input type="hidden" name="item_id" value={publishItemId} />
</form>
<form bind:this={soldOutForm} method="POST" action="?/toggleSoldOut" class="hidden" use:enhance>
  <input type="hidden" name="item_id" value={soldOutItemId} />
  <input type="hidden" name="date" value={soldOutDate} />
  <input type="hidden" name="sold_out" value={soldOutValue} />
</form>
