<script lang="ts">
  interface WeekDay {
    id: string;
    weekday: string;
    dom: string;
    isToday: boolean;
  }
  interface Props {
    days: WeekDay[];
    selectedDay: string;
    onSelect: (id: string) => void;
  }
  let { days, selectedDay, onSelect }: Props = $props();
</script>

<div class="grid grid-cols-7 gap-1.5">
  {#each days as d (d.id)}
    {@const on = d.id === selectedDay}
    <button
      type="button"
      onclick={() => onSelect(d.id)}
      aria-pressed={on}
      class="flex flex-col items-center rounded-tb-xl border py-2 transition
        {on
        ? 'border-tb-red-600 bg-tb-red-600 text-white shadow-tb-sm'
        : 'border-tb-slate-200 bg-white text-tb-slate-700 hover:border-tb-slate-400'}"
    >
      <span class="text-[10px] font-semibold {on ? 'text-tb-red-50' : 'text-tb-slate-400'}">
        週{d.weekday}
      </span>
      <span class="text-lg font-black tabular-nums">{d.dom}</span>
      {#if d.isToday}
        <span class="text-[9px] font-bold {on ? 'text-tb-red-50' : 'text-tb-red-600'}">今天</span>
      {:else}
        <span class="text-[9px]">&nbsp;</span>
      {/if}
    </button>
  {/each}
</div>
