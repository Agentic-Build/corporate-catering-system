<script lang="ts">
  import type { Snippet } from "svelte";
  import type { BreadcrumbItem } from "$lib/platform/navigation";
  import Breadcrumb from "./breadcrumb.svelte";

  interface Props {
    title: string;
    description?: string;
    eyebrow?: string;
    breadcrumbs?: BreadcrumbItem[];
    actions?: Snippet;
    meta?: Snippet;
  }

  let { title, description, eyebrow, breadcrumbs, actions, meta }: Props = $props();
</script>

<header class="mb-5 grid gap-3 border-b border-slate-200 pb-4">
  {#if breadcrumbs && breadcrumbs.length > 0}
    <Breadcrumb items={breadcrumbs} />
  {/if}
  <div class="flex flex-wrap items-start justify-between gap-3">
    <div class="grid gap-1">
      {#if eyebrow}
        <p class="text-xs font-semibold tracking-[0.14em] text-cyan-700">{eyebrow}</p>
      {/if}
      <h1 class="text-xl font-bold text-slate-950 md:text-2xl">{title}</h1>
      {#if description}
        <p class="text-sm text-slate-600">{description}</p>
      {/if}
    </div>
    {#if actions}
      <div class="flex flex-wrap gap-2">
        {@render actions()}
      </div>
    {/if}
  </div>
  {#if meta}
    <div class="grid gap-2 md:grid-cols-3 lg:grid-cols-4">
      {@render meta()}
    </div>
  {/if}
</header>
