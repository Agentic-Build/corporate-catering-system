<script lang="ts">
  interface Props {
    sourceOrderId: string;
    vendorName: string;
    totalPriceMinor: number;
    freq: number;
    itemsPreview: string[];
    availableToday: boolean;
    supplyDate: string;
    action?: string;
  }
  let {
    sourceOrderId,
    vendorName,
    totalPriceMinor,
    freq,
    itemsPreview,
    availableToday,
    supplyDate,
    action = "/?/reorderPast",
  }: Props = $props();

  const preview = $derived(itemsPreview.slice(0, 2).join("、"));
  const disabled = $derived(!availableToday);
</script>

<form
  method="POST"
  {action}
  class="shrink-0 snap-start {disabled ? 'pointer-events-none opacity-50' : ''}"
>
  <input type="hidden" name="source_order_id" value={sourceOrderId} />
  <input type="hidden" name="supply_date" value={supplyDate} />
  <button
    type="submit"
    {disabled}
    class="flex w-44 flex-col items-start gap-1 rounded-tb-2xl border border-tb-slate-200 bg-white p-3 text-left shadow-tb-sm transition hover:-translate-y-0.5 hover:shadow-tb-md disabled:cursor-not-allowed"
    aria-label={disabled ? `${vendorName} 今日無供應` : `再點 ${vendorName}`}
  >
    <div class="flex w-full items-center justify-between gap-1">
      <span class="truncate text-xs font-bold text-tb-slate-900">{vendorName}</span>
      {#if freq > 1}
        <span
          class="shrink-0 rounded-full bg-tb-slate-100 px-1.5 py-0.5 text-[10px] font-semibold text-tb-slate-700"
          >× {freq}</span
        >
      {/if}
    </div>
    {#if preview}
      <p class="line-clamp-1 text-[11px] text-tb-slate-500">{preview}</p>
    {/if}
    <p class="font-jetbrains-mono text-sm font-black tabular-nums text-tb-slate-900">
      ${totalPriceMinor.toLocaleString()}
    </p>
    {#if disabled}
      <span class="text-[10px] font-semibold text-tb-rose-600">今日無供應</span>
    {/if}
  </button>
</form>
