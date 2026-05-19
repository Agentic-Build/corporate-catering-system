<script lang="ts">
  // Modal bottom drawer: scrim + rounded sheet sliding from the bottom edge.
  // Shared by CartSheet, FilterSheet, EntryDetailSheet and NotifModal.
  import type { Snippet } from "svelte";

  interface Props {
    open: boolean;
    onClose: () => void;
    maxHeight?: string;
    children: Snippet;
  }
  let { open, onClose, maxHeight = "90%", children }: Props = $props();
</script>

<div
  class="absolute inset-0 z-50 transition-all {open
    ? 'pointer-events-auto'
    : 'pointer-events-none'}"
>
  <button
    type="button"
    aria-label="關閉"
    class="absolute inset-0 bg-tb-slate-900/50 transition-opacity {open
      ? 'opacity-100'
      : 'opacity-0'}"
    onclick={onClose}
  ></button>
  <div
    class="absolute inset-x-0 bottom-0 flex flex-col rounded-t-[32px] bg-white transition-transform duration-300 {open
      ? 'translate-y-0'
      : 'translate-y-full'}"
    style="max-height: {maxHeight}"
  >
    <div class="flex justify-center pt-3 pb-1">
      <div class="h-1 w-10 rounded-full bg-tb-slate-300"></div>
    </div>
    {@render children()}
  </div>
</div>
