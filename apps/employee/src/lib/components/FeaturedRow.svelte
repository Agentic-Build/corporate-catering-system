<script lang="ts" generics="T extends { key: string }">
  // Horizontal featured scroller — ported from EmployeeView.jsx TbFeaturedRow.
  // Generic over the row's items: the caller supplies a `card` snippet so the
  // same scroller serves both MealCard rows and the reorder-order card row.
  import type { Snippet } from "svelte";
  import { Icon } from "@tbite/ui";

  interface Props {
    title: string;
    subtitle?: string;
    moreHref?: string;
    items: T[];
    card: Snippet<[T]>;
    empty?: Snippet;
  }
  let { title, subtitle, moreHref, items, card, empty }: Props = $props();

  let scroller = $state<HTMLDivElement>();
  function scroll(dir: number) {
    scroller?.scrollBy({ left: dir * 360, behavior: "smooth" });
  }
</script>

<section class="mb-8">
  <div class="mb-3 flex items-end justify-between gap-3">
    <div>
      <h2 class="text-xl font-extrabold tracking-tight text-tb-slate-900">{title}</h2>
      {#if subtitle}<p class="text-sm text-tb-slate-500">{subtitle}</p>{/if}
    </div>
    <div class="flex items-center gap-2">
      {#if moreHref}
        <a href={moreHref} class="text-xs font-bold text-tb-red-700 hover:underline">查看全部</a>
      {/if}
      {#if items.length > 0}
        <div class="flex gap-1">
          <button
            type="button"
            onclick={() => scroll(-1)}
            class="grid h-8 w-8 place-items-center rounded-full border border-tb-slate-200 bg-white text-tb-slate-600 hover:border-tb-slate-400"
            aria-label="向左捲動"
          >
            <Icon name="chevron" class="h-4 w-4 rotate-90" />
          </button>
          <button
            type="button"
            onclick={() => scroll(1)}
            class="grid h-8 w-8 place-items-center rounded-full border border-tb-slate-200 bg-white text-tb-slate-600 hover:border-tb-slate-400"
            aria-label="向右捲動"
          >
            <Icon name="chevron" class="h-4 w-4 -rotate-90" />
          </button>
        </div>
      {/if}
    </div>
  </div>

  {#if items.length === 0}
    {#if empty}{@render empty()}{/if}
  {:else}
    <div
      bind:this={scroller}
      class="no-scrollbar -mx-4 flex gap-4 overflow-x-auto px-4 pb-2 md:-mx-8 md:px-8"
    >
      {#each items as item (item.key)}
        <div class="w-[300px] flex-shrink-0">{@render card(item)}</div>
      {/each}
    </div>
  {/if}
</section>
