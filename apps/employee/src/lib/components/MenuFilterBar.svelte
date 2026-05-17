<script lang="ts">
  // F3 菜單搜尋與篩選 — filter bar above the full-menu grid. All filter
  // state is mirrored into the URL query (q / tags / price_min / price_max /
  // in_stock / sort) so a reload preserves it. The server reads those params
  // and serves a filtered grid from /api/employee/menu.
  import { Icon, Toggle } from "@tbite/ui";
  import { goto } from "$app/navigation";
  import { page } from "$app/stores";

  interface Props {
    tags: string[];
    q: string;
    selectedTags: string[];
    priceMin: number;
    priceMax: number;
    inStock: boolean;
    sort: string;
  }
  let { tags, q, selectedTags, priceMin, priceMax, inStock, sort }: Props = $props();

  // Local editable copies — committed to the URL on apply / toggle.
  let keyword = $state(q);
  let priceMinInput = $state(priceMin > 0 ? String(priceMin) : "");
  let priceMaxInput = $state(priceMax > 0 ? String(priceMax) : "");
  $effect(() => {
    keyword = q;
  });
  $effect(() => {
    priceMinInput = priceMin > 0 ? String(priceMin) : "";
  });
  $effect(() => {
    priceMaxInput = priceMax > 0 ? String(priceMax) : "";
  });

  const sortOptions = [
    { id: "", label: "預設排序" },
    { id: "name", label: "名稱" },
    { id: "price_asc", label: "價格：低到高" },
    { id: "price_desc", label: "價格：高到低" },
    { id: "remain", label: "剩餘份數" },
  ];

  const hasFilter = $derived(
    keyword.trim() !== "" ||
      selectedTags.length > 0 ||
      priceMinInput.trim() !== "" ||
      priceMaxInput.trim() !== "" ||
      inStock ||
      sort !== "",
  );

  // Rebuild the URL preserving plant/day, applying the current filter values.
  function apply(overrides: Partial<Record<string, string | string[] | boolean>> = {}) {
    const url = new URL($page.url);
    const sp = url.searchParams;
    const state: Record<string, string | string[] | boolean> = {
      q: keyword.trim(),
      tags: selectedTags,
      price_min: priceMinInput.trim(),
      price_max: priceMaxInput.trim(),
      in_stock: inStock,
      sort,
      ...overrides,
    };
    for (const key of ["q", "tags", "price_min", "price_max", "in_stock", "sort"]) {
      sp.delete(key);
    }
    if (typeof state.q === "string" && state.q) sp.set("q", state.q);
    if (Array.isArray(state.tags)) for (const t of state.tags) sp.append("tags", t);
    if (typeof state.price_min === "string" && state.price_min)
      sp.set("price_min", state.price_min);
    if (typeof state.price_max === "string" && state.price_max)
      sp.set("price_max", state.price_max);
    if (state.in_stock === true) sp.set("in_stock", "1");
    if (typeof state.sort === "string" && state.sort) sp.set("sort", state.sort);
    goto(url.pathname + url.search, { keepFocus: true, noScroll: true });
  }

  function toggleTag(tag: string) {
    const next = selectedTags.includes(tag)
      ? selectedTags.filter((t) => t !== tag)
      : [...selectedTags, tag];
    apply({ tags: next });
  }

  function clearAll() {
    keyword = "";
    priceMinInput = "";
    priceMaxInput = "";
    const url = new URL($page.url);
    for (const key of ["q", "tags", "price_min", "price_max", "in_stock", "sort"]) {
      url.searchParams.delete(key);
    }
    goto(url.pathname + url.search, { keepFocus: true, noScroll: true });
  }
</script>

<section class="mb-5 rounded-tb-2xl border border-tb-slate-200 bg-white p-4 shadow-tb-sm">
  <div class="flex flex-wrap items-end gap-3">
    <!-- Keyword -->
    <label class="flex min-w-[12rem] flex-1 flex-col gap-1.5">
      <span class="text-[11px] font-bold uppercase tracking-eyebrow text-tb-slate-500">關鍵字</span>
      <div class="relative">
        <Icon
          name="search"
          class="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-tb-slate-400"
        />
        <input
          type="text"
          bind:value={keyword}
          onkeydown={(e) => e.key === "Enter" && apply()}
          placeholder="搜尋餐點名稱…"
          class="w-full rounded-tb-lg border border-tb-slate-300 py-2 pl-9 pr-3 text-sm transition focus:border-tb-red-500 focus:outline-none focus:ring-4 focus:ring-tb-red-100"
        />
      </div>
    </label>

    <!-- Price range -->
    <div class="flex flex-col gap-1.5">
      <span class="text-[11px] font-bold uppercase tracking-eyebrow text-tb-slate-500"
        >價格區間</span
      >
      <div class="flex items-center gap-1.5">
        <input
          type="number"
          min="0"
          bind:value={priceMinInput}
          onkeydown={(e) => e.key === "Enter" && apply()}
          placeholder="最低"
          class="w-20 rounded-tb-lg border border-tb-slate-300 px-2 py-2 text-sm transition focus:border-tb-red-500 focus:outline-none focus:ring-4 focus:ring-tb-red-100"
        />
        <span class="text-tb-slate-400">–</span>
        <input
          type="number"
          min="0"
          bind:value={priceMaxInput}
          onkeydown={(e) => e.key === "Enter" && apply()}
          placeholder="最高"
          class="w-20 rounded-tb-lg border border-tb-slate-300 px-2 py-2 text-sm transition focus:border-tb-red-500 focus:outline-none focus:ring-4 focus:ring-tb-red-100"
        />
      </div>
    </div>

    <!-- Sort -->
    <label class="flex flex-col gap-1.5">
      <span class="text-[11px] font-bold uppercase tracking-eyebrow text-tb-slate-500">排序</span>
      <select
        value={sort}
        onchange={(e) => apply({ sort: e.currentTarget.value })}
        class="rounded-tb-lg border border-tb-slate-300 px-3 py-2 text-sm transition focus:border-tb-red-500 focus:outline-none focus:ring-4 focus:ring-tb-red-100"
      >
        {#each sortOptions as o (o.id)}
          <option value={o.id}>{o.label}</option>
        {/each}
      </select>
    </label>

    <!-- In-stock toggle -->
    <div class="flex flex-col gap-1.5">
      <span class="text-[11px] font-bold uppercase tracking-eyebrow text-tb-slate-500"
        >供應狀態</span
      >
      <div class="flex h-[38px] items-center">
        <Toggle on={inStock} onChange={(next) => apply({ in_stock: next })} label="僅顯示有貨" />
      </div>
    </div>

    <!-- Apply / clear -->
    <div class="flex items-center gap-2">
      <button
        type="button"
        onclick={() => apply()}
        class="rounded-tb-lg bg-tb-red-600 px-3.5 py-2 text-sm font-semibold text-white transition hover:bg-tb-red-700"
      >
        套用篩選
      </button>
      {#if hasFilter}
        <button
          type="button"
          onclick={clearAll}
          class="text-sm font-semibold text-tb-slate-500 transition hover:text-tb-slate-900"
        >
          清除
        </button>
      {/if}
    </div>
  </div>

  <!-- Health-tag chips -->
  {#if tags.length > 0}
    <div class="mt-3 flex flex-wrap items-center gap-2 border-t border-tb-slate-100 pt-3">
      <span class="text-[11px] font-bold uppercase tracking-eyebrow text-tb-slate-500"
        >健康標籤</span
      >
      {#each tags as tag (tag)}
        {@const on = selectedTags.includes(tag)}
        <button
          type="button"
          onclick={() => toggleTag(tag)}
          aria-pressed={on}
          class="rounded-full border px-3 py-1 text-xs font-semibold transition
            {on
            ? 'border-tb-red-500 bg-tb-red-50 text-tb-red-700'
            : 'border-tb-slate-300 bg-white text-tb-slate-600 hover:border-tb-slate-500'}"
        >
          {tag}
        </button>
      {/each}
    </div>
  {/if}
</section>
