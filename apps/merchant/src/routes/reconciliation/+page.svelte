<script lang="ts">
  import { PageHeader, Card, StatCard, StateTag, EmptyState } from "@tbite/ui";
  import type { components } from "@tbite/api-client";
  import { formatMinor } from "$lib/money";

  type StatusBreakdownDTO = components["schemas"]["StatusBreakdownDTO"];

  let { data } = $props();

  const recon = $derived(data.reconciliation);

  // Status-breakdown rows for the live monthly summary.
  const breakdownMeta: {
    key: keyof StatusBreakdownDTO;
    label: string;
    tone: "success" | "danger" | "neutral" | "warning";
  }[] = [
    { key: "picked_up", label: "已領取", tone: "success" },
    { key: "no_show", label: "未領取", tone: "danger" },
    { key: "cancelled", label: "已取消", tone: "neutral" },
    { key: "refunded", label: "已退款", tone: "warning" },
  ];

  function breakdownValue(key: keyof StatusBreakdownDTO): number {
    return recon?.breakdown?.[key] ?? 0;
  }

  function fmtPeriod(start: string | undefined, end: string | undefined): string {
    if (!start) return "—";
    if (!end || end === start) return start;
    return `${start} ～ ${end}`;
  }

  function fmtDate(s: string | undefined | null): string {
    if (!s) return "—";
    return new Date(s).toLocaleDateString("zh-TW", {
      year: "numeric",
      month: "2-digit",
      day: "2-digit",
    });
  }

  const settlementStatusMeta = {
    closed: { tone: "success", label: "已結算" },
    void: { tone: "neutral", label: "已作廢" },
  } as Record<string, { tone: "success" | "neutral"; label: string }>;
</script>

<PageHeader
  eyebrow="Reconciliation · 商家對帳"
  title="商家對帳"
  subtitle="當月為即時推導的暫估金額（未關帳）；下方為福委會已關帳的歷史對帳單。"
/>

<!-- Live current-month summary -->
<section class="mb-8">
  <div class="mb-3 flex items-baseline justify-between">
    <h2 class="text-lg font-bold text-tb-slate-900">本月即時摘要</h2>
    <span class="font-jetbrains-mono text-sm text-tb-slate-500">{data.period}</span>
  </div>

  {#if !recon}
    <EmptyState
      icon="wallet"
      title="尚無本月對帳資料"
      hint="本月尚無已備餐訂單，或資料載入失敗。"
    />
  {:else}
    <div class="mb-3 grid grid-cols-2 gap-3 md:grid-cols-3">
      <StatCard label="訂單數" value={recon.order_count ?? 0} suffix="筆" />
      <StatCard label="餐點份數" value={recon.portion_count ?? 0} suffix="份" />
      <StatCard label="應收金額（暫估）" value={formatMinor(recon.gross_minor)} />
    </div>
    <Card>
      <h3 class="mb-3 text-sm font-bold text-tb-slate-900">訂單狀態分布</h3>
      <div class="grid grid-cols-2 gap-3 md:grid-cols-4">
        {#each breakdownMeta as b (b.key)}
          <div class="rounded-xl border border-tb-slate-200 px-3 py-2.5">
            <StateTag tone={b.tone}>{b.label}</StateTag>
            <p
              class="mt-1.5 font-jetbrains-mono text-2xl font-black tabular-nums text-tb-slate-900"
            >
              {breakdownValue(b.key)}
            </p>
          </div>
        {/each}
      </div>
      <p class="mt-3 text-xs text-tb-slate-400">
        應收金額計入已備餐（已領取／未領取）訂單，與員工薪資代扣口徑一致。
      </p>
    </Card>
  {/if}
</section>

<!-- Historical closed settlements -->
<section>
  <h2 class="mb-3 text-lg font-bold text-tb-slate-900">歷史對帳單</h2>
  {#if data.settlements.length === 0}
    <EmptyState icon="doc" title="尚無已關帳對帳單" hint="福委會關帳後，每月對帳單會列於此。" />
  {:else}
    <!-- Mobile: stacked cards -->
    <div class="space-y-3 md:hidden">
      {#each data.settlements as s (s.id)}
        {@const meta = settlementStatusMeta[s.status] ?? { tone: "neutral", label: s.status }}
        <div class="rounded-tb-2xl border border-tb-slate-200 bg-white p-4 shadow-tb-sm">
          <div class="mb-2 flex items-start justify-between gap-3">
            <div class="font-semibold text-tb-slate-900">
              {fmtPeriod(s.period_start, s.period_end)}
            </div>
            <StateTag tone={meta.tone}>{meta.label}</StateTag>
          </div>
          <dl class="grid grid-cols-3 gap-2 text-xs">
            <div>
              <dt class="text-tb-slate-400">訂單數</dt>
              <dd class="font-jetbrains-mono tabular-nums text-tb-slate-700">
                {s.order_count ?? 0}
              </dd>
            </div>
            <div>
              <dt class="text-tb-slate-400">份數</dt>
              <dd class="font-jetbrains-mono tabular-nums text-tb-slate-700">
                {s.portion_count ?? 0}
              </dd>
            </div>
            <div>
              <dt class="text-tb-slate-400">金額</dt>
              <dd class="font-jetbrains-mono tabular-nums text-tb-slate-900">
                {formatMinor(s.gross_minor)}
              </dd>
            </div>
          </dl>
          <div class="mt-2 flex items-center justify-between">
            <span class="font-jetbrains-mono text-xs text-tb-slate-500">
              關帳日 {fmtDate(s.closed_at)}
            </span>
            <a
              href="/reconciliation/{s.id}"
              class="text-sm font-semibold text-tb-red-600 hover:text-tb-red-700"
            >
              明細
            </a>
          </div>
        </div>
      {/each}
    </div>

    <!-- Desktop: table -->
    <div
      class="hidden overflow-hidden rounded-tb-2xl border border-tb-slate-200 bg-white shadow-tb-sm md:block"
    >
      <table class="w-full text-sm">
        <thead
          class="bg-tb-slate-50/60 text-left text-[11px] font-bold uppercase tracking-eyebrow text-tb-slate-500"
        >
          <tr>
            <th class="px-5 py-3">結算期間</th>
            <th class="px-3 py-3 text-right">訂單數</th>
            <th class="px-3 py-3 text-right">份數</th>
            <th class="px-3 py-3 text-right">金額</th>
            <th class="px-3 py-3">狀態</th>
            <th class="px-3 py-3">關帳日</th>
            <th class="px-5 py-3"></th>
          </tr>
        </thead>
        <tbody class="divide-y divide-tb-slate-100">
          {#each data.settlements as s (s.id)}
            {@const meta = settlementStatusMeta[s.status] ?? { tone: "neutral", label: s.status }}
            <tr class="hover:bg-tb-slate-50/60">
              <td class="px-5 py-3 font-semibold text-tb-slate-900">
                {fmtPeriod(s.period_start, s.period_end)}
              </td>
              <td class="px-3 py-3 text-right font-jetbrains-mono tabular-nums text-tb-slate-700">
                {s.order_count ?? 0}
              </td>
              <td class="px-3 py-3 text-right font-jetbrains-mono tabular-nums text-tb-slate-700">
                {s.portion_count ?? 0}
              </td>
              <td class="px-3 py-3 text-right font-jetbrains-mono tabular-nums text-tb-slate-900">
                {formatMinor(s.gross_minor)}
              </td>
              <td class="px-3 py-3">
                <StateTag tone={meta.tone}>{meta.label}</StateTag>
              </td>
              <td class="px-3 py-3 font-jetbrains-mono text-xs text-tb-slate-500">
                {fmtDate(s.closed_at)}
              </td>
              <td class="px-5 py-3 text-right">
                <a
                  href="/reconciliation/{s.id}"
                  class="text-sm font-semibold text-tb-red-600 hover:text-tb-red-700"
                >
                  明細
                </a>
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  {/if}
</section>
