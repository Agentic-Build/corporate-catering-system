<script lang="ts">
  // 我的訂單 — design-language pass. Tabs + OrderCard styling ported from
  // EmployeePages.jsx; bound to the real /api/employee/orders payload.
  import { PageHeader, Tabs, StateTag, EmptyState, Icon } from "@tbite/ui";

  type Order = {
    id: string;
    status: string;
    supply_date: string;
    plant: string;
    cutoff_at: string;
    total_price_minor: number;
    items: { id: string; menu_item_id: string; qty: number; unit_price_minor: number }[] | null;
  };

  let { data } = $props();
  const orders = $derived((data.orders as Order[]) ?? []);

  const statusTone: Record<string, "info" | "neutral" | "warning" | "danger" | "success"> = {
    draft: "neutral",
    placed: "info",
    cutoff: "warning",
    cancelled: "neutral",
    ready: "success",
    picked_up: "success",
    no_show: "danger",
    refunded: "warning",
  };
  const statusLabel: Record<string, string> = {
    draft: "草稿",
    placed: "已預訂",
    cutoff: "已截單",
    cancelled: "已取消",
    ready: "備餐完成",
    picked_up: "已領取",
    no_show: "未領取",
    refunded: "已退款",
  };
  // Left-accent colour, mirroring the reference OrderCard.
  const accent: Record<string, string> = {
    draft: "border-l-tb-slate-300",
    placed: "border-l-tb-red-600",
    cutoff: "border-l-tb-amber-500",
    cancelled: "border-l-tb-slate-300",
    ready: "border-l-tb-emerald-500",
    picked_up: "border-l-tb-emerald-500",
    no_show: "border-l-tb-rose-600",
    refunded: "border-l-tb-amber-500",
  };

  // Three tabs: 進行中 (draft/placed/cutoff/ready), 已完成 (picked_up/refunded), 已取消.
  const ACTIVE = new Set(["draft", "placed", "cutoff", "ready"]);
  const DONE = new Set(["picked_up", "refunded"]);
  const CANCELLED = new Set(["cancelled", "no_show"]);

  let tab = $state("active");
  const groups = $derived({
    active: orders.filter((o) => ACTIVE.has(o.status)),
    done: orders.filter((o) => DONE.has(o.status)),
    cancelled: orders.filter((o) => CANCELLED.has(o.status)),
  });
  const tabs = $derived([
    { id: "active", label: "進行中", count: groups.active.length },
    { id: "done", label: "已完成", count: groups.done.length },
    { id: "cancelled", label: "已取消", count: groups.cancelled.length },
  ]);
  const shown = $derived(groups[tab as "active" | "done" | "cancelled"]);

  function itemCount(o: Order): number {
    return (o.items ?? []).reduce((s, it) => s + it.qty, 0);
  }
  function fmtCutoff(iso: string): string {
    return iso ? iso.slice(0, 16).replace("T", " ") : "";
  }
</script>

<PageHeader
  eyebrow="Orders · 我的訂單"
  title="訂單與行程"
  subtitle="進行中的訂單可在截單前自由修改份數或取消，不會產生費用。"
>
  {#snippet actions()}
    <a
      href="/payroll"
      class="inline-flex items-center gap-1.5 rounded-tb-lg border border-tb-slate-300 px-3 py-2 text-sm font-semibold text-tb-slate-800 transition hover:border-tb-slate-500"
    >
      <Icon name="wallet" class="h-4 w-4" />薪資代扣明細
    </a>
  {/snippet}
</PageHeader>

<Tabs {tabs} active={tab} onChange={(id) => (tab = id)} />

<div class="grid gap-3">
  {#if shown.length === 0}
    <EmptyState icon="doc" title="目前沒有{tabs.find((t) => t.id === tab)?.label}的訂單" />
  {:else}
    {#each shown as o (o.id)}
      <a
        href={`/orders/${o.id}`}
        class="block overflow-hidden rounded-tb-2xl border border-l-4 border-tb-slate-200 {accent[
          o.status
        ] ?? 'border-l-tb-slate-300'} bg-white shadow-tb-sm transition hover:shadow-tb-md"
      >
        <header
          class="flex flex-wrap items-center justify-between gap-3 border-b border-tb-slate-100 px-5 py-3"
        >
          <div class="flex flex-wrap items-center gap-2.5">
            <StateTag tone={statusTone[o.status] ?? "neutral"}>
              {statusLabel[o.status] ?? o.status}
            </StateTag>
            <span class="text-sm font-bold text-tb-slate-900">{o.supply_date}</span>
            <span class="text-tb-slate-300">·</span>
            <span class="font-jetbrains-mono text-[11px] text-tb-slate-500">
              {o.id.slice(0, 8)}
            </span>
          </div>
          <div class="flex items-center gap-1.5 text-xs text-tb-slate-500">
            <Icon name="pin" class="h-3.5 w-3.5 text-tb-red-600" />{o.plant}
          </div>
        </header>
        <footer
          class="flex flex-wrap items-center justify-between gap-3 bg-tb-slate-50/60 px-5 py-3"
        >
          <div class="text-xs text-tb-slate-500">
            {#if o.status === "placed" || o.status === "draft"}
              <span class="inline-flex items-center gap-1.5 font-semibold text-tb-amber-700">
                <span class="h-1.5 w-1.5 animate-pulse rounded-full bg-tb-amber-500"></span>
                截單 {fmtCutoff(o.cutoff_at)} 前可改
              </span>
            {:else}
              {itemCount(o)} 份餐點
            {/if}
          </div>
          <div class="text-right">
            <div class="text-[10px] text-tb-slate-500">合計（薪資代扣）</div>
            <div class="font-jetbrains-mono text-lg font-black tabular-nums text-tb-slate-900">
              ${o.total_price_minor.toLocaleString()}
            </div>
          </div>
        </footer>
      </a>
    {/each}
  {/if}
</div>
