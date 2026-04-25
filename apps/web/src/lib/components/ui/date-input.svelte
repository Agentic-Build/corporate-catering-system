<script lang="ts">
  /**
   * Date input. Binds directly to ISO "YYYY-MM-DD" — the only date format
   * humans + API should share. Callers that need epochDay should convert at
   * submit time via `isoDateToEpochDay`.
   */
  interface Props {
    value: string;
    min?: string;
    max?: string;
    disabled?: boolean;
    onchange?: (next: string) => void;
  }

  let { value = $bindable(""), min, max, disabled = false, onchange }: Props = $props();

  function handleChange(event: Event) {
    const input = event.currentTarget as HTMLInputElement;
    value = input.value;
    onchange?.(input.value);
  }
</script>

<input
  type="date"
  class="w-full rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm tabular-nums focus:border-cyan-600 focus:outline-none focus:ring-2 focus:ring-cyan-200 disabled:cursor-not-allowed disabled:bg-slate-100"
  {value}
  {min}
  {max}
  {disabled}
  onchange={handleChange}
/>
