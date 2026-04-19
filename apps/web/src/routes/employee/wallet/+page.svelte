<script lang="ts">
  import { onMount } from "svelte";

  import {
    PageHeader,
    Card,
    Button,
    StateTag,
    MoneyAmount,
    EmptyState
  } from "$lib/components/ui";
  import PlantGuard from "$lib/components/employee/plant-guard.svelte";
  import {
    configureEmployeeApi,
    describeApiError,
    loadMenuItemNameMap,
    summarizeOrderLineItems,
    todayTaipeiIsoDate,
    addDaysIsoDate,
    type EmployeeOrderView,
    type PayrollLedgerView
  } from "$lib/employee/api";
  import { isResolvedDisputeStatus } from "$lib/employee/portal";
  import {
    friendlyOrderStatus,
    orderStatusTone,
    ledgerKindIsDebit,
    maskIdentifier
  } from "$lib/platform/labels";
  import { apiClient } from "$lib/platform/api";

  import type { PageData } from "./$types";

  let { data }: { data: PageData } = $props();

  const actor = $derived(data.actor);
  const plantId = $derived(actor?.scope.plantIds[0] ?? null);
  const role = $derived(actor?.role ?? null);

  let orders = $state<EmployeeOrderView[]>([]);
  let loading = $state(true);
  let loadError = $state<string | null>(null);
  let ledgersByOrderId = $state<Record<string, PayrollLedgerView>>({});
  let menuItemNameMap = $state<Record<string, string>>({});

  const today = todayTaipeiIsoDate();

  function monthStart(isoDate: string, offsetMonths: number): string {
    const [yRaw, mRaw] = isoDate.split("-").map((v) => Number.parseInt(v, 10));
    const d = new Date(Date.UTC(yRaw, mRaw - 1 + offsetMonths, 1));
    const y = d.getUTCFullYear();
    const m = `${d.getUTCMonth() + 1}`.padStart(2, "0");
    return `${y}-${m}-01`;
  }
  function monthEnd(isoDate: string, offsetMonths: number): string {
    const [yRaw, mRaw] = isoDate.split("-").map((v) => Number.parseInt(v, 10));
    // Day 0 of next month = last day of target month.
    const d = new Date(Date.UTC(yRaw, mRaw + offsetMonths, 0));
    const y = d.getUTCFullYear();
    const m = `${d.getUTCMonth() + 1}`.padStart(2, "0");
    const day = `${d.getUTCDate()}`.padStart(2, "0");
    return `${y}-${m}-${day}`;
  }

  const thisMonthStart = monthStart(today, 0);
  const thisMonthEnd = monthEnd(today, 0);
  const lastMonthStart = monthStart(today, -1);
  const lastMonthEnd = monthEnd(today, -1);

  function withinRange(isoDate: string, from: string, to: string): boolean {
    return isoDate >= from && isoDate <= to;
  }

  function netDebitForOrdersInRange(fromDate: string, toDate: string): {
    amountMinor: number;
    currency: string;
  } {
    let totalMinor = 0;
    let currency = "TWD";
    for (const order of orders) {
      if (!withinRange(order.deliveryDate, fromDate, toDate)) continue;
      const ledger = ledgersByOrderId[order.orderId];
      if (!ledger) continue;
      currency = ledger.currency;
      for (const entry of ledger.ledgerEntries) {
        if (ledgerKindIsDebit(entry.kind)) {
          totalMinor += entry.amount.amountMinor;
        } else {
          totalMinor -= entry.amount.amountMinor;
        }
      }
    }
    return { amountMinor: totalMinor, currency };
  }

  const summary = $derived.by(() => {
    const thisMonth = netDebitForOrdersInRange(thisMonthStart, thisMonthEnd);
    const lastMonth = netDebitForOrdersInRange(lastMonthStart, lastMonthEnd);
    let openDisputeCount = 0;
    let resolvedDisputeCount = 0;
    for (const ledger of Object.values(ledgersByOrderId)) {
      for (const dispute of ledger.disputes) {
        if (isResolvedDisputeStatus(dispute.status)) {
          resolvedDisputeCount += 1;
        } else {
          openDisputeCount += 1;
        }
      }
    }
    return {
      thisMonth,
      lastMonth,
      openDisputeCount,
      resolvedDisputeCount
    };
  });

  onMount(() => {
    if (role === "employee" && plantId) {
      void loadAll(plantId, data.auth.apiBearerToken);
    }
  });

  function exportCsv() {
    const rows: string[] = [];
    rows.push(["orderId", "deliveryDate", "status", "kind", "amountMinor", "currency", "occurredAt"].join(","));
    for (const order of orders) {
      const ledger = ledgersByOrderId[order.orderId];
      if (!ledger) continue;
      for (const entry of ledger.ledgerEntries) {
        rows.push(
          [
            order.orderId,
            order.deliveryDate,
            order.status,
            entry.kind,
            entry.amount.amountMinor,
            entry.amount.currency,
            entry.occurredAt
          ]
            .map((v) => `"${String(v).replace(/"/g, '""')}"`)
            .join(",")
        );
      }
    }
    const blob = new Blob([rows.join("\n")], { type: "text/csv;charset=utf-8" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = `wallet-${today}.csv`;
    a.click();
    URL.revokeObjectURL(url);
  }

  async function loadAll(resolvedPlantId: string, bearerToken: string | null) {
    loading = true;
    loadError = null;
    try {
      configureEmployeeApi(resolvedPlantId, bearerToken);
      // Fetch ~30 days window to cover "this month" and "last month" summary needs.
      const fromDate = addDaysIsoDate(today, -60);
      const page = await apiClient.employee.listEmployeeOrders(
        resolvedPlantId,
        fromDate,
        undefined,
        1,
        200,
        "deliveryDate",
        "desc"
      );
      orders = page.items;

      const ids = orders.flatMap((o) => o.lineItems.map((li) => li.menuItemId));
      menuItemNameMap = await loadMenuItemNameMap(resolvedPlantId, ids);

      // Fetch ledgers in parallel for all loaded orders so the summary reflects reality.
      const ledgerPairs = await Promise.all(
        orders.map(async (order) => {
          try {
            const ledger = await apiClient.employee.getEmployeeOrderPayrollLedger(order.orderId);
            return [order.orderId, ledger] as const;
          } catch {
            return null;
          }
        })
      );
      const next: Record<string, PayrollLedgerView> = {};
      for (const pair of ledgerPairs) {
        if (pair) next[pair[0]] = pair[1];
      }
      ledgersByOrderId = next;
    } catch (error) {
      loadError = describeApiError(error);
    } finally {
      loading = false;
    }
  }
</script>

<PlantGuard role={role} plantId={plantId}>
  <PageHeader
    eyebrow="薪資扣款"
    title="扣款總覽"
    description="點任一筆訂單查看扣款流水與申訴進度。"
    breadcrumbs={data.breadcrumbs}
  >
    {#snippet actions()}
      <Button variant="secondary" size="sm" onclick={exportCsv} disabled={orders.length === 0}>
        匯出 CSV
      </Button>
    {/snippet}
  </PageHeader>

  <Card title="帳務摘要" description="以配送日歸屬月份統計。">
    <div class="grid gap-3 md:grid-cols-4">
      <article class="rounded-xl border border-slate-200 bg-slate-50 p-3">
        <p class="text-xs text-slate-500">本月扣款</p>
        <MoneyAmount
          amountMinor={summary.thisMonth.amountMinor}
          currency={summary.thisMonth.currency}
        />
      </article>
      <article class="rounded-xl border border-slate-200 bg-slate-50 p-3">
        <p class="text-xs text-slate-500">上月扣款</p>
        <MoneyAmount
          amountMinor={summary.lastMonth.amountMinor}
          currency={summary.lastMonth.currency}
        />
      </article>
      <article class="rounded-xl border border-slate-200 bg-slate-50 p-3">
        <p class="text-xs text-slate-500">進行中申訴</p>
        <p class="mt-1 text-xl font-bold text-amber-700">{summary.openDisputeCount}</p>
      </article>
      <article class="rounded-xl border border-slate-200 bg-slate-50 p-3">
        <p class="text-xs text-slate-500">已結案申訴</p>
        <p class="mt-1 text-xl font-bold text-emerald-700">{summary.resolvedDisputeCount}</p>
      </article>
    </div>
  </Card>

  <Card title="訂單">
    {#if loading}
      <p class="text-sm text-slate-600">訂單載入中...</p>
    {:else if loadError}
      <p class="text-sm text-rose-700">{loadError}</p>
    {:else if orders.length === 0}
      <EmptyState title="尚無訂單" description="還沒下過單，先去菜單看看吧。">
        {#snippet actions()}
          <Button href="/employee/discover" variant="primary">瀏覽菜單</Button>
        {/snippet}
      </EmptyState>
    {:else}
      <div class="grid gap-2">
        {#each orders as order (order.orderId)}
          {@const itemSummary = summarizeOrderLineItems(order.lineItems, menuItemNameMap)}
          <a
            class="grid gap-1 rounded-lg border border-slate-200 bg-white px-3 py-2 text-left transition hover:border-cyan-500 hover:bg-cyan-50"
            href={`/employee/wallet/${order.orderId}`}
          >
            <div class="flex flex-wrap items-center justify-between gap-2">
              <span class="text-sm font-semibold text-slate-900">{itemSummary}</span>
              <StateTag
                label={friendlyOrderStatus(order.status)}
                tone={orderStatusTone(order.status)}
              />
            </div>
            <div class="flex flex-wrap items-center justify-between gap-2 text-xs text-slate-600">
              <span>配送日 {order.deliveryDate} · 訂單 {maskIdentifier(order.orderId)}</span>
              <MoneyAmount
                amountMinor={order.total.amountMinor}
                currency={order.total.currency}
              />
            </div>
          </a>
        {/each}
      </div>
    {/if}
  </Card>
</PlantGuard>
