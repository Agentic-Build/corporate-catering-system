<script lang="ts">
  // Dashed-border empty card — modelled on the empty-state pattern in
  // EmployeePages.jsx (OrdersPage).
  import Icon, { type IconName } from "./Icon.svelte";

  interface Props {
    icon?: IconName;
    title: string;
    hint?: string;
  }
  let { icon, title, hint }: Props = $props();
</script>

<div
  class="grid place-items-center rounded-tb-2xl border border-dashed border-tb-slate-300 bg-white py-16 text-center"
>
  {#if icon}
    <Icon name={icon} class="h-8 w-8 text-tb-slate-300" />
  {/if}
  <p class="mt-2 text-sm font-semibold text-tb-slate-700">{title}</p>
  {#if hint}<p class="mt-1 text-xs text-tb-slate-500">{hint}</p>{/if}
</div>
