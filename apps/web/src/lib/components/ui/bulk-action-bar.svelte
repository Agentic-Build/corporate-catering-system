<script lang="ts">
  /**
   * Floating bulk-action bar. Shown when `count > 0`.
   *
   * Slot-based: parent passes action buttons via the `actions` snippet so it
   * can compose whatever bulk operations the page supports. The bar stays
   * sticky at the bottom of the viewport so users never lose access to it
   * while scrolling a long list.
   */
  import type { Snippet } from "svelte";
  import Button from "./button.svelte";

  interface Props {
    count: number;
    onclear: () => void;
    actions: Snippet;
    processing?: boolean;
  }

  let { count, onclear, actions, processing = false }: Props = $props();
</script>

{#if count > 0}
  <div class="fixed inset-x-0 bottom-0 z-40 border-t border-slate-200 bg-white/95 px-4 py-3 shadow-lg backdrop-blur md:bottom-4 md:left-1/2 md:-translate-x-1/2 md:rounded-2xl md:border md:shadow-2xl md:px-4">
    <div class="mx-auto flex max-w-5xl flex-wrap items-center justify-between gap-3">
      <div class="flex items-center gap-3">
        <span class="flex h-8 w-8 items-center justify-center rounded-full bg-cyan-600 text-xs font-bold text-white tabular-nums">
          {count}
        </span>
        <p class="text-sm font-semibold text-slate-800">已選取 {count} 項</p>
        <Button variant="ghost" size="sm" onclick={onclear} disabled={processing}>
          取消選取
        </Button>
      </div>
      <div class="flex flex-wrap items-center gap-2">
        {@render actions()}
      </div>
    </div>
  </div>
  <!-- Reserve space so content doesn't hide behind the bar on mobile -->
  <div class="h-20 md:h-0" aria-hidden="true"></div>
{/if}
