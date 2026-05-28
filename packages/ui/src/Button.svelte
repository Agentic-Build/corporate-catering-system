<script lang="ts">
  interface Props {
    variant?: "primary" | "secondary" | "ghost" | "danger";
    size?: "sm" | "md";
    type?: "button" | "submit";
    disabled?: boolean;
    fullWidth?: boolean;
    title?: string;
    ariaLabel?: string;
    onclick?: (e: MouseEvent) => void;
    children?: import("svelte").Snippet;
  }
  let {
    variant = "primary",
    size = "md",
    type = "button",
    disabled = false,
    fullWidth = false,
    title,
    ariaLabel,
    onclick,
    children,
  }: Props = $props();

  const base =
    "inline-flex items-center justify-center gap-2 rounded-lg border font-semibold transition focus:outline-none focus:ring-2 focus:ring-offset-1 disabled:cursor-not-allowed disabled:opacity-60";
  const sizes = {
    sm: "text-xs px-2.5 py-1.5",
    md: "text-sm px-3.5 py-2",
  };
  const variants = {
    primary:
      "border-transparent bg-tb-red-600 text-white hover:bg-tb-red-700 focus:ring-tb-red-500",
    secondary:
      "border-tb-slate-300 bg-white text-tb-slate-800 hover:border-tb-slate-500 hover:text-tb-slate-950 focus:ring-tb-slate-300",
    ghost:
      "border-transparent bg-transparent text-tb-slate-700 hover:bg-tb-slate-100 focus:ring-tb-slate-300",
    danger:
      "border-transparent bg-tb-rose-600 text-white hover:bg-tb-rose-700 focus:ring-tb-rose-500",
  };
</script>

<button
  {type}
  {disabled}
  {title}
  aria-label={ariaLabel}
  class="{base} {sizes[size]} {variants[variant]} {fullWidth ? 'w-full' : ''}"
  {onclick}
>
  {@render children?.()}
</button>
