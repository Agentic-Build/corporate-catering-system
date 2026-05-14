<script lang="ts">
  import { onMount } from "svelte";
  import { deserialize } from "$app/forms";
  import RecommendChip from "$lib/components/RecommendChip.svelte";

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
        const more = result.data;
        chips = [...chips, ...((more.chips as RecommendC[]) ?? [])];
        nextCursor = more.nextCursor;
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

<section class="space-y-4 pb-12">
  <header>
    <a href="/" class="text-xs text-tb-slate-500 hover:text-tb-slate-900">← 返回首頁</a>
    <h1 class="mt-1 text-2xl font-black text-tb-slate-900">✨ 推薦你今天</h1>
    <p class="mt-1 text-sm text-tb-slate-500">同事熱門 × 你的常用商家</p>
  </header>

  {#if data.error}
    <div
      class="rounded-tb-2xl border border-tb-rose-300 bg-tb-rose-50/60 p-4 text-sm text-tb-rose-700"
    >
      載入失敗：{data.error}
    </div>
  {/if}

  {#if chips.length === 0 && !data.error}
    <div
      class="rounded-tb-2xl border border-dashed border-tb-slate-200 bg-tb-slate-50 p-8 text-center text-sm text-tb-slate-500"
    >
      正在收集你的偏好，先看看同事都點什麼吧
    </div>
  {:else}
    <div class="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-4">
      {#each chips as c (c.menu_item_id)}
        <RecommendChip
          menuItemId={c.menu_item_id}
          name={c.name}
          unitPrice={c.unit_price}
          reason={c.reason}
          action="?/addToCart"
        />
      {/each}
    </div>
  {/if}

  <div bind:this={sentinel} class="h-8"></div>
  {#if loading}
    <p class="text-center text-xs text-tb-slate-500">載入中…</p>
  {/if}
</section>
