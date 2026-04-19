<script lang="ts">
  /**
   * Money input. Binds to a `minor` integer (cents / 分). UI shows the major
   * unit (e.g. "120" for 120 TWD), and the parent receives the minor integer.
   *
   * Parent must pass { value, currency }. Currency is visual-only; conversion
   * assumes 100 minor = 1 major (TWD/USD/EUR default).
   */
  import { majorToMinor, minorToMajor } from "$lib/platform/time-formats";

  interface Props {
    value: number; // minor
    currency?: string;
    min?: number; // minor
    max?: number; // minor
    disabled?: boolean;
    onchange?: (next: number) => void;
  }

  let {
    value = $bindable(0),
    currency = "TWD",
    min,
    max,
    disabled = false,
    onchange
  }: Props = $props();

  const majorDisplay = $derived(value === 0 ? "" : String(minorToMajor(value)));
  const minMajor = $derived(min !== undefined ? minorToMajor(min) : undefined);
  const maxMajor = $derived(max !== undefined ? minorToMajor(max) : undefined);

  function handleInput(event: Event) {
    const input = event.currentTarget as HTMLInputElement;
    const parsed = Number.parseFloat(input.value);
    if (Number.isNaN(parsed)) {
      value = 0;
      onchange?.(0);
      return;
    }
    const minor = majorToMinor(parsed);
    value = minor;
    onchange?.(minor);
  }
</script>

<div class="flex items-center gap-2">
  <span class="inline-flex items-center rounded-l-lg border border-r-0 border-slate-300 bg-slate-50 px-2 py-2 text-xs font-semibold text-slate-600">
    {currency}
  </span>
  <input
    type="number"
    inputmode="decimal"
    step="0.01"
    class="w-full rounded-r-lg border border-slate-300 bg-white px-3 py-2 text-sm tabular-nums focus:border-cyan-600 focus:outline-none focus:ring-2 focus:ring-cyan-200 disabled:cursor-not-allowed disabled:bg-slate-100"
    value={majorDisplay}
    min={minMajor}
    max={maxMajor}
    placeholder="120"
    {disabled}
    oninput={handleInput}
  />
</div>
