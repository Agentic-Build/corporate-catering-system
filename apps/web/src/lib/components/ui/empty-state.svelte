<script lang="ts">
  import type { Snippet } from "svelte";

  interface Props {
    title: string;
    description?: string;
    actions?: Snippet;
  }

  let { title, description, actions }: Props = $props();
</script>

<div class="grid gap-3 rounded-xl border border-dashed border-slate-300 bg-slate-50/60 p-6 text-center">
  <h4 class="text-sm font-semibold text-slate-800">{title}</h4>
  {#if description}
    <p class="text-sm text-slate-600">{description}</p>
  {/if}
  {#if actions}
    <div class="flex justify-center gap-2">
      {@render actions()}
    </div>
  {/if}
</div>
