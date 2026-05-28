<script lang="ts">
  // Day picker strip with per-day status pip.
  interface Day {
    id: string;
    head: string;
    weekday: string;
    offset: number;
  }
  interface Slot {
    cap: number;
    ordered: number;
  }
  interface Props {
    days: Day[];
    supplyByDate: Record<string, Slot[]>;
    selected: string;
    onSelect: (id: string) => void;
  }
  let { days, supplyByDate, selected, onSelect }: Props = $props();

  function statusFor(day: Day, slots: Slot[]) {
    const empty = slots.length === 0;
    if (day.offset === 0) return { tone: "bg-tb-slate-200 text-tb-slate-700", label: "備餐中" };
    if (day.offset === 1) return { tone: "bg-tb-red-100 text-tb-red-800", label: "今 17:00 截單" };
    if (empty) return { tone: "bg-tb-amber-100 text-tb-amber-800", label: "尚未排菜" };
    return { tone: "bg-tb-emerald-100 text-tb-emerald-800", label: "接受預訂中" };
  }
</script>

<div class="no-scrollbar flex gap-2 overflow-x-auto pb-1">
  {#each days as d (d.id)}
    {@const slots = supplyByDate[d.id] ?? []}
    {@const on = d.id === selected}
    {@const total = slots.reduce((s, x) => s + x.cap, 0)}
    {@const ordered = slots.reduce((s, x) => s + x.ordered, 0)}
    {@const status = statusFor(d, slots)}
    <button
      type="button"
      onclick={() => onSelect(d.id)}
      class="min-w-[140px] flex-shrink-0 rounded-tb-2xl border px-3 py-3 text-left transition {on
        ? 'border-tb-red-600 bg-tb-red-50/40 shadow-tb-sm'
        : 'border-tb-slate-200 bg-white hover:border-tb-slate-400'}"
    >
      <div class="flex items-baseline justify-between">
        <div class="text-sm font-extrabold {on ? 'text-tb-red-700' : 'text-tb-slate-900'}">
          {d.head}
        </div>
        <div class="font-jetbrains-mono text-[10px] text-tb-slate-500">{d.weekday}</div>
      </div>
      <div class="mt-2 flex items-baseline gap-1">
        <span
          class="text-base font-black tabular-nums {on ? 'text-tb-red-700' : 'text-tb-slate-900'}"
          >{slots.length}</span
        >
        <span class="text-[10px] text-tb-slate-500">道菜 · </span>
        <span class="text-[10px] tabular-nums text-tb-slate-500">{ordered}/{total}</span>
      </div>
      <span class="mt-2 inline-block rounded-full px-2 py-0.5 text-xs font-bold {status.tone}">
        {status.label}
      </span>
    </button>
  {/each}
</div>
