<script lang="ts">
  import { onMount, onDestroy } from "svelte";
  import { invalidateAll } from "$app/navigation";
  import { Card, StateTag } from "@tbite/ui";

  let { data, form } = $props();
  const b = $derived(data.batch);
  const entries = $derived(data.entries);

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

  let timer: ReturnType<typeof setInterval> | null = null;
  $effect(() => {
    if (timer) {
      clearInterval(timer);
      timer = null;
    }
    if (b?.status === "locked") {
      timer = setInterval(() => {
        invalidateAll();
      }, 10_000);
    }
  });
  onDestroy(() => {
    if (timer) clearInterval(timer);
  });

  const totals = $derived.by(() => {
    let amount = 0;
    let refunded = 0;
    for (const e of entries) {
      amount += Number(e.amount_minor ?? 0);
      refunded += Number(e.refunded_minor ?? 0);
    }
    return { amount, refunded, net: amount - refunded };
  });
</script>

<section class="space-y-4">
  <header>
    <a href="/payroll" class="text-xs text-tb-slate-500 hover:text-tb-slate-700">← 返回列表</a>
    <h1 class="mt-1 text-2xl font-black text-tb-slate-900 font-jetbrains-mono">
      {b.period_start} — {b.period_end}
    </h1>
    <div class="mt-2 flex items-center gap-3">
      <StateTag tone={statusTone[b.status] ?? "neutral"} pulse={b.status === "locked"}>
        {statusLabel[b.status] ?? b.status}
      </StateTag>
      <a href="/payroll/{b.id}/disputes" class="text-sm text-tb-red-600 hover:text-tb-red-700">→ 爭議列表</a>
    </div>
  </header>

  {#if form?.error}
    <p class="rounded-lg bg-tb-rose-50 px-3 py-2 text-sm text-tb-rose-700">{form.error}</p>
  {/if}

  <Card>
    <div class="grid grid-cols-2 gap-4 text-sm sm:grid-cols-4">
      <div>
        <p class="text-xs uppercase tracking-eyebrow text-tb-slate-500">筆數</p>
        <p class="mt-1 font-semibold text-tb-slate-900">{entries.length}</p>
      </div>
      <div>
        <p class="text-xs uppercase tracking-eyebrow text-tb-slate-500">總額</p>
        <p class="mt-1 font-semibold text-tb-slate-900">${totals.amount.toLocaleString()}</p>
      </div>
      <div>
        <p class="text-xs uppercase tracking-eyebrow text-tb-slate-500">退款</p>
        <p class="mt-1 font-semibold text-tb-rose-700">${totals.refunded.toLocaleString()}</p>
      </div>
      <div>
        <p class="text-xs uppercase tracking-eyebrow text-tb-slate-500">淨額</p>
        <p class="mt-1 font-semibold text-tb-slate-900">${totals.net.toLocaleString()}</p>
      </div>
    </div>

    <div class="mt-4 flex flex-wrap gap-2 border-t border-tb-slate-100 pt-3">
      {#if b.status === "draft"}
        <form method="POST" action="?/lock">
          <button class="rounded-lg bg-tb-red-600 px-3.5 py-2 text-sm font-semibold text-white hover:bg-tb-red-700">
            鎖定批次並排程匯出
          </button>
        </form>
      {/if}
      {#if b.status === "locked"}
        <p class="text-xs text-tb-slate-500">等待 settler worker 產生 CSV — 每 10 秒自動重新整理</p>
      {/if}
      {#if b.status === "exported" && b.export_uri}
        <a
          href={b.export_uri}
          class="rounded-lg border border-tb-slate-300 px-3.5 py-2 text-sm font-semibold text-tb-slate-800 hover:border-tb-slate-500"
        >
          下載 HR CSV
        </a>
        <span class="font-jetbrains-mono break-all text-xs text-tb-slate-500 self-center">{b.export_uri}</span>
      {/if}
    </div>
  </Card>

  {#if entries.length === 0}
    <p class="rounded-tb-2xl border border-tb-slate-200 bg-white p-6 text-center text-sm text-tb-slate-500">
      本批次無 entries
    </p>
  {:else}
    <div class="overflow-hidden rounded-tb-2xl border border-tb-slate-200 bg-white shadow-tb-sm">
      <table class="w-full text-sm">
        <thead class="bg-tb-slate-50 text-left text-xs uppercase tracking-eyebrow text-tb-slate-500">
          <tr>
            <th class="px-4 py-2">user</th>
            <th class="px-4 py-2">orders</th>
            <th class="px-4 py-2 text-right">amount (NTD)</th>
            <th class="px-4 py-2 text-right">refunded (NTD)</th>
            <th class="px-4 py-2 text-right">net</th>
          </tr>
        </thead>
        <tbody>
          {#each entries as e (e.id)}
            <tr class="border-t border-tb-slate-100">
              <td class="px-4 py-3 font-jetbrains-mono text-xs text-tb-slate-700">{e.user_id.slice(0, 8)}</td>
              <td class="px-4 py-3 text-tb-slate-500">{(e.order_ids ?? []).length}</td>
              <td class="px-4 py-3 text-right font-jetbrains-mono">${Number(e.amount_minor).toLocaleString()}</td>
              <td class="px-4 py-3 text-right font-jetbrains-mono text-tb-rose-700">
                ${Number(e.refunded_minor).toLocaleString()}
              </td>
              <td class="px-4 py-3 text-right font-jetbrains-mono font-semibold">
                ${(Number(e.amount_minor) - Number(e.refunded_minor)).toLocaleString()}
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  {/if}
</section>
