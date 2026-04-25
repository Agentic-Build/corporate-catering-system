<script lang="ts">
  import type { Snippet } from "svelte";

  interface Props {
    title?: string;
    description?: string;
    variant?: "default" | "info" | "warning" | "success" | "danger";
    children: Snippet;
    actions?: Snippet;
  }

  let { title, description, variant = "default", children, actions }: Props = $props();

  const tone = $derived.by(() => {
    switch (variant) {
      case "info":
        return "border-cyan-200 bg-cyan-50/60";
      case "warning":
        return "border-amber-200 bg-amber-50/60";
      case "success":
        return "border-emerald-200 bg-emerald-50/60";
      case "danger":
        return "border-rose-200 bg-rose-50/60";
      default:
        return "border-slate-200 bg-white";
    }
  });
</script>

<article class={`rounded-2xl border ${tone} p-4 shadow-sm md:p-5`}>
  {#if title || actions}
    <header class="mb-3 flex flex-wrap items-start justify-between gap-3">
      {#if title}
        <div class="grid gap-1">
          <h3 class="text-base font-semibold text-slate-900">{title}</h3>
          {#if description}
            <p class="text-sm text-slate-600">{description}</p>
          {/if}
        </div>
      {/if}
      {#if actions}
        <div class="flex flex-wrap gap-2">
          {@render actions()}
        </div>
      {/if}
    </header>
  {/if}
  <div class="grid gap-3">
    {@render children()}
  </div>
</article>
