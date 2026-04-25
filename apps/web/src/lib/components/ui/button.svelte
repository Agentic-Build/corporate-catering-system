<script lang="ts">
  import type { Snippet } from "svelte";

  interface Props {
    children: Snippet;
    variant?: "primary" | "secondary" | "danger" | "ghost";
    size?: "sm" | "md";
    type?: "button" | "submit" | "reset";
    disabled?: boolean;
    loading?: boolean;
    href?: string;
    onclick?: (event: MouseEvent) => void;
    title?: string;
    fullWidth?: boolean;
  }

  let {
    children,
    variant = "secondary",
    size = "md",
    type = "button",
    disabled = false,
    loading = false,
    href,
    onclick,
    title,
    fullWidth = false
  }: Props = $props();

  const toneClass = $derived.by(() => {
    switch (variant) {
      case "primary":
        return "border-transparent bg-cyan-700 text-white hover:bg-cyan-800 focus:ring-cyan-500";
      case "danger":
        return "border-transparent bg-rose-600 text-white hover:bg-rose-700 focus:ring-rose-500";
      case "ghost":
        return "border-transparent bg-transparent text-slate-700 hover:bg-slate-100 focus:ring-slate-300";
      default:
        return "border-slate-300 bg-white text-slate-800 hover:border-slate-500 hover:text-slate-950 focus:ring-slate-300";
    }
  });

  const sizeClass = $derived(size === "sm" ? "px-2.5 py-1.5 text-xs" : "px-3.5 py-2 text-sm");

  const className = $derived(
    `inline-flex ${fullWidth ? "w-full justify-center" : ""} items-center gap-2 rounded-lg border font-semibold transition focus:outline-none focus:ring-2 focus:ring-offset-1 disabled:cursor-not-allowed disabled:opacity-60 ${toneClass} ${sizeClass}`
  );

  const isDisabled = $derived(disabled || loading);
</script>

{#if href && !isDisabled}
  <a class={className} {href} {title}>
    {#if loading}<span class="inline-block h-3 w-3 animate-spin rounded-full border-2 border-current border-t-transparent"></span>{/if}
    {@render children()}
  </a>
{:else}
  <button class={className} {type} disabled={isDisabled} {onclick} {title}>
    {#if loading}<span class="inline-block h-3 w-3 animate-spin rounded-full border-2 border-current border-t-transparent"></span>{/if}
    {@render children()}
  </button>
{/if}
