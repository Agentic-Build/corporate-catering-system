<script lang="ts">
  interface Props { size?: number; eyebrow?: boolean }
  let { size = 28, eyebrow = true }: Props = $props();
</script>

<a href="/" class="inline-flex items-center gap-2 font-noto-tc">
  <span
    class="relative inline-flex items-center justify-center rounded-full bg-gradient-to-br from-tb-red-500 to-tb-rose-700 text-white shadow-tb-sm"
    style:width="{size}px"
    style:height="{size}px"
  >
    <span class="text-[14px] font-black leading-none">T</span>
    <span
      class="absolute -right-0.5 -top-0.5 h-2 w-2 rounded-full bg-tb-amber-400"
      aria-hidden
    ></span>
  </span>
  <span class="flex flex-col leading-tight">
    <span class="text-[15px] font-black text-tb-slate-900">T-Bite.</span>
    {#if eyebrow}
      <span
        class="text-[9px] uppercase tracking-eyebrow-wide text-tb-slate-500"
        >Corporate Catering</span
      >
    {/if}
  </span>
</a>
