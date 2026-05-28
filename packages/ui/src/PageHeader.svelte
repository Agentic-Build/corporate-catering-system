<script lang="ts">
  import type { Snippet } from "svelte";

  interface Props {
    eyebrow: string;
    title: string;
    subtitle?: string;
    actions?: Snippet;
  }
  let { eyebrow, title, subtitle, actions }: Props = $props();
</script>

<section class="mb-6 flex flex-wrap items-end justify-between gap-3">
  <div>
    <div class="text-[11px] font-bold uppercase tracking-eyebrow text-tb-red-600">{eyebrow}</div>
    <h1 class="mt-1 text-3xl font-black tracking-tight text-tb-slate-900">{title}</h1>
    {#if subtitle}<p class="mt-1 text-sm text-tb-slate-500">{subtitle}</p>{/if}
  </div>
  {#if actions}
    <div class="flex flex-wrap items-center gap-2">{@render actions()}</div>
  {/if}
</section>
