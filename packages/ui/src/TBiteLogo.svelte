<script lang="ts">
  import logoSvg from './assets/logo.svg';
  import logoPng from './assets/logo.png';

  interface Props {
    size?: number;
    small?: boolean;
    light?: boolean;
  }
  let { size = 36, small = false, light = false }: Props = $props();
</script>

<a href="/" class="inline-flex items-center gap-2.5 font-noto-tc" aria-label="T-Bite home">
  <picture>
    <source srcset={logoSvg} type="image/svg+xml" />
    <img src={logoPng} alt="T-Bite" style="height: {size}px; width: auto;" />
  </picture>
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
