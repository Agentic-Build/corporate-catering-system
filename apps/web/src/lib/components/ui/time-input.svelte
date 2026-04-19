<script lang="ts">
  /**
   * Time-of-day input that binds to a minute-of-day integer (0..1439).
   * UI presents HH:mm; parent never sees raw minutes unless explicitly read.
   */
  import { minuteOfDayToTime, timeToMinuteOfDay } from "$lib/platform/time-formats";

  interface Props {
    value: number; // minute-of-day
    min?: number;
    max?: number;
    step?: number; // seconds
    disabled?: boolean;
    onchange?: (next: number) => void;
  }

  let {
    value = $bindable(0),
    min,
    max,
    step = 900,
    disabled = false,
    onchange
  }: Props = $props();

  const timeValue = $derived(minuteOfDayToTime(value));
  const minTime = $derived(min !== undefined ? minuteOfDayToTime(min) : undefined);
  const maxTime = $derived(max !== undefined ? minuteOfDayToTime(max) : undefined);

  function handleChange(event: Event) {
    const input = event.currentTarget as HTMLInputElement;
    const nextMinute = timeToMinuteOfDay(input.value);
    value = nextMinute;
    onchange?.(nextMinute);
  }
</script>

<input
  type="time"
  class="w-full rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm font-mono tabular-nums focus:border-cyan-600 focus:outline-none focus:ring-2 focus:ring-cyan-200 disabled:cursor-not-allowed disabled:bg-slate-100"
  value={timeValue}
  min={minTime}
  max={maxTime}
  {step}
  {disabled}
  onchange={handleChange}
/>
