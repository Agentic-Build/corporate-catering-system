<script lang="ts">
  // Calibrated against reference_src/ui.jsx + components.jsx TBiteLogo —
  // gradient tile mark with an amber accent dot + wordmark.
  // `size` (px of the tile) is kept for the existing callers; `small`
  // hides the wordmark, `light` is for dark backgrounds.
  interface Props {
    size?: number;
    small?: boolean;
    light?: boolean;
  }
  let { size = 36, small = false, light = false }: Props = $props();
</script>

<a href="/" class="inline-flex items-center gap-2.5 font-noto-tc" aria-label="T-Bite home">
  <span
    class="relative grid place-items-center rounded-tb-2xl bg-gradient-to-br from-tb-red-500 to-tb-rose-700 text-white shadow-tb-sm"
    style="height: {size}px; width: {size}px;"
  >
    <span class="text-lg font-black tracking-tight">T</span>
    <span
      class="absolute -bottom-0.5 -right-0.5 h-2.5 w-2.5 rounded-full bg-tb-amber-400 ring-2 ring-white"
    ></span>
  </span>
  {#if !small}
    <span class="leading-none">
      <span
        class="block text-[18px] font-black tracking-tight {light
          ? 'text-white'
          : 'text-tb-slate-900'}"
      >
        T-Bite<span class="text-tb-red-600">.</span>
      </span>
      <span
        class="mt-0.5 block text-[9px] font-bold uppercase tracking-eyebrow-wide {light
          ? 'text-tb-slate-300'
          : 'text-tb-slate-500'}"
      >
        Corporate Catering
      </span>
    </span>
  {/if}
</a>
