<script lang="ts">
  // Slide-in panel — ported from the drawer shells in EmployeeView.jsx
  // (TbCartDrawer) and MerchantView.jsx (MealLibraryDrawer).
  import type { Snippet } from "svelte";

  interface Props {
    open: boolean;
    onClose: () => void;
    side?: "left" | "right";
    maxWidth?: string;
    header?: Snippet;
    children: Snippet;
    footer?: Snippet;
  }
  let {
    open,
    onClose,
    side = "right",
    maxWidth = "max-w-md",
    header,
    children,
    footer,
  }: Props = $props();

  function onKeydown(e: KeyboardEvent) {
    if (e.key === "Escape" && open) onClose();
  }

  const sideClass = $derived(side === "left" ? "left-0" : "right-0");
  const hiddenTransform = $derived(side === "left" ? "-translate-x-full" : "translate-x-full");
</script>

<svelte:window onkeydown={onKeydown} />

<div
  class="fixed inset-0 z-[70] transition {open ? 'pointer-events-auto' : 'pointer-events-none'}"
>
  <button
    type="button"
    aria-label="關閉"
    class="absolute inset-0 h-full w-full cursor-default bg-tb-slate-900/40 backdrop-blur-sm transition-opacity {open
      ? 'opacity-100'
      : 'opacity-0'}"
    onclick={onClose}
  ></button>
  <div
    class="absolute top-0 {sideClass} flex h-full w-full {maxWidth} flex-col bg-white shadow-xl transition-transform {open
      ? 'translate-x-0'
      : hiddenTransform}"
    role="dialog"
    aria-modal="true"
  >
    {#if header}
      <header class="border-b border-tb-slate-200 px-5 py-4">{@render header()}</header>
    {/if}
    <div class="flex-1 overflow-y-auto px-5 py-4">{@render children()}</div>
    {#if footer}
      <footer class="border-t border-tb-slate-200 bg-tb-slate-50/60 px-5 py-4">
        {@render footer()}
      </footer>
    {/if}
  </div>
</div>
