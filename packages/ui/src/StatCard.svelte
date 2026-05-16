<script lang="ts">
  // Calibrated against reference_src/ui.jsx + components.jsx StatCard.
  // `eyebrow`/`suffix`/`children` are kept (existing callers pass them);
  // `label`/`delta`/`deltaTone`/`hint` mirror the reference.
  import StateTag from "./StateTag.svelte";

  interface Props {
    /** Uppercase eyebrow label (existing callers). */
    eyebrow?: string;
    /** Plain label — reference style; used when `eyebrow` is absent. */
    label?: string;
    value: string | number;
    suffix?: string;
    delta?: string;
    deltaTone?: "success" | "warning" | "danger" | "info" | "pending" | "neutral";
    hint?: string;
    children?: import("svelte").Snippet;
  }
  let {
    eyebrow,
    label,
    value,
    suffix = "",
    delta,
    deltaTone = "info",
    hint,
    children,
  }: Props = $props();
</script>

<div class="rounded-tb-2xl border border-tb-slate-200 bg-white p-4 shadow-tb-sm md:p-5">
  {#if eyebrow}
    <p class="text-[10px] uppercase tracking-eyebrow-wide text-tb-slate-500">{eyebrow}</p>
  {:else if label}
    <p class="text-sm text-tb-slate-500">{label}</p>
  {/if}
  <p class="mt-1.5 flex items-end gap-2">
    <span class="font-jetbrains-mono text-3xl font-black tabular-nums text-tb-slate-900">{value}</span>
    {#if suffix}<span class="text-sm font-semibold text-tb-slate-500">{suffix}</span>{/if}
    {#if delta}<StateTag tone={deltaTone}>{delta}</StateTag>{/if}
  </p>
  {#if hint}
    <p class="mt-2 text-xs text-tb-slate-500">{hint}</p>
  {/if}
  {#if children}
    <div class="mt-3 text-xs text-tb-slate-500">{@render children()}</div>
  {/if}
</div>
