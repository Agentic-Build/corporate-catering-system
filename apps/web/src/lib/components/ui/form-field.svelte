<script lang="ts">
  import type { Snippet } from "svelte";

  interface Props {
    label: string;
    hint?: string;
    error?: string;
    required?: boolean;
    for?: string;
    children: Snippet;
  }

  let { label, hint, error, required = false, for: forId, children }: Props = $props();
</script>

<label class="grid gap-1 text-sm" for={forId}>
  <span class="flex items-center gap-1 font-semibold text-slate-800">
    {label}
    {#if required}<span class="text-rose-600" aria-label="required">*</span>{/if}
  </span>
  {@render children()}
  {#if error}
    <span class="text-xs text-rose-700">{error}</span>
  {:else if hint}
    <span class="text-xs text-slate-500">{hint}</span>
  {/if}
</label>
