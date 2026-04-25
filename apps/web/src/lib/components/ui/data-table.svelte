<script lang="ts" generics="Row">
  import type { Snippet } from "svelte";

  interface Column<R> {
    id: string;
    label: string;
    width?: string;
  }

  interface Props {
    columns: Column<Row>[];
    rows: Row[];
    row: Snippet<[Row]>;
    emptyLabel?: string;
  }

  let { columns, rows, row, emptyLabel = "尚無資料" }: Props = $props();
</script>

<div class="overflow-x-auto rounded-xl border border-slate-200">
  <table class="min-w-full divide-y divide-slate-200 text-sm">
    <thead class="bg-slate-50">
      <tr>
        {#each columns as col}
          <th
            class="px-3 py-2 text-left font-semibold text-slate-700"
            style={col.width ? `width:${col.width}` : undefined}
          >
            {col.label}
          </th>
        {/each}
      </tr>
    </thead>
    <tbody class="divide-y divide-slate-100 bg-white">
      {#if rows.length === 0}
        <tr>
          <td class="px-3 py-4 text-center text-slate-500" colspan={columns.length}>{emptyLabel}</td>
        </tr>
      {:else}
        {#each rows as r}
          {@render row(r)}
        {/each}
      {/if}
    </tbody>
  </table>
</div>
