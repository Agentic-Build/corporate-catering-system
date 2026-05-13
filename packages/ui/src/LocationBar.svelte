<script lang="ts">
  interface PlantOption { id: string; label: string; }
  interface DayOption { id: string; head: string; sub?: string; }

  interface Props {
    plants: PlantOption[];
    selectedPlant: string;
    onPlantChange?: (id: string) => void;
    days: DayOption[];
    selectedDay: string;
    onDayChange?: (id: string) => void;
  }
  let { plants, selectedPlant, onPlantChange, days, selectedDay, onDayChange }: Props = $props();
</script>

<div class="flex flex-wrap items-center gap-3 rounded-tb-2xl border border-tb-slate-200 bg-white p-2 shadow-tb-sm">
  <div class="flex flex-wrap gap-1 rounded-full bg-tb-slate-100 p-1">
    {#each plants as p (p.id)}
      <button
        type="button"
        class="rounded-full px-3 py-1.5 text-xs font-semibold transition
          {selectedPlant === p.id ? 'bg-tb-slate-900 text-white shadow-tb-sm' : 'text-tb-slate-700 hover:text-tb-slate-950'}"
        onclick={() => onPlantChange?.(p.id)}
      >{p.label}</button>
    {/each}
  </div>
  <div class="flex flex-wrap gap-1 rounded-full bg-tb-slate-100 p-1">
    {#each days as d (d.id)}
      <button
        type="button"
        class="flex flex-col items-center rounded-full px-3 py-1 text-xs font-semibold leading-tight transition
          {selectedDay === d.id ? 'bg-tb-slate-900 text-white shadow-tb-sm' : 'text-tb-slate-700 hover:text-tb-slate-950'}"
        onclick={() => onDayChange?.(d.id)}
      >
        <span>{d.head}</span>
        {#if d.sub}<span class="text-[10px] font-medium opacity-75">{d.sub}</span>{/if}
      </button>
    {/each}
  </div>
</div>
