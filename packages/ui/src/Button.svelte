<script lang="ts">
  interface Props {
    variant?: "primary" | "secondary" | "ghost" | "danger";
    size?: "sm" | "md";
    type?: "button" | "submit";
    disabled?: boolean;
    onclick?: (e: MouseEvent) => void;
    children?: import("svelte").Snippet;
  }
  let {
    variant = "primary",
    size = "md",
    type = "button",
    disabled = false,
    onclick,
    children,
  }: Props = $props();

  const base =
    "inline-flex items-center justify-center rounded-lg border font-semibold transition focus:outline-none focus:ring-2 focus:ring-offset-1";
  const sizes = {
    sm: "text-xs px-2.5 py-1.5",
    md: "text-sm px-3.5 py-2",
  };
  const variants = {
    primary:
      "bg-tb-red-600 border-tb-red-600 text-white hover:bg-tb-red-700 focus:ring-tb-red-500",
    secondary:
      "bg-white border-tb-slate-300 text-tb-slate-800 hover:border-tb-slate-500 hover:text-tb-slate-950 focus:ring-tb-slate-300",
    ghost: "border-transparent text-tb-slate-800 hover:bg-tb-slate-100",
    danger:
      "bg-tb-rose-600 border-tb-rose-600 text-white hover:bg-tb-rose-700 focus:ring-tb-rose-500",
  };
</script>

<button
  {type}
  {disabled}
  class="{base} {sizes[size]} {variants[variant]} disabled:cursor-not-allowed disabled:opacity-60"
  {onclick}
>
  {@render children?.()}
</button>
