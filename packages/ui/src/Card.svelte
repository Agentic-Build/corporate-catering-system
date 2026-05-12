<script lang="ts">
  interface Props { tone?: "default" | "info" | "warning" | "success" | "danger"; children?: import("svelte").Snippet }
  let { tone = "default", children }: Props = $props();
  const tones = {
    default: "border-tb-slate-200 bg-white",
    info:    "border-tb-red-200 bg-tb-red-50/60",
    warning: "border-tb-amber-300 bg-tb-amber-50/60",
    success: "border-tb-emerald-500/40 bg-emerald-50/60",
    danger:  "border-tb-rose-600/30 bg-tb-rose-50/60",
  };
</script>

<div class="rounded-tb-2xl border {tones[tone]} p-4 shadow-tb-sm md:p-5">
  {@render children?.()}
</div>
