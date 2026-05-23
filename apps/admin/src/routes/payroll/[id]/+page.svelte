<script lang="ts">
  import { onDestroy } from "svelte";
  import { invalidateAll } from "$app/navigation";
  import { PageHeader, Card, StatCard, StateTag, Button, Icon } from "@tbite/ui";

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

  const exceptions = $derived(data.exceptions ?? []);
  const exKindLabel = {
    employee_departed: "員工已離職／停用",
    deduction_failed: "扣款失敗",
  } as Record<string, string>;
  const exStatusMeta = {
    open: { tone: "warning", label: "待處理" },
    resolved: { tone: "success", label: "已處理" },
    excluded: { tone: "neutral", label: "已排除出 CSV" },
  } as Record<string, { tone: "warning" | "success" | "neutral"; label: string }>;

  const totals = $derived.by(() => {
    let amount = 0;
    let refunded = 0;
    for (const e of entries) {
      amount += Number(e.amount_minor ?? 0);
      refunded += Number(e.refunded_minor ?? 0);
    }
    return { amount, refunded, net: amount - refunded };
  });
  const ntd = (minor: number) => "$" + Math.round(minor).toLocaleString();
</script>

<a
  href="/payroll"
  class="-mb-2 inline-flex items-center gap-1 text-xs font-semibold text-tb-slate-500 hover:text-tb-slate-700"
>
  <Icon name="chevron" class="h-3.5 w-3.5 rotate-90" />返回批次列表
</a>

<PageHeader
  eyebrow="薪資代扣"
  title="{b.period_start} — {b.period_end}"
  subtitle="月結批次明細 · 鎖定後由 settler worker 產生 HR CSV"
>
  {#snippet actions()}
    <StateTag tone={statusTone[b.status] ?? "neutral"} pulse={b.status === "locked"}>
      {statusLabel[b.status] ?? b.status}
    </StateTag>
    <a href="/payroll/{b.id}/disputes">
      <Button variant="secondary" size="sm">
        <Icon name="alert" class="h-3.5 w-3.5" />爭議列表
      </Button>
    </a>
  {/snippet}
</PageHeader>

{#if form?.error}
  <p class="rounded-lg bg-tb-rose-50 px-3 py-2 text-sm text-tb-rose-700">{form.error}</p>
{/if}

<section class="grid gap-3 sm:grid-cols-4">
  <StatCard label="代扣筆數" value={entries.length} delta="筆" deltaTone="info" />
  <StatCard label="代扣總額" value={ntd(totals.amount)} />
  <StatCard label="退款金額" value={ntd(totals.refunded)} delta="退款" deltaTone="warning" />
  <StatCard label="淨額" value={ntd(totals.net)} delta="HR 匯款" deltaTone="success" />
</section>

<Card title="批次操作">
  <div class="flex flex-wrap items-center gap-2">
    {#if b.status === "draft"}
      <form method="POST" action="?/lock">
        <Button variant="primary" size="md" type="submit">鎖定批次並排程匯出</Button>
      </form>
    {/if}
    {#if b.status === "locked"}
      <p class="text-xs text-tb-slate-500">等待 settler worker 產生 CSV · 每 10 秒自動重新整理</p>
    {/if}
    {#if b.status === "exported" && b.export_uri}
      <a href={b.export_uri}>
        <Button variant="secondary" size="md">
          <Icon name="download" class="h-3.5 w-3.5" />下載 HR CSV
        </Button>
      </a>
      <span class="self-center break-all font-jetbrains-mono text-xs text-tb-slate-500">
        {b.export_uri}
      </span>
    {/if}
    {#if b.status === "closed"}
      <p class="text-xs text-tb-slate-500">此批次已關閉</p>
    {/if}
  </div>
</Card>

<Card
  title="月結例外清單"
  description="離職／停用員工自動偵測;扣款失敗可手動標記。排除的明細不會出現在 HR CSV。"
>
  {#if form?.exError}
    <p class="mb-3 rounded-tb-xl bg-tb-rose-50 px-3 py-2 text-sm text-tb-rose-700">
      {form.exError}
    </p>
  {/if}
  {#if exceptions.length === 0}
    <p class="text-sm text-tb-slate-500">目前沒有偵測到例外。</p>
  {:else}
    <ul class="divide-y divide-tb-slate-100">
      {#each exceptions as ex (ex.id)}
        {@const meta = exStatusMeta[ex.status] ?? { tone: "neutral", label: ex.status }}
        <li class="py-3">
          <div class="flex items-center justify-between gap-2">
            <div class="min-w-0">
              <span class="text-sm font-semibold text-tb-slate-900">
                {exKindLabel[ex.kind] ?? ex.kind}
              </span>
              <span class="font-jetbrains-mono text-xs text-tb-slate-500">
                · 員工 {ex.user_id.slice(0, 8)}
              </span>
            </div>
            <StateTag tone={meta.tone}>{meta.label}</StateTag>
          </div>
          {#if ex.detail}
            <p class="mt-1 text-xs text-tb-slate-500">{ex.detail}</p>
          {/if}
          {#if ex.resolution}
            <p class="mt-1 text-xs text-tb-slate-600">處理說明:{ex.resolution}</p>
          {/if}
          {#if ex.status === "open"}
            <div class="mt-2 flex gap-2">
              <form method="POST" action="?/resolveException">
                <input type="hidden" name="exception_id" value={ex.id} />
                <input type="hidden" name="status" value="resolved" />
                <Button variant="secondary" size="sm" type="submit">標記已處理</Button>
              </form>
              <form method="POST" action="?/resolveException">
                <input type="hidden" name="exception_id" value={ex.id} />
                <input type="hidden" name="status" value="excluded" />
                <Button variant="danger" size="sm" type="submit">排除出 CSV</Button>
              </form>
            </div>
          {/if}
        </li>
      {/each}
    </ul>
  {/if}
  {#if b.status === "draft" && entries.length > 0}
    <form
      method="POST"
      action="?/flagException"
      class="mt-4 flex flex-wrap items-end gap-2 border-t border-tb-slate-100 pt-4"
    >
      <label class="flex flex-col gap-1 text-xs">
        <span class="font-semibold text-tb-slate-600">手動標記扣款失敗</span>
        <select
          name="entry_id"
          required
          class="rounded-tb-lg border border-tb-slate-300 px-3 py-2 text-sm transition focus:border-tb-red-500 focus:outline-none focus:ring-4 focus:ring-tb-red-100"
        >
          <option value="" disabled selected>選擇代扣明細</option>
          {#each entries as e (e.id)}
            <option value={e.id}>
              員工 {e.user_id.slice(0, 8)} · {ntd(Number(e.amount_minor))}
            </option>
          {/each}
        </select>
      </label>
      <input
        name="detail"
        maxlength="500"
        placeholder="原因（選填）"
        class="flex-1 rounded-tb-lg border border-tb-slate-300 px-3 py-2 text-sm transition focus:border-tb-red-500 focus:outline-none focus:ring-4 focus:ring-tb-red-100"
      />
      <Button variant="secondary" size="sm" type="submit">標記例外</Button>
    </form>
  {/if}
</Card>

{#if entries.length === 0}
  <p
    class="rounded-tb-2xl border border-dashed border-tb-slate-300 bg-tb-slate-50/60 p-8 text-center text-sm text-tb-slate-500"
  >
    本批次尚無代扣明細
  </p>
{:else}
  <Card title="代扣明細" description="{entries.length} 筆 · 金額為新台幣">
    <div class="overflow-x-auto rounded-xl border border-tb-slate-200">
      <table class="w-full min-w-[36rem] text-sm">
        <thead
          class="bg-tb-slate-50/60 text-left text-[11px] font-bold uppercase tracking-wider text-tb-slate-500"
        >
          <tr>
            <th class="px-4 py-2.5">員工</th>
            <th class="px-4 py-2.5 text-right">訂單數</th>
            <th class="px-4 py-2.5 text-right">代扣金額</th>
            <th class="px-4 py-2.5 text-right">退款</th>
            <th class="px-4 py-2.5 text-right">淨額</th>
          </tr>
        </thead>
        <tbody class="divide-y divide-tb-slate-100">
          {#each entries as e (e.id)}
            <tr class="hover:bg-tb-slate-50/60">
              <td class="px-4 py-2.5 font-jetbrains-mono text-xs text-tb-slate-700">
                {e.user_id.slice(0, 8)}
              </td>
              <td class="px-4 py-2.5 text-right font-jetbrains-mono tabular-nums">
                {(e.order_ids ?? []).length}
              </td>
              <td class="px-4 py-2.5 text-right font-jetbrains-mono tabular-nums">
                {ntd(Number(e.amount_minor))}
              </td>
              <td class="px-4 py-2.5 text-right font-jetbrains-mono tabular-nums text-tb-rose-700">
                {Number(e.refunded_minor) > 0 ? ntd(Number(e.refunded_minor)) : "—"}
              </td>
              <td class="px-4 py-2.5 text-right font-jetbrains-mono font-bold tabular-nums">
                {ntd(Number(e.amount_minor) - Number(e.refunded_minor))}
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  </Card>
{/if}
