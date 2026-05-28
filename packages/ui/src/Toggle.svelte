<script lang="ts">
  interface Props {
    on: boolean;
    onChange: (next: boolean) => void;
    label?: string;
  }
  let { on, onChange, label }: Props = $props();

  function toggle() {
    onChange(!on);
  }

  function onKeydown(e: KeyboardEvent) {
    if (e.key === " " || e.key === "Enter") {
      e.preventDefault();
      onChange(!on);
    }
  }
</script>

<span class="inline-flex cursor-pointer select-none items-center gap-2">
  <span
    role="switch"
    aria-checked={on}
    aria-label={label}
    tabindex={0}
    onclick={toggle}
    onkeydown={onKeydown}
    class="relative inline-flex h-6 w-10 items-center rounded-full transition {on
      ? 'bg-tb-red-600'
      : 'bg-tb-slate-300'}"
  >
    <span
      class="absolute top-0.5 h-5 w-5 rounded-full bg-white shadow transition-transform {on
        ? 'translate-x-[18px]'
        : 'translate-x-0.5'}"
    ></span>
  </span>
  {#if label}<span class="text-xs text-tb-slate-600">{label}</span>{/if}
</span>
