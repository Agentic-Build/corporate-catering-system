<script lang="ts">
  interface Props {
    eyebrow: string;
    value: string | number;
    suffix?: string;
    delta?: string;
    deltaTone?: "up" | "down" | "neutral";
    children?: import("svelte").Snippet;
  }
  let {
    eyebrow,
    value,
    suffix = "",
    delta,
    deltaTone = "neutral",
    children,
  }: Props = $props();
  const deltaToneClass = {
    up: "text-emerald-600",
    down: "text-tb-rose-600",
    neutral: "text-tb-slate-500",
  };
</script>

<div class="rounded-tb-2xl border border-tb-slate-200 bg-white p-4 shadow-tb-sm md:p-5">
  <p class="text-[10px] uppercase tracking-eyebrow-wide text-tb-slate-500">{eyebrow}</p>
  <p class="mt-2 flex items-baseline gap-1">
    <span class="font-jetbrains-mono text-3xl font-black tabular-nums text-tb-slate-900">{value}</span>
    {#if suffix}<span class="text-sm font-semibold text-tb-slate-500">{suffix}</span>{/if}
  </p>
  {#if delta}
    <p class="mt-1 text-xs font-semibold {deltaToneClass[deltaTone]}">{delta}</p>
  {/if}
  {#if children}
    <div class="mt-3 text-xs text-tb-slate-500">{@render children()}</div>
  {/if}
</div>
