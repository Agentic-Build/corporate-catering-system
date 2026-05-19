<script lang="ts">
  // HomeScreen — location bar, search, 7-day strip, filter section,
  // category pills and a vendor-card list. Data comes from
  // /api/employee/home (today's flat menu), grouped client-side by vendor.
  import { onMount } from "svelte";
  import { getHome, groupByVendor, type MenuItem, type VendorGroup } from "$lib/api";
  import { applyFilters } from "$lib/filter";
  import { favorites } from "$lib/favorites.svelte";
  import { buildDays, CATEGORIES, plateColor, PLANTS, QUICK_TAGS } from "$lib/sample";
  import { session } from "$lib/session.svelte";
  import { uiState } from "$lib/ui.svelte";
  import { emptyFilters, type MealFilters } from "$lib/components/FilterSheet.svelte";
  import AppIcon from "$lib/components/AppIcon.svelte";
  import VendorCard from "$lib/components/VendorCard.svelte";

  const days = buildDays();
  let day = $state(days[0].id);
  let category = $state("all");
  let filters = $state<MealFilters>(emptyFilters());

  let menu = $state<MenuItem[]>([]);
  let loading = $state(true);
  let error = $state<string | null>(null);

  const plantLabel = $derived(
    PLANTS.find((p) => p.id === (session.user?.plant ?? ""))?.label ?? PLANTS[0].label,
  );

  async function load(d: string) {
    loading = true;
    error = null;
    try {
      const home = await getHome(d);
      menu = home.day_menu ?? [];
    } catch (e) {
      error = e instanceof Error ? e.message : "載入失敗";
      menu = [];
    } finally {
      loading = false;
    }
  }

  onMount(() => load(day));

  function selectDay(d: string) {
    day = d;
    load(d);
  }

  // Category pills map onto health tags; "all" passes everything.
  function inCategory(item: MenuItem): boolean {
    if (category === "all") return true;
    const tags = [...(item.tags ?? []), ...(item.badges ?? [])];
    return tags.some((t) => t.toLowerCase().includes(category));
  }

  const vendors = $derived<VendorGroup[]>(
    groupByVendor(applyFilters(menu.filter(inCategory), filters)),
  );

  function toggleTag(t: string) {
    filters = {
      ...filters,
      tags: filters.tags.includes(t)
        ? filters.tags.filter((x) => x !== t)
        : [...filters.tags, t],
    };
  }
</script>

<div class="flex h-full flex-col">
  <!-- Header -->
  <div
    class="flex-shrink-0 bg-white px-4 pb-2"
    style="padding-top: max(env(safe-area-inset-top), 0.75rem)"
  >
    <div class="mb-3 flex items-center justify-between">
      <div class="flex items-center gap-2">
        <div
          class="grid h-8 w-8 place-items-center rounded-xl bg-tb-red-600 text-sm font-black text-white"
        >
          T
        </div>
        <div>
          <div class="text-[10px] font-bold uppercase tracking-wide text-tb-slate-500">
            領餐地點
          </div>
          <div class="flex items-center gap-1 text-sm font-extrabold leading-tight text-tb-slate-900">
            {plantLabel} <span class="text-xs text-tb-red-500">▾</span>
          </div>
        </div>
      </div>
      <div class="flex items-center gap-2">
        <button
          type="button"
          aria-label="通知"
          onclick={() => (uiState.notifOpen = true)}
          class="relative grid h-9 w-9 place-items-center rounded-full bg-tb-slate-100"
        >
          <AppIcon name="bell" class="h-5 w-5 text-tb-slate-700" />
          <span
            class="absolute right-1.5 top-1.5 h-2 w-2 rounded-full bg-tb-red-500 ring-1 ring-white"
          ></span>
        </button>
        <a
          href="/profile"
          class="grid h-9 w-9 place-items-center rounded-full bg-gradient-to-br from-tb-red-500 to-tb-rose-700 text-sm font-bold text-white"
        >
          {(session.user?.display_name ?? "你").slice(0, 1)}
        </a>
      </div>
    </div>

    <!-- Search -->
    <div class="relative mb-3">
      <AppIcon
        name="search"
        class="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-tb-slate-400"
      />
      <input
        value={filters.search}
        oninput={(e) => (filters = { ...filters, search: e.currentTarget.value })}
        placeholder="搜尋餐廳或餐點…"
        class="w-full rounded-2xl bg-tb-slate-100 py-3 pl-9 pr-4 text-sm text-tb-slate-800 outline-none placeholder:text-tb-slate-400"
      />
    </div>

    <!-- Day strip -->
    <div class="no-scroll -mx-4 flex gap-2 overflow-x-auto px-4 pb-1">
      {#each days as d (d.id)}
        {@const on = d.id === day}
        <button
          type="button"
          onclick={() => selectDay(d.id)}
          class="min-w-[70px] flex-shrink-0 rounded-2xl px-3.5 py-2 text-center transition {on
            ? 'bg-tb-red-600 text-white'
            : 'bg-tb-slate-100 text-tb-slate-700'}"
        >
          <div class="text-xs font-extrabold leading-tight {on ? 'text-white' : 'text-tb-slate-900'}">
            {d.label}
          </div>
          <div class="mt-0.5 text-[10px] {on ? 'text-tb-red-100' : 'text-tb-slate-500'}">
            {d.sub}
          </div>
        </button>
      {/each}
    </div>
  </div>

  <!-- Scrollable content -->
  <div class="no-scroll flex-1 overflow-y-auto bg-tb-slate-50">
    <!-- Filter section -->
    <div class="border-b border-tb-slate-100 bg-white px-4 py-3">
      <div class="mb-2 grid grid-cols-12 gap-2 text-xs">
        <div class="col-span-5">
          <div class="mb-1 text-[10px] font-bold text-tb-slate-500">價格區間</div>
          <div class="flex items-center gap-1">
            <input
              type="number"
              value={filters.priceMin}
              oninput={(e) => (filters = { ...filters, priceMin: e.currentTarget.value })}
              placeholder="最低"
              class="w-full rounded-lg bg-tb-slate-50 px-2 py-2 text-center text-xs outline-none ring-1 ring-tb-slate-200 focus:ring-tb-slate-400"
            />
            <span class="text-tb-slate-300">–</span>
            <input
              type="number"
              value={filters.priceMax}
              oninput={(e) => (filters = { ...filters, priceMax: e.currentTarget.value })}
              placeholder="最高"
              class="w-full rounded-lg bg-tb-slate-50 px-2 py-2 text-center text-xs outline-none ring-1 ring-tb-slate-200 focus:ring-tb-slate-400"
            />
          </div>
        </div>
        <div class="col-span-4">
          <div class="mb-1 text-[10px] font-bold text-tb-slate-500">排序</div>
          <select
            value={filters.sortBy}
            onchange={(e) =>
              (filters = {
                ...filters,
                sortBy: e.currentTarget.value as MealFilters["sortBy"],
              })}
            class="w-full rounded-lg bg-tb-slate-50 px-2 py-2 text-xs font-medium outline-none ring-1 ring-tb-slate-200 focus:ring-tb-slate-400"
          >
            <option value="default">預設排序</option>
            <option value="name">名稱</option>
            <option value="price_asc">價格低到高</option>
            <option value="price_desc">價格高到低</option>
          </select>
        </div>
        <div class="col-span-3 flex flex-col">
          <div class="mb-1 text-[10px] font-bold text-tb-slate-500">供應狀態</div>
          <button
            type="button"
            onclick={() => (filters = { ...filters, showAvail: !filters.showAvail })}
            class="flex items-center justify-center gap-1 rounded-lg px-2 py-2 text-[10px] font-bold transition {filters.showAvail
              ? 'bg-tb-emerald-600 text-white'
              : 'bg-tb-slate-50 text-tb-slate-600 ring-1 ring-tb-slate-200'}"
          >
            {filters.showAvail ? "✓ 有貨" : "全部"}
          </button>
        </div>
      </div>
      <div class="flex flex-wrap items-center gap-1.5">
        {#each QUICK_TAGS as t (t)}
          {@const on = filters.tags.includes(t)}
          <button
            type="button"
            onclick={() => toggleTag(t)}
            class="rounded-full px-2.5 py-1 text-[11px] font-bold {on
              ? 'bg-tb-slate-900 text-white'
              : 'bg-tb-slate-100 text-tb-slate-600 ring-1 ring-tb-slate-200'}"
          >
            {t}
          </button>
        {/each}
      </div>
    </div>

    <!-- Category pills -->
    <div class="no-scroll flex gap-2 overflow-x-auto bg-tb-slate-50 px-4 py-3">
      {#each CATEGORIES as c (c.id)}
        {@const on = c.id === category}
        <button
          type="button"
          onclick={() => (category = c.id)}
          class="flex flex-shrink-0 items-center gap-1.5 rounded-full px-3.5 py-2 text-xs font-bold transition {on
            ? 'bg-tb-slate-900 text-white'
            : 'bg-white text-tb-slate-700 shadow-sm ring-1 ring-tb-slate-200'}"
        >
          <span>{c.glyph}</span>{c.label}
        </button>
      {/each}
    </div>

    <!-- Section header -->
    <div class="flex items-baseline justify-between px-4 pb-2">
      <h2 class="text-base font-extrabold text-tb-slate-900">
        今日可訂餐廳 · {vendors.length} 家
      </h2>
      <span class="text-xs text-tb-slate-500">截單 17:00</span>
    </div>

    <!-- Vendor cards -->
    <div class="grid gap-3 px-4 pb-4">
      {#if loading}
        {#each [0, 1, 2] as i (i)}
          <div class="h-64 animate-pulse rounded-3xl bg-tb-slate-200"></div>
        {/each}
      {:else if error}
        <div
          class="grid place-items-center rounded-3xl border border-dashed border-tb-slate-300 bg-white py-16 text-center"
        >
          <p class="text-sm text-tb-slate-500">{error}</p>
          <button
            type="button"
            onclick={() => load(day)}
            class="mt-3 rounded-full bg-tb-red-600 px-4 py-1.5 text-xs font-bold text-white"
          >
            重試
          </button>
        </div>
      {:else if vendors.length === 0}
        <div
          class="grid place-items-center rounded-3xl border border-dashed border-tb-slate-300 bg-white py-16 text-center"
        >
          <p class="text-sm text-tb-slate-500">這天沒有可訂的餐廳</p>
        </div>
      {:else}
        {#each vendors as v, i (v.vendor_id)}
          <VendorCard
            vendor={v}
            tag={i === 0 ? "今日熱門" : "今日供應"}
            tagColor={plateColor(v.vendor_id) === "rose" ? "bg-tb-pink-500" : "bg-tb-rose-500"}
            favorite={favorites.has(v.vendor_id)}
            onToggleFavorite={() => favorites.toggle(v.vendor_id)}
          />
        {/each}
      {/if}
    </div>
  </div>
</div>
