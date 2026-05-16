<script lang="ts">
  // Ported from the search box in EmployeeView.jsx — pill input with a
  // left search icon and a red focus ring.
  import Icon from "./Icon.svelte";

  interface Props {
    value: string;
    onInput: (value: string) => void;
    placeholder?: string;
  }
  let { value, onInput, placeholder = "搜尋…" }: Props = $props();
</script>

<div class="relative">
  <Icon
    name="search"
    class="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-tb-slate-400"
  />
  <input
    type="text"
    {value}
    {placeholder}
    oninput={(e) => onInput(e.currentTarget.value)}
    class="w-full rounded-full border border-tb-slate-200 bg-tb-slate-100 py-2.5 pl-10 pr-3 text-sm placeholder-tb-slate-500 transition focus:border-tb-red-500 focus:bg-white focus:outline-none focus:ring-4 focus:ring-tb-red-50"
  />
</div>
