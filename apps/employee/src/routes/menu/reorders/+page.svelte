<script lang="ts">
  import { onMount } from "svelte";
  import { deserialize } from "$app/forms";
  import { PageHeader, EmptyState } from "@tbite/ui";

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
        chips = [...chips, ...((result.data.chips as ReorderC[]) ?? [])];
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
  eyebrow="Reorder · 再點一次"
  title="再點一次"
  subtitle="你最近 30 天的訂單，依商家頻率排序 · 一鍵重新預訂。"
/>

{#if data.error}
  <div
    class="rounded-tb-2xl border border-tb-rose-300 bg-tb-rose-50/60 p-4 text-sm text-tb-rose-700"
  >
    載入失敗：{data.error}
  </div>
{:else if chips.length === 0}
  <EmptyState icon="doc" title="尚無訂單紀錄" hint="點完第一份午餐後，這裡就會出現。" />
{:else}
  <div class="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
    {#each chips as c (c.source_order_id)}
      <form
        method="POST"
        action="?/reorderPast"
        class={c.available_today ? "" : "pointer-events-none opacity-50"}
      >
        <input type="hidden" name="source_order_id" value={c.source_order_id} />
        <input type="hidden" name="supply_date" value={data.targetDay} />
        <button
          type="submit"
          disabled={!c.available_today}
          class="flex w-full flex-col gap-2 rounded-tb-2xl border border-tb-slate-200 bg-white p-4 text-left shadow-tb-sm transition hover:-translate-y-0.5 hover:shadow-tb-md disabled:cursor-not-allowed"
        >
          <div class="flex items-center justify-between gap-2">
            <span class="truncate text-sm font-bold text-tb-slate-900">{c.vendor_name}</span>
            {#if c.freq > 1}
              <span
                class="shrink-0 rounded-full bg-tb-slate-100 px-2 py-0.5 text-[10px] font-semibold text-tb-slate-700"
                >× {c.freq}</span
              >
            {/if}
          </div>
          {#if c.items_preview && c.items_preview.length > 0}
            <p class="line-clamp-2 text-xs text-tb-slate-500">
              {c.items_preview.slice(0, 3).join("、")}
            </p>
          {/if}
          <div class="flex items-center justify-between">
            <span class="font-jetbrains-mono text-base font-black tabular-nums text-tb-slate-900">
              ${c.total_price_minor.toLocaleString()}
            </span>
            {#if c.available_today}
              <span class="text-xs font-bold text-tb-red-700">再點一次 →</span>
            {:else}
              <span class="text-[11px] font-semibold text-tb-rose-600">今日無供應</span>
            {/if}
          </div>
        </button>
      </form>
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
