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

  function onKeydown(e: KeyboardEvent) {
    if (e.key === "Escape") onClose();
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
      class="relative w-[92%] {width} rounded-tb-2xl border border-tb-slate-200 bg-white p-5 shadow-xl"
      role="dialog"
      aria-modal="true"
      aria-label={title}
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
