<script lang="ts">
  // Ported from reference_src/ui.jsx RemainBar — remaining-stock progress
  // bar. Low stock → amber, critical → rose.
  interface Props {
    remain: number;
    cap: number;
  }
  let { remain, cap }: Props = $props();

  const pct = $derived(cap > 0 ? Math.max(0, Math.min(100, Math.round((remain / cap) * 100))) : 0);
  const barTone = $derived(
    remain === 0
      ? "bg-tb-slate-300"
      : pct <= 15
        ? "bg-tb-rose-500"
        : pct <= 35
          ? "bg-tb-amber-500"
          : "bg-tb-emerald-500",
  );
  const textTone = $derived(
    remain === 0
      ? "text-tb-slate-500"
      : pct <= 15
        ? "text-tb-rose-700"
        : pct <= 35
          ? "text-tb-amber-700"
          : "text-tb-emerald-700",
  );
</script>

<div class="flex items-center gap-2">
  <div class="h-1.5 flex-1 overflow-hidden rounded-full bg-tb-slate-100">
    <div class="h-full {barTone} transition-all" style="width: {pct}%"></div>
  </div>
  <span class="text-xs font-semibold tabular-nums {textTone}">
    {remain === 0 ? "已售罄" : `剩餘 ${remain} 份`}
  </span>
</div>
