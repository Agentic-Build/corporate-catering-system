<script lang="ts">
  import { StateTag } from "@tbite/ui";
  let { data } = $props();

  const filters = [
    { id: "", label: "全部" },
    { id: "draft", label: "草稿" },
    { id: "locked", label: "已鎖定" },
    { id: "exported", label: "已匯出" },
    { id: "closed", label: "已關閉" },
  ];

  const statusTone = {
    draft: "neutral",
    locked: "warning",
    exported: "success",
    closed: "neutral",
  } as Record<string, "info" | "neutral" | "warning" | "danger" | "success">;
  const statusLabel = {
    draft: "草稿",
    locked: "已鎖定",
    exported: "已匯出",
    closed: "已關閉",
  } as Record<string, string>;
</script>

<section class="space-y-4">
  <header class="flex items-center justify-between">
    <h1 class="text-2xl font-black text-tb-slate-900">月結批次</h1>
    <a href="/payroll/new" class="rounded-lg bg-tb-red-600 px-3.5 py-2 text-sm font-semibold text-white hover:bg-tb-red-700">+ 建立月份</a>
  </header>

  <div class="flex flex-wrap gap-1 rounded-full bg-tb-slate-100 p-1">
    {#each filters as f}
      <a href={f.id ? `?status=${f.id}` : "?"}
         class="rounded-full px-3 py-1 text-xs font-semibold {data.status === f.id ? 'bg-tb-slate-900 text-white' : 'text-tb-slate-700'}">
        {f.label}
      </a>
    {/each}
  </div>

  {#if data.batches.length === 0}
    <p class="rounded-tb-2xl border border-tb-slate-200 bg-white p-6 text-center text-sm text-tb-slate-500">
      尚無批次
    </p>
  {:else}
    <div class="overflow-hidden rounded-tb-2xl border border-tb-slate-200 bg-white shadow-tb-sm">
      <table class="w-full text-sm">
        <thead class="bg-tb-slate-50 text-left text-xs uppercase tracking-eyebrow text-tb-slate-500">
          <tr>
            <th class="px-4 py-2">期間</th>
            <th class="px-4 py-2">狀態</th>
            <th class="px-4 py-2">鎖定時間</th>
            <th class="px-4 py-2">匯出時間</th>
            <th class="px-4 py-2">CSV</th>
            <th class="px-4 py-2"></th>
          </tr>
        </thead>
        <tbody>
          {#each data.batches as b (b.id)}
            <tr class="border-t border-tb-slate-100">
              <td class="px-4 py-3 font-semibold text-tb-slate-900 font-jetbrains-mono">
                {b.period_start} — {b.period_end}
              </td>
              <td class="px-4 py-3">
                <StateTag tone={statusTone[b.status] ?? "neutral"}>{statusLabel[b.status] ?? b.status}</StateTag>
              </td>
              <td class="px-4 py-3 text-xs text-tb-slate-500 font-jetbrains-mono">{b.locked_at ?? "-"}</td>
              <td class="px-4 py-3 text-xs text-tb-slate-500 font-jetbrains-mono">{b.exported_at ?? "-"}</td>
              <td class="px-4 py-3 text-xs">
                {#if b.export_uri}
                  <span class="font-jetbrains-mono break-all text-tb-slate-500">{b.export_uri}</span>
                {:else}
                  -
                {/if}
              </td>
              <td class="px-4 py-3 text-right">
                <a href="/payroll/{b.id}" class="text-tb-red-600 hover:text-tb-red-700">詳細</a>
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  {/if}
</section>
