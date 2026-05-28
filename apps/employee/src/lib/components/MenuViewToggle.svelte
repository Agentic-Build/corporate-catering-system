<script lang="ts">
  interface Props {
    view: "meal" | "vendor";
    onChange: (next: "meal" | "vendor") => void;
  }
  let { view, onChange }: Props = $props();

  const options: { id: "meal" | "vendor"; label: string }[] = [
    { id: "meal", label: "依餐點" },
    { id: "vendor", label: "依店家" },
  ];
</script>

<div class="inline-flex rounded-tb-lg border border-tb-slate-200 bg-tb-slate-50 p-0.5">
  {#each options as opt (opt.id)}
    <button
      type="button"
      onclick={() => onChange(opt.id)}
      aria-pressed={view === opt.id}
      class="rounded-tb-md px-3 py-1 text-xs font-semibold transition {view === opt.id
        ? 'bg-white text-tb-slate-900 shadow-tb-sm'
        : 'text-tb-slate-500 hover:text-tb-slate-800'}"
    >
      {opt.label}
    </button>
  {/each}
</div>
