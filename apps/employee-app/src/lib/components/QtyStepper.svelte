<script lang="ts">
  // Quantity control: a single "+" button when empty, expanding to a
  // red −/count/+ pill once the item is in the cart.
  import AppIcon from "./AppIcon.svelte";

  interface Props {
    qty: number;
    onChange: (next: number) => void;
  }
  let { qty, onChange }: Props = $props();
</script>

{#if qty > 0}
  <div class="flex items-center gap-1 rounded-full bg-tb-red-600 p-1">
    <button
      type="button"
      aria-label="減少數量"
      class="grid h-7 w-7 place-items-center rounded-full bg-tb-red-700 text-white"
      onclick={() => onChange(qty - 1)}
    >
      <AppIcon name="minus" class="h-3.5 w-3.5" />
    </button>
    <span class="w-6 text-center text-sm font-black tabular-nums text-white">{qty}</span>
    <button
      type="button"
      aria-label="增加數量"
      class="grid h-7 w-7 place-items-center rounded-full bg-tb-red-700 text-white"
      onclick={() => onChange(qty + 1)}
    >
      <AppIcon name="plus" class="h-3.5 w-3.5" />
    </button>
  </div>
{:else}
  <button
    type="button"
    aria-label="加入購物車"
    class="grid h-9 w-9 place-items-center rounded-full bg-tb-red-600 text-white shadow-sm transition active:scale-95"
    onclick={() => onChange(1)}
  >
    <AppIcon name="plus" class="h-4 w-4" />
  </button>
{/if}
