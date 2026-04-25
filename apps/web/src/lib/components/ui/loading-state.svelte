<script lang="ts">
  interface Props {
    /** Number of skeleton rows to render. */
    rows?: number;
    /** Optional label shown above the skeleton. */
    label?: string;
    variant?: "list" | "card" | "inline";
  }

  let { rows = 3, label, variant = "list" }: Props = $props();
</script>

{#if variant === "inline"}
  <span class="inline-flex items-center gap-2 text-xs text-slate-500">
    <span class="inline-block h-3 w-3 animate-spin rounded-full border-2 border-slate-300 border-t-slate-600"></span>
    {label ?? "載入中"}
  </span>
{:else}
  <div class="grid gap-2" role="status" aria-live="polite" aria-busy="true">
    {#if label}
      <p class="text-xs text-slate-500">{label}</p>
    {/if}
    {#if variant === "card"}
      <div class="grid gap-3 rounded-xl border border-slate-200 bg-white p-4">
        <div class="h-4 w-32 animate-pulse rounded bg-slate-200"></div>
        <div class="h-3 w-full animate-pulse rounded bg-slate-100"></div>
        <div class="h-3 w-5/6 animate-pulse rounded bg-slate-100"></div>
        <div class="h-3 w-3/4 animate-pulse rounded bg-slate-100"></div>
      </div>
    {:else}
      <ul class="grid gap-2">
        {#each Array.from({ length: rows }) as _, index (index)}
          <li class="grid grid-cols-[1fr_auto] items-center gap-3 rounded-lg border border-slate-200 bg-white px-3 py-2">
            <span class="h-3 w-full animate-pulse rounded bg-slate-100"></span>
            <span class="h-3 w-16 animate-pulse rounded bg-slate-100"></span>
          </li>
        {/each}
      </ul>
    {/if}
  </div>
{/if}
