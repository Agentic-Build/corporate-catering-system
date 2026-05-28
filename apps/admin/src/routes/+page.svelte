<script lang="ts">
  import { Card, StatCard, StateTag, Button, Icon } from "@tbite/ui";
  import ApprovalCard from "$lib/components/ApprovalCard.svelte";
  import AlertList from "$lib/components/AlertList.svelte";

  let { data } = $props();

  const ntd = (minor: number) => "$" + Math.round(minor).toLocaleString();
  function compact(minor: number): string {
    const n = Math.round(minor);
    if (n >= 1_000_000) return "$" + (n / 1_000_000).toFixed(2) + "M";
    if (n >= 1_000) return "$" + (n / 1_000).toFixed(1) + "K";
    return "$" + n.toLocaleString();
  }

  const batch = $derived(data.payroll.batch);
  const entries = $derived(data.payroll.entries);
  const netTotal = $derived(data.payroll.total - data.payroll.refunded);

  const summary = $derived(
    `${data.counts.pending} 件審核待處理 · ${data.counts.anomalies7d} 則告警` +
      (batch ? ` · 本月月結 ${ntd(netTotal)}` : ""),
  );
</script>

<section>
  <div class="text-[11px] font-bold uppercase tracking-eyebrow text-tb-red-600">
    合規 · 治理 · 對帳
  </div>
  <h1 class="mt-1 text-3xl font-black tracking-tight text-tb-slate-900">福委會後台</h1>
  <p class="mt-1 text-sm text-tb-slate-500">{summary}</p>
</section>

<section class="grid gap-3 md:grid-cols-4">
  <StatCard
    label="待審商家"
    value={data.counts.pending}
    delta={data.counts.pending > 0 ? "待處理" : "已清空"}
    deltaTone={data.counts.pending > 0 ? "warning" : "success"}
  />
  <StatCard label="已核准商家" value={data.counts.approved} delta="營運中" deltaTone="success" />
  <StatCard
    label="近 7 日告警"
    value={data.counts.anomalies7d}
    delta={`${data.counts.anomaliesSevere} 嚴重`}
    deltaTone={data.counts.anomaliesSevere > 0 ? "danger" : "info"}
  />
  <StatCard
    label="本月對帳金額"
    value={batch ? compact(netTotal) : "—"}
    delta={batch ? `${entries.length} 筆` : "尚無批次"}
    deltaTone="info"
  />
</section>

<Card
  title="商家入駐審核"
  description="{data.counts.pending} 件申請待處理 · 操作會自動寫入稽核日誌"
>
  {#snippet actions()}
    <a href="/audit">
      <Button variant="secondary" size="sm">
        <Icon name="download" class="h-3.5 w-3.5" />下載審核紀錄
      </Button>
    </a>
    <a href="/vendors">
      <Button variant="primary" size="sm">
        <Icon name="plus" class="h-3.5 w-3.5" />新增邀請
      </Button>
    </a>
  {/snippet}
  {#if data.pendingVendors.length === 0}
    <p
      class="rounded-xl border border-dashed border-tb-slate-300 bg-tb-slate-50/60 px-4 py-6 text-center text-sm text-tb-slate-500"
    >
      目前沒有待審核的商家申請
    </p>
  {:else}
    <div class="grid gap-3">
      {#each data.pendingVendors as v (v.id)}
        <ApprovalCard vendor={v} />
      {/each}
    </div>
  {/if}
</Card>

<AlertList anomalies={data.anomalies} />

<Card
  title="本月月結預覽"
  description={batch
    ? `${batch.period_start} — ${batch.period_end} · ${entries.length} 筆 · 退款 ${ntd(data.payroll.refunded)}`
    : "尚無月結批次"}
>
  {#snippet actions()}
    {#if batch}
      <a href="/payroll/{batch.id}">
        <Button variant="primary" size="sm">
          <Icon name="chevron" class="h-3.5 w-3.5 -rotate-90" />批次詳情
        </Button>
      </a>
    {:else}
      <a href="/payroll/new">
        <Button variant="primary" size="sm">
          <Icon name="plus" class="h-3.5 w-3.5" />建立批次
        </Button>
      </a>
    {/if}
  {/snippet}

  {#if !batch || entries.length === 0}
    <p
      class="rounded-xl border border-dashed border-tb-slate-300 bg-tb-slate-50/60 px-4 py-6 text-center text-sm text-tb-slate-500"
    >
      {batch ? "本批次尚無月結明細" : "尚未建立月結批次"}
    </p>
  {:else}
    <div class="divide-y divide-tb-slate-100 md:hidden">
      {#each entries.slice(0, 8) as e (e.id)}
        <div class="py-3 first:pt-0 last:pb-0">
          <div class="flex items-center justify-between gap-2">
            <span class="font-jetbrains-mono text-xs text-tb-slate-700"
              >{e.user_id.slice(0, 8)}</span
            >
            {#if Number(e.refunded_minor) > 0}
              <StateTag tone="warning">部分退款</StateTag>
            {:else}
              <StateTag tone="success">已對帳</StateTag>
            {/if}
          </div>
          <div class="mt-2 flex items-center justify-between gap-2 text-xs text-tb-slate-500">
            <span>{(e.order_ids ?? []).length} 筆訂單</span>
            <span class="font-jetbrains-mono">
              {#if Number(e.refunded_minor) > 0}退款 {ntd(Number(e.refunded_minor))} ·{/if}
              <span class="font-bold text-tb-slate-900 tabular-nums"
                >{ntd(Number(e.amount_minor))}</span
              >
            </span>
          </div>
        </div>
      {/each}
    </div>

    <div class="hidden overflow-hidden rounded-xl border border-tb-slate-200 md:block">
      <table class="w-full text-sm">
        <thead
          class="bg-tb-slate-50/60 text-left text-[11px] font-bold uppercase tracking-wider text-tb-slate-500"
        >
          <tr>
            <th class="px-4 py-2.5">員工</th>
            <th class="px-4 py-2.5 text-right">訂單數</th>
            <th class="px-4 py-2.5 text-right">月結金額</th>
            <th class="px-4 py-2.5 text-right">退款</th>
            <th class="px-4 py-2.5">狀態</th>
          </tr>
        </thead>
        <tbody class="divide-y divide-tb-slate-100">
          {#each entries.slice(0, 8) as e (e.id)}
            <tr class="hover:bg-tb-slate-50/60">
              <td class="px-4 py-2.5 font-jetbrains-mono text-xs text-tb-slate-700">
                {e.user_id.slice(0, 8)}
              </td>
              <td class="px-4 py-2.5 text-right font-jetbrains-mono tabular-nums">
                {(e.order_ids ?? []).length}
              </td>
              <td class="px-4 py-2.5 text-right font-jetbrains-mono font-bold tabular-nums">
                {ntd(Number(e.amount_minor))}
              </td>
              <td class="px-4 py-2.5 text-right font-jetbrains-mono tabular-nums text-tb-rose-700">
                {Number(e.refunded_minor) > 0 ? ntd(Number(e.refunded_minor)) : "—"}
              </td>
              <td class="px-4 py-2.5">
                {#if Number(e.refunded_minor) > 0}
                  <StateTag tone="warning">部分退款</StateTag>
                {:else}
                  <StateTag tone="success">已對帳</StateTag>
                {/if}
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
    {#if entries.length > 8}
      <p class="mt-3 text-center text-xs text-tb-slate-500">
        顯示前 8 筆，共 {entries.length} 筆 ·
        <a href="/payroll/{batch.id}" class="font-semibold text-tb-red-600 hover:text-tb-red-700"
          >查看完整批次</a
        >
      </p>
    {/if}
  {/if}
</Card>
