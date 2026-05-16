<script lang="ts">
  // 我的常點 — design-language pass. FavoritesPage style from
  // EmployeePages.jsx: a No.1 hero card plus the remaining favourites as
  // rows. The favourites API has no frequency/last-ordered/photo fields, so
  // those reference embellishments are omitted (no fabricated data).
  import { onMount } from "svelte";
  import { deserialize } from "$app/forms";
  import { PageHeader, EmptyState, Button, Icon } from "@tbite/ui";

  let { data } = $props();

  type FavoriteC = {
    menu_item_id: string;
    name: string;
    unit_price: number;
    vendor_id: string;
    available_today: boolean;
  };

  let chips = $state<FavoriteC[]>((data.chips as FavoriteC[]) ?? []);
  let nextCursor = $state<string | undefined>(data.nextCursor);
  let loading = $state(false);

  let sentinel: HTMLDivElement | undefined;

  const hero = $derived(chips[0]);
  const rest = $derived(chips.slice(1));

  async function loadMore() {
    if (loading || !nextCursor) return;
    loading = true;
    try {
      const fd = new FormData();
      fd.set("cursor", nextCursor);
      const r = await fetch("?/loadMore", { method: "POST", body: fd });
      const result = deserialize(await r.text()) as
        | { type: "success"; data?: { chips?: FavoriteC[]; nextCursor?: string } }
        | { type: "failure" | "error" | "redirect"; [k: string]: unknown };
      if (result.type === "success" && result.data) {
        chips = [...chips, ...((result.data.chips as FavoriteC[]) ?? [])];
        nextCursor = result.data.nextCursor;
      }
    } finally {
      loading = false;
    }
  }

  onMount(() => {
    if (!sentinel || !("IntersectionObserver" in window)) return;
    const obs = new IntersectionObserver(
      (entries) => {
        for (const e of entries) if (e.isIntersecting) loadMore();
      },
      { rootMargin: "300px" },
    );
    obs.observe(sentinel);
    return () => obs.disconnect();
  });
</script>

<PageHeader
  eyebrow="Favorites · 我的常點"
  title="你收藏的菜色"
  subtitle="一鍵把收藏的餐點加進今日預訂；今日無供應的會標示出來。"
/>

{#if data.error}
  <div
    class="rounded-tb-2xl border border-tb-rose-300 bg-tb-rose-50/60 p-4 text-sm text-tb-rose-700"
  >
    載入失敗：{data.error}
  </div>
{:else if chips.length === 0}
  <EmptyState icon="heart" title="尚無收藏" hint="從今日菜單點 ⭐ 收藏喜歡的菜色。" />
{:else}
  <!-- Hero — favourite #1 -->
  <article
    class="mb-5 overflow-hidden rounded-tb-2xl border border-tb-amber-200 bg-gradient-to-br from-tb-amber-50 to-tb-rose-50"
  >
    <div class="flex flex-col gap-4 p-5 md:flex-row md:items-center">
      <div
        class="grid h-32 w-full flex-shrink-0 place-items-center rounded-tb-2xl bg-white/70 md:h-28 md:w-44"
      >
        <Icon name="heart" class="h-10 w-10 text-tb-amber-400" />
      </div>
      <div class="flex-1">
        <div class="text-[11px] font-bold uppercase tracking-eyebrow text-tb-amber-800">
          最常收藏 · No.1
        </div>
        <h2 class="mt-1 text-2xl font-black tracking-tight text-tb-slate-900">{hero.name}</h2>
        <p class="text-sm text-tb-slate-600">
          {hero.available_today ? "今日供應中" : "今日無供應"}
        </p>
        <div class="mt-3 flex items-center gap-2">
          <form method="POST" action="?/addToCart">
            <input type="hidden" name="menu_item_id" value={hero.menu_item_id} />
            <Button variant="primary" size="sm" type="submit" disabled={!hero.available_today}>
              <Icon name="plus" class="h-3.5 w-3.5" />
              立即加單 ${hero.unit_price.toLocaleString()}
            </Button>
          </form>
        </div>
      </div>
    </div>
  </article>

  {#if rest.length > 0}
    <h3 class="mb-2 text-base font-extrabold text-tb-slate-900">其餘常點 · {rest.length} 項</h3>
    <div class="grid grid-cols-1 gap-2.5 xl:grid-cols-2">
      {#each rest as f (f.menu_item_id)}
        <div
          class="flex items-center gap-4 rounded-tb-2xl border border-tb-slate-200 bg-white p-3 transition hover:shadow-tb-md
            {f.available_today ? '' : 'opacity-60'}"
        >
          <div
            class="grid h-16 w-16 flex-shrink-0 place-items-center rounded-tb-xl bg-tb-slate-100"
          >
            <Icon name="heart" class="h-6 w-6 text-tb-amber-400" />
          </div>
          <div class="min-w-0 flex-1">
            <div class="truncate text-base font-extrabold text-tb-slate-900">{f.name}</div>
            <div class="mt-1 text-[11px] text-tb-slate-500">
              {f.available_today ? "今日供應中" : "今日無供應"}
            </div>
          </div>
          <div class="flex flex-col items-end gap-2">
            <div class="font-jetbrains-mono text-lg font-black tabular-nums text-tb-slate-900">
              ${f.unit_price.toLocaleString()}
            </div>
            <form method="POST" action="?/addToCart">
              <input type="hidden" name="menu_item_id" value={f.menu_item_id} />
              <Button variant="primary" size="sm" type="submit" disabled={!f.available_today}>
                <Icon name="plus" class="h-3.5 w-3.5" />立即加單
              </Button>
            </form>
          </div>
        </div>
      {/each}
    </div>
  {/if}
{/if}

<div bind:this={sentinel} class="h-8"></div>
{#if loading}
  <p class="text-center text-xs text-tb-slate-500">載入中…</p>
{/if}
