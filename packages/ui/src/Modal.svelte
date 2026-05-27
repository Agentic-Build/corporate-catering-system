<script lang="ts">
  // Ported from reference_src/ui.jsx Modal — scrim + centred card.
  import type { Snippet } from "svelte";
  import Icon from "./Icon.svelte";

  interface Props {
    open: boolean;
    onClose: () => void;
    title: string;
    width?: string;
    children: Snippet;
    footer?: Snippet;
  }
  let { open, onClose, title, width = "max-w-md", children, footer }: Props = $props();

  let dialog = $state<HTMLDivElement>();

  const FOCUSABLE =
    'a[href], button:not([disabled]), input, select, textarea, [tabindex]:not([tabindex="-1"])';

  function onKeydown(e: KeyboardEvent) {
    if (!open) return;
    if (e.key === "Escape") {
      onClose();
      return;
    }
    if (e.key === "Tab" && dialog) {
      const focusable = Array.from(dialog.querySelectorAll<HTMLElement>(FOCUSABLE));
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

  // Lock body scroll while the modal is open.
  $effect(() => {
    if (!open) return;
    const prev = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    return () => {
      document.body.style.overflow = prev;
    };
  });

  // Focus management — move focus into the dialog on open, restore it on close.
  $effect(() => {
    if (!open) return;
    const previouslyFocused = document.activeElement as HTMLElement | null;
    const first = dialog?.querySelector<HTMLElement>(
      'a[href], button:not([disabled]), input, select, textarea, [tabindex]:not([tabindex="-1"])',
    );
    (first ?? dialog)?.focus();
    return () => {
      previouslyFocused?.focus();
    };
  });
</script>

<svelte:window onkeydown={onKeydown} />

{#if open}
  <div class="fixed inset-0 z-[80] grid place-items-center fade-up">
    <button
      type="button"
      class="absolute inset-0 h-full w-full cursor-default bg-tb-slate-900/40 backdrop-blur-sm"
      aria-label="關閉"
      onclick={onClose}
    ></button>
    <div
      bind:this={dialog}
      class="relative w-[92%] {width} rounded-tb-2xl border border-tb-slate-200 bg-white p-5 shadow-2xl"
      role="dialog"
      aria-modal="true"
      aria-label={title}
      tabindex="-1"
    >
      <div class="mb-3 flex items-start justify-between gap-3">
        <h2 class="text-lg font-bold text-tb-slate-900">{title}</h2>
        <button
          type="button"
          onclick={onClose}
          class="rounded-lg p-1.5 text-tb-slate-500 transition hover:bg-tb-slate-100"
          aria-label="關閉"
        >
          <Icon name="close" class="h-4 w-4" />
        </button>
      </div>
      <div>{@render children()}</div>
      {#if footer}
        <div class="mt-4 flex justify-end gap-2">{@render footer()}</div>
      {/if}
    </div>
  </div>
{/if}
