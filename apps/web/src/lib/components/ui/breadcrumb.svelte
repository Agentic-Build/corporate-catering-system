<script lang="ts">
  import type { BreadcrumbItem } from "$lib/platform/navigation";

  interface Props {
    items: BreadcrumbItem[];
  }

  let { items }: Props = $props();
</script>

<nav aria-label="breadcrumb" class="text-xs text-slate-500">
  <ol class="flex flex-wrap items-center gap-1">
    {#each items as item, index}
      <li class="flex items-center gap-1">
        {#if item.href}
          <a class="font-medium text-cyan-700 hover:text-cyan-900" href={item.href}>{item.label}</a>
        {:else}
          <span class="font-semibold text-slate-700">{item.label}</span>
        {/if}
        {#if index < items.length - 1}
          <span aria-hidden="true" class="text-slate-400">/</span>
        {/if}
      </li>
    {/each}
  </ol>
</nav>
