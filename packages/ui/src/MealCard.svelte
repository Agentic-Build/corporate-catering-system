<script lang="ts">
  interface Props {
    name: string;
    vendor: string;
    priceMinor: number;
    remain: number;
    capacity: number;
    pickupWindow?: string;
    badges?: string[];
    image?: string;
    qty?: number;
    soldOut?: boolean;
    lowStockThreshold?: number;
    onIncrement?: () => void;
    onDecrement?: () => void;
    onClick?: () => void;
    isFavorite?: boolean;
    onToggleFavorite?: () => void;
  }
  let {
    name,
    vendor,
    priceMinor,
    remain,
    capacity,
    pickupWindow = "",
    badges = [],
    image,
    qty = 0,
    soldOut = false,
    lowStockThreshold = 10,
    onIncrement,
    onDecrement,
    onClick,
    isFavorite = false,
    onToggleFavorite,
  }: Props = $props();

  const isLowStock = $derived(!soldOut && remain > 0 && remain <= lowStockThreshold);
  const isSoldOut = $derived(soldOut || remain === 0);
  const priceFormatted = $derived("$" + priceMinor.toLocaleString());
</script>

<article
  class="group relative overflow-hidden rounded-tb-2xl border border-tb-slate-200 bg-white shadow-tb-sm transition hover:-translate-y-0.5 hover:shadow-tb-md
    {isSoldOut ? 'opacity-60' : ''}"
>
  <button type="button" class="block w-full text-left" onclick={onClick} disabled={isSoldOut}>
    <div class="aspect-[16/10] w-full overflow-hidden bg-tb-slate-100">
      {#if image}
        <img
          src={image}
          alt={name}
          loading="lazy"
          class="h-full w-full object-cover transition group-hover:scale-[1.03]"
        />
      {:else}
        <div
          class="flex h-full w-full items-center justify-center text-tb-slate-400 text-xs uppercase tracking-eyebrow"
        >
          No image
        </div>
      {/if}
    </div>
    <div class="p-3">
      <p class="text-[10px] uppercase tracking-eyebrow text-tb-slate-500">{vendor}</p>
      <h3 class="mt-1 text-sm font-bold leading-snug text-tb-slate-900">{name}</h3>
      <p class="mt-1 font-jetbrains-mono text-base font-black tabular-nums text-tb-slate-900">
        {priceFormatted}
      </p>
      {#if pickupWindow}
        <p class="mt-1 text-[11px] text-tb-slate-500">領餐：{pickupWindow}</p>
      {/if}
      {#if badges.length > 0}
        <div class="mt-2 flex flex-wrap gap-1">
          {#each badges as b (b)}
            <span
              class="rounded-full bg-tb-slate-100 px-2 py-0.5 text-[10px] font-semibold text-tb-slate-700"
              >{b}</span
            >
          {/each}
        </div>
      {/if}
    </div>
  </button>

  {#if isSoldOut}
    <div class="pointer-events-none absolute inset-0 grid place-items-center bg-tb-slate-900/50">
      <span class="rounded-full bg-white px-3 py-1 text-xs font-bold text-tb-slate-800"
        >本日已售罄</span
      >
    </div>
  {:else if isLowStock}
    <span
      class="absolute top-2 inline-flex items-center gap-1 rounded-full bg-tb-rose-600 px-2 py-0.5 text-[10px] font-semibold text-white
        {onToggleFavorite ? 'left-2' : 'right-2'}"
    >
      <span class="h-1.5 w-1.5 rounded-full bg-tb-amber-300 animate-pulse" aria-hidden="true"
      ></span>
      僅剩 {remain} 份
    </span>
  {/if}

  {#if onToggleFavorite}
    <button
      type="button"
      class="absolute right-2 top-2 inline-flex h-11 w-11 items-center justify-center rounded-full bg-white/90 text-base shadow-tb-sm transition hover:bg-white focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-tb-amber-400 active:scale-95
        {isFavorite ? 'text-tb-amber-400' : 'text-tb-slate-400 hover:text-tb-amber-400'}"
      onclick={onToggleFavorite}
      aria-label={isFavorite ? "取消最愛" : "加入最愛"}
      aria-pressed={isFavorite}
    >
      <span aria-hidden="true">{isFavorite ? "★" : "☆"}</span>
    </button>
  {/if}

  {#if !isSoldOut}
    <div class="absolute bottom-3 right-3 flex items-center gap-1.5">
      <button
        type="button"
        class="h-11 w-11 rounded-full border border-tb-slate-300 bg-white text-base font-black text-tb-slate-700 transition active:scale-95 disabled:opacity-40"
        onclick={onDecrement}
        disabled={qty === 0}
        aria-label="減少">−</button
      >
      <span class="min-w-[1ch] text-center font-jetbrains-mono text-sm font-bold tabular-nums"
        >{qty}</span
      >
      <button
        type="button"
        class="h-11 w-11 rounded-full border border-tb-red-600 bg-tb-red-600 text-base font-black text-white transition active:scale-95 disabled:bg-tb-slate-100 disabled:text-tb-slate-400 disabled:border-tb-slate-200"
        onclick={onIncrement}
        disabled={qty >= remain}
        aria-label="增加">+</button
      >
    </div>
  {/if}
</article>
