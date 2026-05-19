<script lang="ts">
  // Single meal row inside the vendor-detail list: thumbnail, name, desc,
  // remaining-stock bar, price and an inline quantity stepper.
  import type { MenuItem } from "$lib/api";
  import { money, plateColor } from "$lib/sample";
  import Plate from "./Plate.svelte";
  import QtyStepper from "./QtyStepper.svelte";

  interface Props {
    meal: MenuItem;
    qty: number;
    onChange: (next: number) => void;
  }
  let { meal, qty, onChange }: Props = $props();

  const color = $derived(plateColor(meal.vendor_id));
  const pct = $derived(
    meal.capacity > 0 ? Math.round((meal.remain / meal.capacity) * 100) : 0,
  );
  const barColor = $derived(
    meal.sold_out
      ? "bg-tb-slate-300"
      : pct <= 20
        ? "bg-tb-rose-500"
        : pct <= 40
          ? "bg-tb-amber-400"
          : "bg-tb-emerald-500",
  );
</script>

<article
  class="flex gap-3 rounded-2xl bg-white p-3.5 shadow-sm ring-1 {meal.sold_out
    ? 'opacity-70 ring-tb-slate-100'
    : 'ring-tb-slate-200/70'}"
>
  <Plate {color} class="h-20 w-20 flex-shrink-0 rounded-xl" label={meal.name.slice(0, 4)} />
  <div class="min-w-0 flex-1">
    <div class="text-sm font-extrabold leading-tight text-tb-slate-900">{meal.name}</div>
    <div class="mt-0.5 text-[11px] leading-snug text-tb-slate-500">{meal.description}</div>
    <div class="mt-2 flex items-center gap-1.5">
      <div class="h-1 w-16 overflow-hidden rounded-full bg-tb-slate-100">
        <div class="h-full rounded-full {barColor}" style="width: {pct}%"></div>
      </div>
      <span
        class="text-[10px] font-bold {meal.sold_out
          ? 'text-tb-slate-400'
          : pct <= 20
            ? 'text-tb-rose-600'
            : 'text-tb-slate-500'}"
      >
        {meal.sold_out ? "售罄" : pct <= 20 ? `剩 ${meal.remain} 份` : `${meal.remain} 份`}
      </span>
    </div>
    <div class="mt-2 flex items-center justify-between">
      <span class="text-base font-black tabular-nums text-tb-slate-900">
        {money(meal.price_minor)}
      </span>
      {#if meal.sold_out}
        <span class="rounded-full bg-tb-slate-100 px-3 py-1.5 text-[11px] font-bold text-tb-slate-400">
          售罄
        </span>
      {:else}
        <QtyStepper {qty} {onChange} />
      {/if}
    </div>
  </div>
</article>
