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

{#if entries.length === 0}
  <p
    class="rounded-tb-2xl border border-dashed border-tb-slate-300 bg-tb-slate-50/60 p-8 text-center text-sm text-tb-slate-500"
  >
    本批次尚無代扣明細
  </p>
{:else}
  <Card title="代扣明細" description="{entries.length} 筆 · 金額為新台幣">
    <div class="overflow-hidden rounded-xl border border-tb-slate-200">
      <table class="w-full text-sm">
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
