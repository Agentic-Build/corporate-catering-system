<script lang="ts">
  // Slide-in panel — ported from the drawer shells in EmployeeView.jsx
  // (TbCartDrawer) and MerchantView.jsx (MealLibraryDrawer).
  import type { Snippet } from "svelte";

  interface Props {
    open: boolean;
    onClose: () => void;
    side?: "left" | "right";
    maxWidth?: string;
    title?: string;
    header?: Snippet;
    children: Snippet;
    footer?: Snippet;
  }
  let {
    open,
    onClose,
    side = "right",
    maxWidth = "max-w-md",
    title,
    header,
    children,
    footer,
  }: Props = $props();

  let panel = $state<HTMLDivElement>();

  const FOCUSABLE =
    'a[href], button:not([disabled]), input, select, textarea, [tabindex]:not([tabindex="-1"])';

  function onKeydown(e: KeyboardEvent) {
    if (!open) return;
    if (e.key === "Escape") {
      onClose();
      return;
    }
    if (e.key === "Tab" && panel) {
      const focusable = Array.from(panel.querySelectorAll<HTMLElement>(FOCUSABLE));
      if (focusable.length === 0) {
        e.preventDefault();
        return;
      }
      const first = focusable.at(0)!;
      const last = focusable.at(-1)!;
      if (e.shiftKey) {
        if (document.activeElement === first) {
          e.preventDefault();
          last.focus();
        }
      } else {
        if (document.activeElement === last) {
          e.preventDefault();
          first.focus();
        }
      }
    }
  }

  const sideClass = $derived(side === "left" ? "left-0" : "right-0");
  const hiddenTransform = $derived(side === "left" ? "-translate-x-full" : "translate-x-full");

  // Focus management — move focus into the panel on open, restore it on close.
  $effect(() => {
    if (!open) return;
    const previouslyFocused = document.activeElement as HTMLElement | null;
    const first = panel?.querySelector<HTMLElement>(
      'a[href], button:not([disabled]), input, select, textarea, [tabindex]:not([tabindex="-1"])',
    );
    (first ?? panel)?.focus();
    return () => {
      previouslyFocused?.focus();
    };
  });
</script>

<svelte:window onkeydown={onKeydown} />

<div class="fixed inset-0 z-[70] transition {open ? 'pointer-events-auto' : 'pointer-events-none'}">
  <button
    type="button"
    aria-label="關閉"
    class="absolute inset-0 h-full w-full cursor-default bg-tb-slate-900/40 backdrop-blur-sm transition-opacity {open
      ? 'opacity-100'
      : 'opacity-0'}"
    onclick={onClose}
  ></button>
  <div
    bind:this={panel}
    class="absolute top-0 {sideClass} flex h-full w-full {maxWidth} flex-col bg-white shadow-2xl transition-transform {open
      ? 'translate-x-0'
      : hiddenTransform}"
    role="dialog"
    aria-modal="true"
    aria-label={title}
    tabindex="-1"
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
