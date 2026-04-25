<script lang="ts">
  import { toasts, type ToastEntry } from "./toast-store";

  let items = $state<ToastEntry[]>([]);

  toasts.subscribe((list) => {
    items = list;
  });

  function toneClass(tone: string): string {
    if (tone === "success") return "border-emerald-200 bg-emerald-50 text-emerald-900";
    if (tone === "error") return "border-rose-200 bg-rose-50 text-rose-900";
    return "border-cyan-200 bg-cyan-50 text-cyan-900";
  }
</script>

{#if items.length > 0}
  <div class="pointer-events-none fixed inset-x-0 bottom-4 z-50 grid justify-items-center gap-2 px-4">
    {#each items as toast (toast.id)}
      <button
        type="button"
        class={`pointer-events-auto w-full max-w-md rounded-xl border px-4 py-3 text-sm shadow-lg backdrop-blur ${toneClass(toast.tone)}`}
        onclick={() => toasts.dismiss(toast.id)}
      >
        {toast.message}
      </button>
    {/each}
  </div>
{/if}
