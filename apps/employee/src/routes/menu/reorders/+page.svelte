<script lang="ts">
  import { onMount } from "svelte";
  import { deserialize } from "$app/forms";
  import ReorderChip from "$lib/components/ReorderChip.svelte";

  let { data, form } = $props();

  type ReorderC = {
    source_order_id: string;
    vendor_name: string;
    total_price_minor: number;
    freq: number;
    items_preview: string[] | null;
    available_today: boolean;
    vendor_id: string;
  };

  let chips = $state<ReorderC[]>((data.chips as ReorderC[]) ?? []);
  let nextCursor = $state<number | undefined>(data.nextCursor);
  let loading = $state(false);

  let toast = $state<string | null>(null);
  function showToast(text: string) {
    toast = text;
    setTimeout(() => (toast = null), 4500);
  }
  $effect(() => {
    const f = form as { reorderToast?: string } | null | undefined;
    if (f?.reorderToast) showToast(f.reorderToast);
  });

  let sentinel: HTMLDivElement | undefined;

  async function loadMore() {
    if (loading || nextCursor == null) return;
    loading = true;
    try {
      const fd = new FormData();
      fd.set("cursor", String(nextCursor));
      const r = await fetch("?/loadMore", {
        method: "POST",
        body: fd,
        headers: { "x-sveltekit-action": "true" },
      });
      const result = deserialize(await r.text()) as
        | { type: "success"; data?: { chips?: ReorderC[]; nextCursor?: number } }
        | { type: "failure"; data?: unknown }
        | { type: "error" | "redirect"; [k: string]: unknown };
      if (result.type === "success" && result.data) {
        const more = result.data;
        chips = [...chips, ...((more.chips as ReorderC[]) ?? [])];
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
  <header class="flex items-center justify-between">
    <div>
      <a href="/" class="text-xs text-tb-slate-500 hover:text-tb-slate-900">← 返回首頁</a>
      <h1 class="mt-1 text-2xl font-black text-tb-slate-900">✋ 再點一次</h1>
      <p class="mt-1 text-sm text-tb-slate-500">你最近 30 天的訂單，依商家頻率排序</p>
    </div>
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
      還沒有訂單紀錄 — 點完第一份午餐後就會出現
    </div>
  {:else}
    <div class="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-4">
      {#each chips as c (c.source_order_id)}
        <ReorderChip
          sourceOrderId={c.source_order_id}
          vendorName={c.vendor_name}
          totalPriceMinor={c.total_price_minor}
          freq={c.freq}
          itemsPreview={c.items_preview ?? []}
          availableToday={c.available_today}
          supplyDate={data.targetDay}
          action="?/reorderPast"
        />
      {/each}
    </div>
  {/if}

  <div bind:this={sentinel} class="h-8"></div>
  {#if loading}
    <p class="text-center text-xs text-tb-slate-500">載入中…</p>
  {/if}

  {#if toast}
    <div
      role="alert"
      aria-live="polite"
      class="fixed bottom-5 left-1/2 z-40 w-[min(28rem,calc(100vw-2rem))] -translate-x-1/2 rounded-tb-2xl border border-tb-amber-300 bg-tb-amber-50 px-4 py-3 text-sm text-tb-amber-700 shadow-tb-md"
    >
      {toast}
    </div>
  {/if}
</section>
