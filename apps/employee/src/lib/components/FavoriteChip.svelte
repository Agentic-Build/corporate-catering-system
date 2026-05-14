<script lang="ts">
  interface Props {
    menuItemId: string;
    name: string;
    unitPrice: number;
    availableToday: boolean;
    action?: string;
  }
  let {
    menuItemId,
    name,
    unitPrice,
    availableToday,
    action = "/?/addFavoriteChipToCart",
  }: Props = $props();
  const disabled = $derived(!availableToday);
</script>

<form
  method="POST"
  {action}
  role="listitem"
  class="shrink-0 snap-start {disabled ? 'pointer-events-none opacity-50' : ''}"
>
  <input type="hidden" name="menu_item_id" value={menuItemId} />
  <button
    type="submit"
    {disabled}
    class="flex w-36 flex-col items-start gap-1 rounded-tb-2xl border border-tb-slate-200 bg-white p-3 text-left shadow-tb-sm transition hover:-translate-y-0.5 hover:shadow-tb-md focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-tb-amber-400 disabled:cursor-not-allowed"
    aria-label={disabled ? `${name} 今日無供應` : `加入 ${name}`}
  >
    <span class="line-clamp-2 text-xs font-bold text-tb-slate-900">{name}</span>
    <p class="font-jetbrains-mono text-sm font-black tabular-nums text-tb-slate-900">
      ${unitPrice.toLocaleString()}
    </p>
    {#if disabled}
      <span class="text-[10px] font-semibold text-tb-rose-600">今日無供應</span>
    {/if}
  </button>
</form>
