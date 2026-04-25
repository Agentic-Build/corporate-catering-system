<script lang="ts">
  /**
   * Chip input — replaces comma-separated string inputs with a visual chip list.
   * Items are strings. Pressing Enter (or comma) appends; clicking × removes.
   *
   * Parent binds `values: string[]`. Great for reminder days, evidence refs,
   * issue checklist IDs, health tags.
   */
  interface Props {
    values: string[];
    placeholder?: string;
    disabled?: boolean;
    suggestions?: string[];
    validate?: (raw: string) => string | null; // return error message, or null
    onchange?: (next: string[]) => void;
  }

  let {
    values = $bindable([]),
    placeholder = "輸入後按 Enter 加入",
    disabled = false,
    suggestions,
    validate,
    onchange
  }: Props = $props();

  let draft = $state("");
  let error = $state<string | null>(null);

  function commit() {
    const raw = draft.trim();
    if (!raw) return;
    const errMessage = validate?.(raw) ?? null;
    if (errMessage) {
      error = errMessage;
      return;
    }
    if (values.includes(raw)) {
      error = "已在清單中";
      return;
    }
    const next = [...values, raw];
    values = next;
    onchange?.(next);
    draft = "";
    error = null;
  }

  function remove(index: number) {
    const next = values.filter((_, i) => i !== index);
    values = next;
    onchange?.(next);
  }

  function handleKey(event: KeyboardEvent) {
    if (event.key === "Enter" || event.key === ",") {
      event.preventDefault();
      commit();
    } else if (event.key === "Backspace" && !draft && values.length > 0) {
      remove(values.length - 1);
    }
  }

  function pickSuggestion(value: string) {
    if (values.includes(value)) return;
    const next = [...values, value];
    values = next;
    onchange?.(next);
  }

  const remainingSuggestions = $derived(
    suggestions?.filter((s) => !values.includes(s)) ?? []
  );
</script>

<div class="grid gap-1">
  <div
    class="flex flex-wrap items-center gap-1 rounded-lg border border-slate-300 bg-white p-1.5 focus-within:border-cyan-600 focus-within:ring-2 focus-within:ring-cyan-200"
  >
    {#each values as value, index (value)}
      <span class="inline-flex items-center gap-1 rounded-full bg-cyan-100 px-2 py-0.5 text-xs font-semibold text-cyan-900">
        {value}
        {#if !disabled}
          <button
            type="button"
            class="ml-0.5 rounded-full text-cyan-700 hover:text-rose-700"
            aria-label="移除 {value}"
            onclick={() => remove(index)}
          >
            ×
          </button>
        {/if}
      </span>
    {/each}
    <input
      type="text"
      class="flex-1 border-0 bg-transparent px-2 py-1 text-sm focus:outline-none focus:ring-0"
      bind:value={draft}
      onkeydown={handleKey}
      onblur={commit}
      placeholder={values.length === 0 ? placeholder : ""}
      {disabled}
    />
  </div>
  {#if error}
    <span class="text-xs text-rose-700">{error}</span>
  {/if}
  {#if remainingSuggestions.length > 0}
    <div class="flex flex-wrap gap-1 pt-0.5">
      <span class="text-[11px] text-slate-500">建議：</span>
      {#each remainingSuggestions as suggestion}
        <button
          type="button"
          class="inline-flex items-center rounded-full border border-slate-200 bg-slate-50 px-2 py-0.5 text-[11px] font-medium text-slate-600 transition hover:border-cyan-500 hover:text-cyan-800"
          onclick={() => pickSuggestion(suggestion)}
        >
          + {suggestion}
        </button>
      {/each}
    </div>
  {/if}
</div>
