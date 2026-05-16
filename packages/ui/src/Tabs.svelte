<script lang="ts">
  // Ported from the OrdersPage tab bar in EmployeePages.jsx —
  // underline style with an optional count pill.
  interface Tab {
    id: string;
    label: string;
    count?: number;
  }
  interface Props {
    tabs: Tab[];
    active: string;
    onChange: (id: string) => void;
  }
  let { tabs, active, onChange }: Props = $props();
</script>

<div class="mb-5 flex flex-wrap items-center gap-2 border-b border-tb-slate-200">
  {#each tabs as t (t.id)}
    {@const on = t.id === active}
    <button
      type="button"
      onclick={() => onChange(t.id)}
      class="relative -mb-px flex items-center gap-2 border-b-2 px-3 py-2.5 text-sm font-bold transition
        {on
        ? 'border-tb-red-600 text-tb-red-700'
        : 'border-transparent text-tb-slate-500 hover:text-tb-slate-900'}"
    >
      {t.label}
      {#if t.count !== undefined}
        <span
          class="grid h-5 min-w-[20px] place-items-center rounded-full px-1.5 text-[10px] font-bold tabular-nums
            {on ? 'bg-tb-red-100 text-tb-red-800' : 'bg-tb-slate-100 text-tb-slate-600'}"
        >{t.count}</span>
      {/if}
    </button>
  {/each}
</div>
