<script lang="ts">
  import { PageHeader, Card, StatCard, StateTag, EmptyState, Button, Icon } from "@tbite/ui";
  import { formatMinor } from "$lib/money";

  let { data } = $props();

  // The detail payload may nest its summary under `settlement` or be flat.
  const s = $derived(data.settlement?.settlement ?? data.settlement ?? {});
  const orders = $derived(s.orders ?? data.settlement?.orders ?? []);

  const orderStatusMeta = {
    picked_up: { tone: "success", label: "已領取" },
    no_show: { tone: "danger", label: "未領取" },
    cancelled: { tone: "neutral", label: "已取消" },
    refunded: { tone: "warning", label: "已退款" },
    placed: { tone: "info", label: "已預訂" },
    cutoff: { tone: "warning", label: "已截單" },
    ready: { tone: "info", label: "備餐完成" },
  } as Record<
    string,
    { tone: "success" | "danger" | "neutral" | "warning" | "info"; label: string }
  >;

  function fmtPeriod(start: string | undefined, end: string | undefined): string {
    if (!start) return "—";
    if (!end || end === start) return start;
    return `${start} ～ ${end}`;
  }

  function fmtDate(v: string | undefined | null): string {
    if (!v) return "—";
    return new Date(v).toLocaleDateString("zh-TW", {
      year: "numeric",
      month: "2-digit",
      day: "2-digit",
    });
  }

  function orderPortions(o: any): number {
    if (typeof o.portion_count === "number") return o.portion_count;
    return (o.items ?? []).reduce((sum: number, it: any) => sum + (it.qty ?? 0), 0);
  }
</script>

<PageHeader eyebrow="Reconciliation · 對帳單明細" title="對帳單明細">
  {#snippet actions()}
    <a href="/reconciliation">
      <Button variant="secondary" size="sm">
        <Icon name="chevron" class="h-4 w-4" />返回對帳
      </Button>
    </a>
  {/snippet}
</PageHeader>

<div class="mb-4 flex flex-wrap items-center gap-2">
  <span class="text-sm font-semibold text-tb-slate-700">
    結算期間 {fmtPeriod(s.period_start, s.period_end)}
  </span>
  {#if s.status}
    <StateTag tone={s.status === "void" ? "neutral" : "success"}>
      {s.status === "void" ? "已作廢" : "已結算"}
    </StateTag>
  {/if}
  {#if s.closed_at}
    <span class="text-xs text-tb-slate-400">關帳於 {fmtDate(s.closed_at)}</span>
  {/if}
</div>

<section class="mb-8 grid grid-cols-2 gap-3 md:grid-cols-3">
  <StatCard label="訂單數" value={s.order_count ?? 0} suffix="筆" />
  <StatCard label="餐點份數" value={s.portion_count ?? 0} suffix="份" />
  <StatCard label="結算金額" value={formatMinor(s.gross_minor)} />
</section>

<section>
  <h2 class="mb-3 text-lg font-bold text-tb-slate-900">訂單明細</h2>
  {#if orders.length === 0}
    <EmptyState icon="doc" title="無訂單明細" hint="此對帳單未包含可展開的訂單級資料。" />
  {:else}
    <div class="overflow-hidden rounded-tb-2xl border border-tb-slate-200 bg-white shadow-tb-sm">
      <table class="w-full text-sm">
        <thead
          class="bg-tb-slate-50/60 text-left text-[11px] font-bold uppercase tracking-eyebrow text-tb-slate-500"
        >
          <tr>
            <th class="px-5 py-3">訂單</th>
            <th class="px-3 py-3">供餐日</th>
            <th class="px-3 py-3">廠區</th>
            <th class="px-3 py-3 text-right">份數</th>
            <th class="px-3 py-3 text-right">金額</th>
            <th class="px-5 py-3">狀態</th>
          </tr>
        </thead>
        <tbody class="divide-y divide-tb-slate-100">
          {#each orders as o (o.id)}
            {@const meta = orderStatusMeta[o.status] ?? { tone: "neutral", label: o.status }}
            <tr class="hover:bg-tb-slate-50/60">
              <td class="px-5 py-3 font-jetbrains-mono text-xs text-tb-slate-500">
                {String(o.id ?? "").slice(0, 8)}…
              </td>
              <td class="px-3 py-3 font-jetbrains-mono text-xs text-tb-slate-600">
                {o.supply_date ?? "—"}
              </td>
              <td class="px-3 py-3 text-tb-slate-700">{o.plant ?? "—"}</td>
              <td class="px-3 py-3 text-right font-jetbrains-mono tabular-nums text-tb-slate-700">
                {orderPortions(o)}
              </td>
              <td class="px-3 py-3 text-right font-jetbrains-mono tabular-nums text-tb-slate-900">
                {formatMinor(o.total_price_minor)}
              </td>
              <td class="px-5 py-3">
                <StateTag tone={meta.tone}>{meta.label}</StateTag>
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  {/if}
</section>
