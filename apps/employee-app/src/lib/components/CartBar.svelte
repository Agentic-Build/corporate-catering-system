<script lang="ts">
  // Sticky "view cart" bar, shown above the bottom nav whenever the cart
  // has items. Mirrors the mockup's slide-up dark pill.
  import { cart } from "$lib/cart.svelte";
  import { money } from "$lib/sample";

  interface Props {
    onOpen: () => void;
    /** Extra bottom offset, e.g. to clear the bottom nav. */
    bottom?: string;
  }
  let { onOpen, bottom = "5rem" }: Props = $props();
</script>

{#if cart.count > 0}
  <div class="slide-up absolute inset-x-4 z-40" style="bottom: {bottom}">
    <button
      type="button"
      onclick={onOpen}
      class="flex w-full items-center justify-between rounded-2xl bg-tb-slate-900 px-4 py-3.5 text-white shadow-2xl"
    >
      <div class="flex items-center gap-2">
        <span class="grid h-7 w-7 place-items-center rounded-full bg-tb-red-600 text-xs font-black">
          {cart.count}
        </span>
        <span class="text-sm font-bold">查看購物車</span>
      </div>
      <span class="text-base font-black tabular-nums">{money(cart.total)}</span>
    </button>
  </div>
{/if}
