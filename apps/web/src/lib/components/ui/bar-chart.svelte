<script lang="ts">
  /**
   * Zero-dependency horizontal bar chart for "Top N" style views.
   *
   * Given `items: { label, value }[]`, draws a bar per row with proportional
   * width and inline value. Auto-formats values using the supplied formatter.
   */

  interface Item {
    label: string;
    value: number;
  }

  interface Props {
    items: Item[];
    format?: (value: number) => string;
    /** Maximum bars to show. Excess items collapse into "…及 N 筆更多". */
    limit?: number;
    tone?: "cyan" | "emerald" | "amber" | "rose";
  }

  let {
    items,
    format = (value: number) => value.toLocaleString(),
    limit = 8,
    tone = "cyan"
  }: Props = $props();

  const maxValue = $derived(items.length === 0 ? 1 : Math.max(...items.map((i) => i.value || 0), 1));
  const visible = $derived(items.slice(0, limit));
  const overflow = $derived(Math.max(0, items.length - limit));

  const toneClass = $derived.by(() => {
    switch (tone) {
      case "emerald":
        return "bg-emerald-500";
      case "amber":
        return "bg-amber-500";
      case "rose":
        return "bg-rose-500";
      default:
        return "bg-cyan-500";
    }
  });
</script>

{#if items.length === 0}
  <p class="text-xs text-slate-500">尚無資料</p>
{:else}
  <ul class="grid gap-1.5">
    {#each visible as item (item.label)}
      {@const pct = Math.max(2, Math.round(((item.value || 0) / maxValue) * 100))}
      <li class="grid gap-1">
        <div class="flex items-baseline justify-between gap-2 text-xs">
          <span class="truncate font-medium text-slate-700">{item.label}</span>
          <span class="shrink-0 tabular-nums font-semibold text-slate-900">{format(item.value)}</span>
        </div>
        <div class="h-1.5 overflow-hidden rounded-full bg-slate-100">
          <div class={`h-full rounded-full ${toneClass}`} style={`width: ${pct}%`}></div>
        </div>
      </li>
    {/each}
  </ul>
  {#if overflow > 0}
    <p class="mt-2 text-xs text-slate-500">…及 {overflow} 筆更多</p>
  {/if}
{/if}
