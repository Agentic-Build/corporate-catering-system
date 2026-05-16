<script lang="ts">
  // 推薦你今天 — design-language pass. The recommendations API returns name /
  // price / reason only (no photo, stock or capacity), so items render as
  // recommendation cards in a grid rather than full MealCards.
  import { onMount } from "svelte";
  import { deserialize } from "$app/forms";
  import { PageHeader, EmptyState, Button, Icon } from "@tbite/ui";

  let { data } = $props();

  type RecommendC = {
    menu_item_id: string;
    name: string;
    unit_price: number;
    vendor_id: string;
    reason: string;
    score: number;
  };

  let chips = $state<RecommendC[]>((data.chips as RecommendC[]) ?? []);
  let nextCursor = $state<number | undefined>(data.nextCursor);
  let loading = $state(false);

  let sentinel: HTMLDivElement | undefined;

  async function loadMore() {
    if (loading || nextCursor == null) return;
    loading = true;
    try {
      const fd = new FormData();
      fd.set("cursor", String(nextCursor));
      const r = await fetch("?/loadMore", { method: "POST", body: fd });
      const result = deserialize(await r.text()) as
        | { type: "success"; data?: { chips?: RecommendC[]; nextCursor?: number } }
        | { type: "failure" | "error" | "redirect"; [k: string]: unknown };
      if (result.type === "success" && result.data) {
        chips = [...chips, ...((result.data.chips as RecommendC[]) ?? [])];
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
  eyebrow="For You · 推薦你今天"
  title="推薦你今天"
  subtitle="依同事熱門度與你的常用商家推算 · 一鍵加入今日預訂。"
/>

{#if data.error}
  <div
    class="rounded-tb-2xl border border-tb-rose-300 bg-tb-rose-50/60 p-4 text-sm text-tb-rose-700"
  >
    載入失敗：{data.error}
  </div>
{:else if chips.length === 0}
  <EmptyState
    icon="tag"
    title="尚無推薦"
    hint="正在收集你的偏好，先看看同事都點什麼吧。"
  />
{:else}
  <div class="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
    {#each chips as c (c.menu_item_id)}
      <article
        class="flex flex-col justify-between rounded-tb-2xl border border-tb-slate-200 bg-white p-4 shadow-tb-sm transition hover:-translate-y-0.5 hover:shadow-tb-md"
      >
        <div>
          <span
            class="rounded-full bg-tb-amber-50 px-2 py-0.5 text-[10px] font-semibold text-tb-amber-700"
            >{c.reason}</span
          >
          <h3 class="mt-2 text-sm font-bold leading-snug text-tb-slate-900">{c.name}</h3>
        </div>
        <div class="mt-3 flex items-center justify-between gap-2">
          <span class="font-jetbrains-mono text-base font-black tabular-nums text-tb-slate-900">
            ${c.unit_price.toLocaleString()}
          </span>
          <form method="POST" action="?/addToCart">
            <input type="hidden" name="menu_item_id" value={c.menu_item_id} />
            <Button variant="primary" size="sm" type="submit">
              <Icon name="plus" class="h-3.5 w-3.5" />加單
            </Button>
          </form>
        </div>
      </article>
    {/each}
  </div>
{/if}

<div bind:this={sentinel} class="h-8"></div>
{#if loading}
  <p class="text-center text-xs text-tb-slate-500">載入中…</p>
{/if}
