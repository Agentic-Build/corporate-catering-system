<script lang="ts">
  import type { Snippet } from "svelte";

  interface Props {
    title: string;
    icon?: string;
    moreHref?: string;
    moreLabel?: string;
    emptyHint?: string;
    isEmpty?: boolean;
    children?: Snippet;
  }
  let {
    title,
    icon,
    moreHref,
    moreLabel = "看更多",
    emptyHint,
    isEmpty = false,
    children,
  }: Props = $props();
</script>

<section class="space-y-2" aria-label={title}>
  <header class="flex items-center justify-between px-1">
    <h2 class="flex items-center gap-2 text-sm font-bold text-tb-slate-900">
      {#if icon}<span aria-hidden="true">{icon}</span>{/if}
      <span>{title}</span>
    </h2>
    {#if moreHref}
      <a
        href={moreHref}
        class="rounded-tb text-xs font-semibold text-tb-slate-500 hover:text-tb-slate-900 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-tb-amber-400"
        >《{moreLabel}》</a
      >
    {/if}
  </header>

  {#if isEmpty}
    <div
      class="rounded-tb-2xl border border-dashed border-tb-slate-200 bg-tb-slate-50 p-4 text-center text-xs text-tb-slate-500"
    >
      {emptyHint ?? ""}
    </div>
  {:else}
    <div
      role="list"
      class="flex snap-x snap-mandatory gap-2 overflow-x-auto pb-2 [scrollbar-width:thin]"
    >
      {@render children?.()}
    </div>
  {/if}
</section>
