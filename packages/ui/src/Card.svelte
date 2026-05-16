<script lang="ts">
  // Calibrated against reference_src/ui.jsx + components.jsx Card.
  // `tone` is kept (existing callers pass it); reference calls it `variant`.
  interface Props {
    tone?: "default" | "info" | "warning" | "success" | "danger";
    title?: string;
    description?: string;
    actions?: import("svelte").Snippet;
    children?: import("svelte").Snippet;
  }
  let { tone = "default", title, description, actions, children }: Props = $props();
  const tones = {
    default: "border-tb-slate-200 bg-white",
    info:    "border-tb-red-200 bg-tb-red-50/60",
    warning: "border-tb-amber-300 bg-tb-amber-50/60",
    success: "border-tb-emerald-500/40 bg-emerald-50/60",
    danger:  "border-tb-rose-600/30 bg-tb-rose-50/60",
  };
</script>

<article class="rounded-tb-2xl border {tones[tone]} p-4 shadow-tb-sm md:p-5">
  {#if title || actions}
    <header class="mb-3 flex flex-wrap items-start justify-between gap-3">
      {#if title}
        <div class="grid gap-1">
          <h3 class="text-base font-semibold text-tb-slate-900">{title}</h3>
          {#if description}<p class="text-sm text-tb-slate-600">{description}</p>{/if}
        </div>
      {/if}
      {#if actions}<div class="flex flex-wrap gap-2">{@render actions()}</div>{/if}
    </header>
  {/if}
  {@render children?.()}
</article>
