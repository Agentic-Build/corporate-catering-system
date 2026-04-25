<script lang="ts">
  /**
   * Mobile-friendly quantity stepper (`− [1] +`). Uses real buttons so it works
   * on touch without a keyboard. Debounces nothing — parent decides.
   */
  interface Props {
    value: number;
    min?: number;
    max?: number;
    step?: number;
    disabled?: boolean;
    "aria-label"?: string;
    onchange?: (next: number) => void;
  }

  let {
    value = $bindable(1),
    min = 1,
    max = 99,
    step = 1,
    disabled = false,
    "aria-label": ariaLabel = "數量",
    onchange
  }: Props = $props();

  const canDec = $derived(!disabled && value > min);
  const canInc = $derived(!disabled && value < max);

  function clamp(next: number): number {
    return Math.max(min, Math.min(max, next));
  }

  function dec() {
    if (!canDec) return;
    const next = clamp(value - step);
    value = next;
    onchange?.(next);
  }

  function inc() {
    if (!canInc) return;
    const next = clamp(value + step);
    value = next;
    onchange?.(next);
  }

  function handleDirect(event: Event) {
    const input = event.currentTarget as HTMLInputElement;
    const parsed = Number.parseInt(input.value, 10);
    if (Number.isNaN(parsed)) return;
    const next = clamp(parsed);
    value = next;
    onchange?.(next);
  }
</script>

<div
  class="inline-flex items-center gap-0 rounded-full border border-slate-300 bg-white shadow-sm"
  role="group"
  aria-label={ariaLabel}
>
  <button
    type="button"
    class="flex h-9 w-9 items-center justify-center rounded-l-full text-lg font-semibold text-slate-700 transition hover:bg-slate-100 disabled:cursor-not-allowed disabled:text-slate-300"
    onclick={dec}
    disabled={!canDec}
    aria-label="減少 {ariaLabel}"
  >
    −
  </button>
  <input
    type="number"
    class="w-12 border-x border-slate-200 bg-transparent px-1 py-1.5 text-center text-sm font-semibold tabular-nums text-slate-900 focus:outline-none"
    {value}
    {min}
    {max}
    {step}
    {disabled}
    onchange={handleDirect}
    aria-label={ariaLabel}
  />
  <button
    type="button"
    class="flex h-9 w-9 items-center justify-center rounded-r-full text-lg font-semibold text-slate-700 transition hover:bg-slate-100 disabled:cursor-not-allowed disabled:text-slate-300"
    onclick={inc}
    disabled={!canInc}
    aria-label="增加 {ariaLabel}"
  >
    +
  </button>
</div>
