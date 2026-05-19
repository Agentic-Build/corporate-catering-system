<script lang="ts">
  // VendorDetail — cover, vendor info card, filter bar + FilterSheet,
  // meal list with quantity steppers, and a sticky "view cart" bar.
  import { onMount } from "svelte";
  import { goto } from "$app/navigation";
  import { page } from "$app/stores";
  import { getMenu, type MenuItem } from "$lib/api";
  import { cart } from "$lib/cart.svelte";
  import { applyFilters } from "$lib/filter";
  import { favorites } from "$lib/favorites.svelte";
  import { plateColor } from "$lib/sample";
  import { uiState } from "$lib/ui.svelte";
  import {
    emptyFilters,
    filtersActive,
    type MealFilters,
  } from "$lib/components/FilterSheet.svelte";
  import AppIcon from "$lib/components/AppIcon.svelte";
  import CartBar from "$lib/components/CartBar.svelte";
  import FilterSheet from "$lib/components/FilterSheet.svelte";
  import MealRow from "$lib/components/MealRow.svelte";
  import Plate from "$lib/components/Plate.svelte";

  const vendorId = $derived($page.params.id ?? "");

  let allMeals = $state<MenuItem[]>([]);
  let loading = $state(true);
  let error = $state<string | null>(null);
  let filterOpen = $state(false);
  let filters = $state<MealFilters>(emptyFilters());

  const today = new Date().toISOString().slice(0, 10);

  async function load(id: string) {
    loading = true;
    error = null;
    try {
      // Fetch the full day menu, then keep only this vendor's items.
      const items = await getMenu({ day: today });
      allMeals = items.filter((m) => m.vendor_id === id);
    } catch (e) {
      error = e instanceof Error ? e.message : "載入失敗";
      allMeals = [];
    } finally {
      loading = false;
    }
  }

  onMount(() => load(vendorId));

  const vendorName = $derived(allMeals[0]?.vendor ?? "餐廳");
  const etaLabel = $derived(allMeals[0]?.eta_label ?? allMeals[0]?.pickup_window ?? "");
  const color = $derived(plateColor(vendorId));
  const meals = $derived(applyFilters(allMeals, filters));
  const hasFilters = $derived(filtersActive(filters));
  const isFav = $derived(favorites.has(vendorId));

  function changeQty(m: MenuItem, next: number) {
    cart.set(
      m.id,
      next,
      { name: m.name, price: m.price_minor },
      { id: m.vendor_id, name: m.vendor },
    );
  }
</script>

<div class="fade-in flex h-full flex-col">
  <!-- Cover -->
  <div class="relative flex-shrink-0">
    <Plate {color} class="h-48 w-full" />
    <button
      type="button"
      aria-label="返回"
      onclick={() => goto("/")}
      class="absolute left-4 grid h-9 w-9 place-items-center rounded-full bg-white/95 shadow"
      style="top: max(env(safe-area-inset-top), 1rem)"
    >
      <AppIcon name="back" class="h-5 w-5 text-tb-slate-900" />
    </button>
    <button
      type="button"
      aria-label="收藏"
      onclick={() => favorites.toggle(vendorId)}
      class="absolute right-4 grid h-9 w-9 place-items-center rounded-full text-base shadow transition {isFav
        ? 'bg-tb-rose-500 text-white'
        : 'bg-white/95 text-tb-slate-600'}"
      style="top: max(env(safe-area-inset-top), 1rem)"
    >
      {isFav ? "♥" : "♡"}
    </button>
  </div>

  <!-- Vendor info card -->
  <div class="flex-shrink-0 bg-white px-4 pb-3 pt-4 shadow-sm">
    <div class="flex items-start justify-between gap-3">
      <div>
        <h1 class="text-xl font-black text-tb-slate-900">{vendorName}</h1>
        <div class="mt-0.5 text-xs text-tb-slate-500">{allMeals.length} 道餐點</div>
      </div>
    </div>
    <div class="mt-3 flex flex-wrap gap-2 text-[11px]">
      {#if etaLabel}
        <span class="flex items-center gap-1 rounded-full bg-tb-slate-100 px-2.5 py-1">
          <span>🕐</span>{etaLabel}
        </span>
      {/if}
      <span
        class="flex items-center gap-1 rounded-full bg-tb-emerald-50 px-2.5 py-1 font-bold text-tb-emerald-700"
      >
        <span>✓</span>免外送費 · 免低消
      </span>
      <span
        class="flex items-center gap-1 rounded-full bg-tb-red-50 px-2.5 py-1 font-bold text-tb-red-700"
      >
        <span>💳</span>薪資代扣
      </span>
    </div>
    <div class="mt-2.5 text-[11px] font-semibold text-tb-amber-700">
      ⏰ 截單時間:前一日 17:00
    </div>
  </div>

  <!-- Filter bar -->
  <div
    class="flex flex-shrink-0 items-center gap-2 border-t border-tb-slate-100 bg-white px-4 py-2"
  >
    <button
      type="button"
      onclick={() => (filterOpen = true)}
      class="flex items-center gap-1.5 rounded-full bg-tb-slate-100 px-3 py-1.5 text-xs font-bold text-tb-slate-700"
    >
      <span>🔍</span> 篩選
      {#if hasFilters}
        <span
          class="ml-0.5 grid h-4 w-4 place-items-center rounded-full bg-tb-red-600 text-[9px] text-white"
        >
          !
        </span>
      {/if}
    </button>
    {#if filters.sortBy !== "default"}
      <span class="rounded-full bg-tb-sky-50 px-2 py-1 text-[10px] font-bold text-tb-sky-700">
        排序:{filters.sortBy === "name" ? "名稱" : filters.sortBy === "price_asc" ? "價格↑" : "價格↓"}
      </span>
    {/if}
    {#if filters.showAvail}
      <span
        class="rounded-full bg-tb-emerald-50 px-2 py-1 text-[10px] font-bold text-tb-emerald-700"
      >
        僅顯示有貨
      </span>
    {/if}
  </div>

  <!-- Meal list -->
  <div class="no-scroll flex-1 overflow-y-auto bg-tb-slate-50">
    <div class="flex items-center justify-between px-4 pb-1 pt-3">
      <span class="text-[11px] font-bold uppercase tracking-wider text-tb-slate-500">
        全部餐點 · {meals.length} 項
      </span>
      {#if hasFilters}
        <button
          type="button"
          onclick={() => (filters = emptyFilters())}
          class="text-[10px] font-bold text-tb-red-600"
        >
          清除篩選
        </button>
      {/if}
    </div>
    <div class="grid gap-2 px-4 pb-28">
      {#if loading}
        {#each [0, 1, 2] as i (i)}
          <div class="h-28 animate-pulse rounded-2xl bg-tb-slate-200"></div>
        {/each}
      {:else if error}
        <div
          class="grid place-items-center rounded-2xl border border-dashed border-tb-slate-300 bg-white py-12 text-center"
        >
          <p class="text-sm text-tb-slate-500">{error}</p>
        </div>
      {:else if meals.length === 0}
        <div
          class="grid place-items-center rounded-2xl border border-dashed border-tb-slate-300 bg-white py-12 text-center"
        >
          <p class="text-sm text-tb-slate-500">沒有符合條件的餐點</p>
        </div>
      {:else}
        {#each meals as m (m.id)}
          <MealRow meal={m} qty={cart.qty(m.id)} onChange={(n) => changeQty(m, n)} />
        {/each}
      {/if}
    </div>
  </div>

  <FilterSheet
    open={filterOpen}
    onClose={() => (filterOpen = false)}
    {filters}
    onChange={(f) => (filters = f)}
    onReset={() => (filters = emptyFilters())}
  />

  <CartBar onOpen={() => (uiState.cartOpen = true)} bottom="max(env(safe-area-inset-bottom), 1rem)" />
</div>
