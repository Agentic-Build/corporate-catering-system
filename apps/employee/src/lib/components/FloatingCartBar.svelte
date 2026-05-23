<script lang="ts">
  // Floating cart pill — ported from EmployeeView.jsx. Dark rounded bar fixed
  // at the bottom; shows the cart count + total and opens the cart drawer.
  import { Icon } from "@tbite/ui";
  import { cart } from "$lib/cart.svelte";

  interface Props {
    onOpen: () => void;
  }
  let { onOpen }: Props = $props();
</script>

{#if cart.count > 0}
  <!-- bottom-20 on mobile clears the fixed BottomNav; lg restores bottom-5. -->
  <div class="fixed inset-x-0 bottom-20 z-40 px-4 lg:bottom-5">
    <div
      class="mx-auto flex max-w-md items-center justify-between gap-3 rounded-full border border-tb-slate-800 bg-tb-slate-900 px-2 py-2 pl-5 text-white shadow-tb-md"
    >
      <div class="flex items-center gap-3">
        <span class="grid h-9 w-9 place-items-center rounded-full bg-tb-red-600">
          <Icon name="cart" class="h-4 w-4" />
        </span>
        <div>
          <div class="text-[11px] text-tb-slate-300">已選 {cart.count} 份 · 薪資代扣</div>
          <div class="font-jetbrains-mono text-base font-black leading-tight tabular-nums">
            ${cart.total.toLocaleString()}
          </div>
        </div>
      </div>
      <button
        type="button"
        onclick={onOpen}
        class="rounded-full bg-white px-4 py-2 text-sm font-bold text-tb-slate-900 transition hover:bg-tb-amber-300"
        >查看購物車 →</button
      >
    </div>
  </div>
{/if}
