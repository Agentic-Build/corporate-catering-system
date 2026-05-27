<script lang="ts">
  import { PageHeader, Card, StateTag, Button, Icon } from "@tbite/ui";
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

<PageHeader
  eyebrow="月結"
  title="月結批次"
  subtitle="聚合 picked_up／no_show 訂單為對帳批次 · 鎖定後排程匯出 HR CSV"
>
  {#snippet actions()}
    <a href="/payroll/new">
      <Button variant="primary" size="md">
        <Icon name="plus" class="h-3.5 w-3.5" />建立月份
      </Button>
    </a>
  {/snippet}
</PageHeader>

<div
  class="no-scrollbar flex items-center gap-1 overflow-x-auto rounded-full bg-tb-slate-100 p-1 md:flex-wrap"
>
  {#each filters as f}
    <a
      href={f.id ? `?status=${f.id}` : "?"}
      class="flex-shrink-0 whitespace-nowrap rounded-full px-3.5 py-1.5 text-xs font-semibold transition {data.status ===
      f.id
        ? 'bg-tb-slate-900 text-white'
        : 'text-tb-slate-700 hover:bg-tb-slate-200'}"
    >
      {f.label}
    </a>
  {/each}
</div>

<div class="mt-4">
  {#if data.batches.length === 0}
    <p
      class="rounded-tb-2xl border border-dashed border-tb-slate-300 bg-tb-slate-50/60 p-8 text-center text-sm text-tb-slate-500"
    >
      尚無月結批次
    </p>
  {:else}
    <Card>
      <!-- Mobile: card list. -->
      <div class="divide-y divide-tb-slate-100 md:hidden">
        {#each data.batches as b (b.id)}
          <div class="py-3 first:pt-0 last:pb-0">
            <div class="flex items-start justify-between gap-2">
              <div class="font-jetbrains-mono text-xs font-semibold text-tb-slate-800">
                {b.period_start} — {b.period_end}
              </div>
              <a
                href="/payroll/{b.id}"
                class="flex-shrink-0 text-sm font-semibold text-tb-red-600 hover:text-tb-red-700"
                >詳細</a
              >
            </div>
            <div class="mt-2 flex items-center gap-2">
              <StateTag tone={statusTone[b.status] ?? "neutral"} pulse={b.status === "locked"}>
                {statusLabel[b.status] ?? b.status}
              </StateTag>
            </div>
            <dl class="mt-2 grid grid-cols-2 gap-1 text-xs text-tb-slate-500">
              <div>
                <dt class="text-tb-slate-500">鎖定時間</dt>
                <dd class="font-jetbrains-mono">
                  {b.locked_at ? b.locked_at.slice(0, 16).replace("T", " ") : "—"}
                </dd>
              </div>
              <div>
                <dt class="text-tb-slate-500">匯出時間</dt>
                <dd class="font-jetbrains-mono">
                  {b.exported_at ? b.exported_at.slice(0, 16).replace("T", " ") : "—"}
                </dd>
              </div>
            </dl>
          </div>
        {/each}
      </div>

      <!-- Desktop: table (unchanged). -->
      <div class="hidden overflow-hidden rounded-xl border border-tb-slate-200 md:block">
        <table class="w-full text-sm">
          <thead
            class="bg-tb-slate-50/60 text-left text-[11px] font-bold uppercase tracking-wider text-tb-slate-500"
          >
            <tr>
              <th scope="col" class="px-4 py-2.5">月結周期</th>
              <th scope="col" class="px-4 py-2.5">狀態</th>
              <th scope="col" class="px-4 py-2.5">鎖定時間</th>
              <th scope="col" class="px-4 py-2.5">匯出時間</th>
              <th scope="col" class="px-4 py-2.5"></th>
            </tr>
          </thead>
          <tbody class="divide-y divide-tb-slate-100">
            {#each data.batches as b (b.id)}
              <tr class="hover:bg-tb-slate-50/60">
                <td class="px-4 py-3 font-jetbrains-mono text-xs font-semibold text-tb-slate-800">
                  {b.period_start} — {b.period_end}
                </td>
                <td class="px-4 py-3">
                  <StateTag tone={statusTone[b.status] ?? "neutral"} pulse={b.status === "locked"}>
                    {statusLabel[b.status] ?? b.status}
                  </StateTag>
                </td>
                <td class="px-4 py-3 font-jetbrains-mono text-xs text-tb-slate-500">
                  {b.locked_at ? b.locked_at.slice(0, 16).replace("T", " ") : "—"}
                </td>
                <td class="px-4 py-3 font-jetbrains-mono text-xs text-tb-slate-500">
                  {b.exported_at ? b.exported_at.slice(0, 16).replace("T", " ") : "—"}
                </td>
                <td class="px-4 py-3 text-right">
                  <a
                    href="/payroll/{b.id}"
                    class="text-sm font-semibold text-tb-red-600 hover:text-tb-red-700">詳細</a
                  >
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    </Card>
  {/if}
</div>
