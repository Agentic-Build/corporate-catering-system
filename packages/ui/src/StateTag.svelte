<script lang="ts">
  // Calibrated against reference_src/ui.jsx + components.jsx StateTag —
  // adds the `pending` tone (reference has 6 tones). The leading dot +
  // `pulse` are kept; existing callers depend on them.
  interface Props {
    tone?: "success" | "warning" | "danger" | "info" | "pending" | "neutral";
    pulse?: boolean;
    children?: import("svelte").Snippet;
  }
  let { tone = "neutral", pulse = false, children }: Props = $props();
  const tones = {
    success: "bg-tb-emerald-50 text-tb-emerald-700 border-tb-emerald-200",
    warning: "bg-tb-amber-50 text-tb-amber-700 border-tb-amber-300",
    danger:  "bg-tb-rose-50 text-tb-rose-700 border-tb-rose-300",
    info:    "bg-tb-red-50 text-tb-red-700 border-tb-red-200",
    pending: "bg-tb-slate-100 text-tb-slate-800 border-tb-slate-300",
    neutral: "bg-tb-slate-100 text-tb-slate-700 border-tb-slate-200",
  };
  const dots = {
    success: "bg-tb-emerald-500",
    warning: "bg-tb-amber-400",
    danger:  "bg-tb-rose-600",
    info:    "bg-tb-red-600",
    pending: "bg-tb-slate-500",
    neutral: "bg-tb-slate-400",
  };
</script>

<span class="inline-flex items-center gap-1 rounded-full border px-2 py-0.5 text-xs font-semibold {tones[tone]}">
  <span class="h-1.5 w-1.5 rounded-full {dots[tone]} {pulse ? 'animate-pulse' : ''}" aria-hidden="true"></span>
  {@render children?.()}
</span>
