<script lang="ts" module>
  // Shared filter state shape for the home grid and vendor-detail list.
  export interface MealFilters {
    search: string;
    priceMin: string;
    priceMax: string;
    sortBy: "default" | "name" | "price_asc" | "price_desc";
    showAvail: boolean;
    tags: string[];
  }

  export function emptyFilters(): MealFilters {
    return { search: "", priceMin: "", priceMax: "", sortBy: "default", showAvail: false, tags: [] };
  }

  export function filtersActive(f: MealFilters): boolean {
    return (
      f.search !== "" ||
      f.priceMin !== "" ||
      f.priceMax !== "" ||
      f.sortBy !== "default" ||
      f.showAvail ||
      f.tags.length > 0
    );
  }
</script>

<script lang="ts">
  // Bottom-drawer filter & sort panel.
  import { QUICK_TAGS } from "$lib/sample";
  import AppIcon from "./AppIcon.svelte";
  import BottomSheet from "./BottomSheet.svelte";

  interface Props {
    open: boolean;
    onClose: () => void;
    filters: MealFilters;
    onChange: (next: MealFilters) => void;
    onReset: () => void;
  }
  let { open, onClose, filters, onChange, onReset }: Props = $props();

  const SORTS: { v: MealFilters["sortBy"]; l: string }[] = [
    { v: "default", l: "預設排序" },
    { v: "name", l: "名稱" },
    { v: "price_asc", l: "價格:低到高" },
    { v: "price_desc", l: "價格:高到低" },
  ];

  function patch(p: Partial<MealFilters>) {
    onChange({ ...filters, ...p });
  }
  function toggleTag(t: string) {
    patch({
      tags: filters.tags.includes(t)
        ? filters.tags.filter((x) => x !== t)
        : [...filters.tags, t],
    });
  }
</script>

<BottomSheet {open} {onClose}>
  <div class="flex items-center justify-between border-b border-tb-slate-100 px-5 py-3">
    <h2 class="text-lg font-extrabold text-tb-slate-900">篩選與排序</h2>
    <button
      type="button"
      class="grid h-8 w-8 place-items-center rounded-full bg-tb-slate-100 text-lg text-tb-slate-600"
      onclick={onClose}
    >
      ✕
    </button>
  </div>

  <div class="no-scroll grid flex-1 gap-4 overflow-y-auto px-5 py-4">
    <div>
      <div class="mb-1.5 text-xs font-bold text-tb-slate-700">關鍵字</div>
      <div class="relative">
        <AppIcon
          name="search"
          class="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-tb-slate-400"
        />
        <input
          value={filters.search}
          oninput={(e) => patch({ search: e.currentTarget.value })}
          placeholder="搜尋餐點名稱…"
          class="w-full rounded-xl bg-tb-slate-50 py-2.5 pl-9 pr-3 text-sm outline-none ring-1 ring-tb-slate-200 focus:ring-tb-slate-400"
        />
      </div>
    </div>

    <div>
      <div class="mb-1.5 text-xs font-bold text-tb-slate-700">價格區間</div>
      <div class="flex items-center gap-2">
        <input
          type="number"
          value={filters.priceMin}
          oninput={(e) => patch({ priceMin: e.currentTarget.value })}
          placeholder="最低"
          class="flex-1 rounded-xl bg-tb-slate-50 px-3 py-2.5 text-center text-sm outline-none ring-1 ring-tb-slate-200 focus:ring-tb-slate-400"
        />
        <span class="text-tb-slate-400">–</span>
        <input
          type="number"
          value={filters.priceMax}
          oninput={(e) => patch({ priceMax: e.currentTarget.value })}
          placeholder="最高"
          class="flex-1 rounded-xl bg-tb-slate-50 px-3 py-2.5 text-center text-sm outline-none ring-1 ring-tb-slate-200 focus:ring-tb-slate-400"
        />
      </div>
    </div>

    <div>
      <div class="mb-1.5 text-xs font-bold text-tb-slate-700">排序</div>
      <div class="grid gap-1.5">
        {#each SORTS as s (s.v)}
          <button
            type="button"
            onclick={() => patch({ sortBy: s.v })}
            class="w-full rounded-xl px-3 py-2.5 text-left text-sm font-bold transition {filters.sortBy ===
            s.v
              ? 'bg-tb-sky-600 text-white'
              : 'bg-tb-slate-100 text-tb-slate-700'}"
          >
            {filters.sortBy === s.v ? "✓ " : ""}{s.l}
          </button>
        {/each}
      </div>
    </div>

    <div class="flex items-center justify-between">
      <span class="text-xs font-bold text-tb-slate-700">供應狀態</span>
      <button
        type="button"
        aria-label="僅顯示有貨"
        onclick={() => patch({ showAvail: !filters.showAvail })}
        class="relative inline-flex h-7 w-12 items-center rounded-full transition {filters.showAvail
          ? 'bg-tb-emerald-600'
          : 'bg-tb-slate-300'}"
      >
        <span
          class="inline-block h-5 w-5 rounded-full bg-white shadow transition {filters.showAvail
            ? 'translate-x-6'
            : 'translate-x-1'}"
        ></span>
      </button>
    </div>

    <div>
      <div class="mb-1.5 text-xs font-bold text-tb-slate-700">快速標籤</div>
      <div class="flex flex-wrap gap-1.5">
        {#each QUICK_TAGS as t (t)}
          <button
            type="button"
            onclick={() => toggleTag(t)}
            class="rounded-full px-3 py-1.5 text-xs font-bold {filters.tags.includes(t)
              ? 'bg-tb-slate-900 text-white'
              : 'bg-tb-slate-100 text-tb-slate-700 ring-1 ring-tb-slate-200'}"
          >
            {t}
          </button>
        {/each}
      </div>
    </div>
  </div>

  <div class="flex gap-2 border-t border-tb-slate-100 px-5 py-4">
    <button
      type="button"
      onclick={() => {
        onReset();
        onClose();
      }}
      class="flex-1 rounded-2xl bg-tb-slate-100 py-3.5 text-sm font-bold text-tb-slate-700"
    >
      清除篩選
    </button>
    <button
      type="button"
      onclick={onClose}
      class="flex-1 rounded-2xl bg-tb-red-600 py-3.5 text-sm font-bold text-white"
    >
      套用篩選
    </button>
  </div>
</BottomSheet>
