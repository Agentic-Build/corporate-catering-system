<script lang="ts">
  // Order-progress bar — ported from MerchantView.jsx OrderProgress.
  // Shows 已訂/上限; high fill is good for the merchant.
  interface Props {
    ordered: number;
    cap: number;
  }
  let { ordered, cap }: Props = $props();

  const pct = $derived(cap > 0 ? Math.min(100, Math.round((ordered / cap) * 100)) : 0);
  const barTone = $derived(
    pct === 0
      ? "bg-tb-slate-200"
      : pct <= 25
        ? "bg-tb-amber-400"
        : pct < 90
          ? "bg-tb-emerald-500"
          : "bg-tb-red-600",
  );
</script>

<div
  class="flex items-center gap-2"
  role="progressbar"
  aria-valuenow={pct}
  aria-valuemin={0}
  aria-valuemax={100}
  aria-label="已訂購 {ordered} 份，上限 {cap} 份"
>
  <div class="h-1.5 flex-1 overflow-hidden rounded-full bg-tb-slate-100">
    <div class="h-full {barTone} transition-all" style="width: {pct}%"></div>
  </div>
  <span
    class="whitespace-nowrap font-jetbrains-mono text-xs font-bold tabular-nums text-tb-slate-700"
  >
    {ordered} / {cap}
  </span>
</div>
