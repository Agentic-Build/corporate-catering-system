<script lang="ts">
  // Plant + day selector. Matches the design-system `TbLocationBar`: a collapsed
  // capsule (pin + plant · day + chevron) that opens a dropdown panel listing
  // pickup plants and the day strip. Props are unchanged from the prior version
  // so callers need no edits.
  import Icon from "./Icon.svelte";

  interface PlantOption {
    id: string;
    label: string;
  }
  interface DayOption {
    id: string;
    head: string;
    sub?: string;
  }

  interface Props {
    plants: PlantOption[];
    selectedPlant: string;
    onPlantChange?: (id: string) => void;
    days: DayOption[];
    selectedDay: string;
    onDayChange?: (id: string) => void;
  }
  let { plants, selectedPlant, onPlantChange, days, selectedDay, onDayChange }: Props = $props();

  let open = $state(false);
  let root = $state<HTMLElement>();

  const currentPlant = $derived(plants.find((p) => p.id === selectedPlant));
  const currentDay = $derived(days.find((d) => d.id === selectedDay));

  // Close on outside click while the panel is open.
  $effect(() => {
    if (!open) return;
    function onDown(e: MouseEvent) {
      if (root && !root.contains(e.target as Node)) open = false;
    }
    document.addEventListener("mousedown", onDown);
    return () => document.removeEventListener("mousedown", onDown);
  });

  function pickPlant(id: string) {
    onPlantChange?.(id);
    open = false;
  }
  function pickDay(id: string) {
    onDayChange?.(id);
    open = false;
  }
</script>

<div class="relative" bind:this={root}>
  <button
    type="button"
    class="flex items-center gap-3 rounded-tb-full bg-tb-slate-100 px-4 py-2.5 text-sm transition hover:bg-tb-slate-200"
    onclick={() => (open = !open)}
    aria-expanded={open}
  >
    <span
      class="grid h-7 w-7 flex-shrink-0 place-items-center rounded-tb-full bg-tb-red-600 text-white"
    >
      <Icon name="pin" class="h-4 w-4" />
    </span>
    <div class="flex items-center gap-2 truncate">
      <span class="font-bold text-tb-slate-900">{currentPlant?.label ?? "選擇廠區"}</span>
      <span class="text-tb-slate-300">·</span>
      <span class="font-semibold text-tb-red-700">{currentDay?.head ?? ""}</span>
    </div>
    <Icon name="chevron" class="h-4 w-4 text-tb-slate-500 transition {open ? 'rotate-180' : ''}" />
  </button>

  {#if open}
    <div
      class="fade-up absolute left-0 top-full z-40 mt-2 w-[420px] max-w-[calc(100vw-2rem)] overflow-hidden rounded-tb-2xl border border-tb-slate-200 bg-white p-4 shadow-xl"
    >
      <div class="text-[11px] font-bold uppercase tracking-wider text-tb-slate-500">領餐廠區</div>
      <div class="mt-2 grid gap-1">
        {#each plants as p (p.id)}
          <button
            type="button"
            class="flex items-center justify-between rounded-tb-xl px-3 py-2 text-left text-sm transition
              {p.id === selectedPlant
              ? 'bg-tb-red-50 text-tb-red-900'
              : 'text-tb-slate-700 hover:bg-tb-slate-50'}"
            onclick={() => pickPlant(p.id)}
          >
            <span class="font-semibold">{p.label}</span>
            {#if p.id === selectedPlant}<Icon name="check" class="h-4 w-4 text-tb-red-600" />{/if}
          </button>
        {/each}
      </div>
      <div class="mt-3 text-[11px] font-bold uppercase tracking-wider text-tb-slate-500">取餐日</div>
      <div class="no-scrollbar mt-2 flex gap-2 overflow-x-auto pb-1">
        {#each days as d (d.id)}
          {@const on = d.id === selectedDay}
          <button
            type="button"
            class="min-w-[88px] flex-shrink-0 rounded-tb-xl border px-3 py-2 text-left transition
              {on
              ? 'border-tb-red-600 bg-tb-red-600 text-white'
              : 'border-tb-slate-200 bg-white text-tb-slate-800 hover:border-tb-slate-400'}"
            onclick={() => pickDay(d.id)}
          >
            <div class="text-sm font-bold">{d.head}</div>
            {#if d.sub}<div class="text-[10px] {on ? 'text-tb-red-50' : 'text-tb-slate-500'}">
                {d.sub}
              </div>{/if}
          </button>
        {/each}
      </div>
    </div>
  {/if}
</div>
