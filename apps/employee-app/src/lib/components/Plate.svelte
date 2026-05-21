<script lang="ts">
  // Diagonal-stripe placeholder block standing in for vendor/meal photos.
  import type { PlateColor } from "$lib/sample";

  interface Props {
    color?: PlateColor;
    class?: string;
    label?: string;
  }
  let { color = "stone", class: cls = "", label }: Props = $props();

  const cssClass: Record<PlateColor, string> = {
    warm: "placeholder-warm",
    green: "placeholder-green",
    cool: "placeholder-cool",
    rose: "placeholder-rose",
    stone: "placeholder-stone",
  };
</script>

<div class="relative overflow-hidden {cssClass[color]} {cls}">
  {#if label}
    <div class="absolute inset-0 grid place-items-center">
      <span
        class="rounded bg-white/70 px-1.5 py-0.5 font-mono text-[9px] tracking-wide text-tb-slate-600"
      >
        {label}
      </span>
    </div>
  {/if}
</div>
