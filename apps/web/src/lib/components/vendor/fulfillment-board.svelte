<script lang="ts">
  import { onMount, untrack } from "svelte";

  import { Button, Card, FormField, StateTag, toasts } from "$lib/components/ui";
  import { apiClient, ensureApiClientConfigured } from "$lib/platform/api";
  import { normalizeApiFailure } from "$lib/platform/api/failure";
  import {
    friendlyDeliveryStatus,
    deliveryStatusTone,
    maskIdentifier
  } from "$lib/platform/labels";
  import {
    DELIVERY_STATUS_OPTIONS,
    currentTaipeiContractDateTime,
    formatTaipeiDateTime,
    nextDeliveryStatus,
    orderStatusLabel,
    todayTaipeiIsoDate
  } from "$lib/vendor/helpers";
  import type { VendorFulfillmentDeliveryStatus } from "../../../../../../contract/generated/ts-client";

  interface Props {
    apiBearerToken: string | null;
    fixedPlantId: string | null;
    initialPlantId: string | null;
  }

  let { apiBearerToken, fixedPlantId, initialPlantId }: Props = $props();

  type FulfillmentBoard = Awaited<
    ReturnType<typeof apiClient.vendor.listVendorFulfillmentBoard>
  >;
  type Order = FulfillmentBoard["orders"][number];

  let board = $state<FulfillmentBoard | null>(null);
  let loading = $state(false);
  let errorMessage = $state<string | null>(null);

  let deliveryDate = $state(todayTaipeiIsoDate());
  let plantIdFilter = $state(untrack(() => fixedPlantId ?? initialPlantId ?? ""));
  let includeAudit = $state(false);

  let viewMode = $state<"kanban" | "list" | "audit">("kanban");
  let submittingByOrderId = $state<Record<string, boolean>>({});

  const KANBAN_STATUSES: readonly VendorFulfillmentDeliveryStatus[] = [
    "PENDING_PREP",
    "PREPARING",
    "PACKED",
    "OUT_FOR_DELIVERY",
    "DELIVERED",
    "CANCELLED"
  ];

  const portionCount = $derived.by(() => {
    if (!board) return 0;
    return board.orders.reduce(
      (total, order) =>
        total + order.lineItems.reduce((sum, line) => sum + line.quantity, 0),
      0
    );
  });

  const ordersByStatus = $derived.by(() => {
    const grouped: Record<VendorFulfillmentDeliveryStatus, Order[]> = {
      PENDING_PREP: [],
      PREPARING: [],
      PACKED: [],
      OUT_FOR_DELIVERY: [],
      DELIVERED: [],
      CANCELLED: []
    };
    if (!board) return grouped;
    for (const order of board.orders) {
      grouped[order.deliveryStatus as VendorFulfillmentDeliveryStatus].push(order);
    }
    return grouped;
  });

  onMount(async () => {
    try {
      ensureApiClientConfigured(apiBearerToken);
    } catch (error) {
      errorMessage = normalizeApiFailure(error).localizedMessage;
      return;
    }
    await refresh();
  });

  async function refresh() {
    if (loading) return;
    loading = true;
    errorMessage = null;
    try {
      const plantForQuery = fixedPlantId ?? (plantIdFilter.trim() || undefined);
      const result = await apiClient.vendor.listVendorFulfillmentBoard(
        deliveryDate,
        plantForQuery,
        includeAudit || viewMode === "audit"
      );
      board = result;
    } catch (error) {
      const failure = normalizeApiFailure(error);
      errorMessage = failure.localizedMessage;
      toasts.error(failure.localizedMessage);
    } finally {
      loading = false;
    }
  }

  async function advanceOrder(order: Order) {
    const current = order.deliveryStatus as VendorFulfillmentDeliveryStatus;
    const next = nextDeliveryStatus(current) as VendorFulfillmentDeliveryStatus;
    if (next === current) {
      toasts.info(`訂單 ${maskIdentifier(order.orderId)} 已是終態。`);
      return;
    }
    if (submittingByOrderId[order.orderId]) return;
    submittingByOrderId = { ...submittingByOrderId, [order.orderId]: true };
    try {
      const result = await apiClient.vendor.advanceVendorFulfillmentDeliveryStatus(order.orderId, {
        toStatus: next,
        occurredAt: currentTaipeiContractDateTime()
      });
      toasts.success(
        `訂單 ${maskIdentifier(result.orderId)}：${friendlyDeliveryStatus(result.fromStatus)} → ${friendlyDeliveryStatus(result.toStatus)}`
      );
      await refresh();
    } catch (error) {
      toasts.error(normalizeApiFailure(error).localizedMessage);
    } finally {
      submittingByOrderId = { ...submittingByOrderId, [order.orderId]: false };
    }
  }

  function isTerminal(status: VendorFulfillmentDeliveryStatus): boolean {
    return nextDeliveryStatus(status) === status;
  }

  function switchView(mode: "kanban" | "list" | "audit") {
    const wasAudit = viewMode === "audit";
    viewMode = mode;
    if (mode === "audit" && !wasAudit) {
      // need audit data now
      void refresh();
    }
  }
</script>

{#if errorMessage}
  <div class="mb-4 rounded-lg border border-rose-200 bg-rose-50 px-3 py-2 text-sm text-rose-900">
    {errorMessage}
  </div>
{/if}

<Card title="篩選條件">
  <div class="grid gap-3 md:grid-cols-4">
    <FormField label="配送日">
      <input type="date" class="rounded border border-slate-300 bg-white px-2 py-1.5" bind:value={deliveryDate} />
    </FormField>
    <FormField label="廠區">
      {#if fixedPlantId}
        <input
          class="rounded border border-slate-300 bg-slate-100 px-2 py-1.5 text-slate-500"
          value={fixedPlantId}
          readonly
        />
      {:else}
        <input
          class="rounded border border-slate-300 bg-white px-2 py-1.5"
          placeholder="留空代表全部廠區"
          bind:value={plantIdFilter}
        />
      {/if}
    </FormField>
    <div class="flex items-end md:col-span-2 md:justify-end">
      <Button variant="primary" onclick={refresh} loading={loading}>套用篩選</Button>
    </div>
  </div>
</Card>

<section class="mt-4 grid gap-3 md:grid-cols-3">
  <Card title="配送日">
    <p class="text-xl font-bold text-slate-900">{board?.deliveryDate ?? "-"}</p>
  </Card>
  <Card title="履約訂單數">
    <p class="text-xl font-bold text-slate-900 tabular-nums">{board?.orders.length ?? 0}</p>
  </Card>
  <Card title="份數總量">
    <p class="text-xl font-bold text-slate-900 tabular-nums">{portionCount}</p>
  </Card>
</section>

<div class="mt-4 flex flex-wrap gap-2">
  {#each [
    { mode: "kanban" as const, label: "看板模式" },
    { mode: "list" as const, label: "列表模式" },
    { mode: "audit" as const, label: "查看稽核軌跡" }
  ] as tab}
    <button
      type="button"
      class={`rounded-full border px-3 py-1.5 text-sm font-semibold transition ${
        viewMode === tab.mode
          ? "border-cyan-700 bg-cyan-700 text-white"
          : "border-slate-300 bg-white text-slate-700 hover:border-cyan-600 hover:text-cyan-800"
      }`}
      onclick={() => switchView(tab.mode)}
    >
      {tab.label}
    </button>
  {/each}
</div>

{#if viewMode === "kanban"}
  <section class="mt-3 overflow-x-auto">
    <div class="grid grid-flow-col auto-cols-[minmax(220px,1fr)] gap-3 min-w-max">
      {#each KANBAN_STATUSES as status}
        {@const ordersInColumn = ordersByStatus[status]}
        <div class="flex flex-col gap-2 rounded-xl border border-slate-200 bg-slate-50/60 p-3">
          <header class="flex items-center justify-between gap-2">
            <h4 class="text-sm font-semibold text-slate-800">
              {friendlyDeliveryStatus(status)}
            </h4>
            <span class="inline-flex min-w-6 items-center justify-center rounded-full bg-white px-2 py-0.5 text-xs font-semibold text-slate-700 ring-1 ring-slate-300 tabular-nums">
              {ordersInColumn.length}
            </span>
          </header>

          {#if ordersInColumn.length === 0}
            <p class="text-xs text-slate-500">—</p>
          {:else}
            {#each ordersInColumn as order}
              {@const portions = order.lineItems.reduce((sum, li) => sum + li.quantity, 0)}
              {@const specials = order.lineItems.flatMap((li) => li.specialRequests)}
              {@const terminal = isTerminal(status)}
              {@const nextStatus = nextDeliveryStatus(status)}
              <article class="grid gap-2 rounded-lg border border-slate-200 bg-white p-2.5 shadow-sm">
                <div class="flex items-center justify-between gap-2">
                  <span class="font-mono text-xs font-semibold text-slate-800">
                    {maskIdentifier(order.orderId)}
                  </span>
                  <span class="text-xs text-slate-600">{order.plantId}</span>
                </div>
                <div class="flex items-center justify-between text-xs text-slate-700">
                  <span>份數 <span class="font-semibold tabular-nums">{portions}</span></span>
                  <StateTag
                    label={friendlyDeliveryStatus(order.deliveryStatus)}
                    tone={deliveryStatusTone(order.deliveryStatus)}
                  />
                </div>
                {#if specials.length > 0}
                  <div class="flex flex-wrap gap-1">
                    {#each specials as sr}
                      <span class="rounded-full bg-amber-100 px-2 py-0.5 text-[11px] font-medium text-amber-900">
                        {sr}
                      </span>
                    {/each}
                  </div>
                {/if}
                {#if terminal}
                  <Button size="sm" variant="ghost" disabled>終態</Button>
                {:else}
                  <Button
                    size="sm"
                    variant="primary"
                    fullWidth
                    loading={submittingByOrderId[order.orderId] === true}
                    onclick={() => advanceOrder(order)}
                  >
                    → {friendlyDeliveryStatus(nextStatus)}
                  </Button>
                {/if}
              </article>
            {/each}
          {/if}
        </div>
      {/each}
    </div>
  </section>
{:else if viewMode === "list"}
  <div class="mt-3 grid gap-4">
    {#if !fixedPlantId}
      <Card title="廠區彙總">
        <div class="overflow-x-auto rounded-lg border border-slate-200">
          <table class="min-w-full text-sm">
            <thead class="bg-slate-50 text-left text-xs font-semibold tracking-wide text-slate-600">
              <tr>
                <th class="px-3 py-2">廠區</th>
                <th class="px-3 py-2">訂單</th>
                <th class="px-3 py-2">份數</th>
                <th class="px-3 py-2">配送狀態分布</th>
                <th class="px-3 py-2">特殊需求</th>
              </tr>
            </thead>
            <tbody>
              {#if !board || board.plants.length === 0}
                <tr>
                  <td class="px-3 py-4 text-slate-500" colspan="5">尚無履約匯總資料。</td>
                </tr>
              {:else}
                {#each board.plants as plant}
                  <tr class="border-t border-slate-100">
                    <td class="px-3 py-2 font-medium">{plant.plantId}</td>
                    <td class="px-3 py-2 tabular-nums">{plant.orderCount}</td>
                    <td class="px-3 py-2 tabular-nums">{plant.portionCount}</td>
                    <td class="px-3 py-2">
                      <div class="flex flex-wrap gap-1">
                        {#each plant.deliveryStatusCounts as entry}
                          <StateTag
                            label={`${friendlyDeliveryStatus(entry.status)} ${entry.count}`}
                            tone={deliveryStatusTone(entry.status)}
                          />
                        {/each}
                      </div>
                    </td>
                    <td class="px-3 py-2">
                      <div class="flex flex-wrap gap-1 text-xs">
                        {#each plant.specialRequestCounts as entry}
                          <span class="rounded-full border border-slate-300 bg-white px-2 py-0.5">
                            {entry.specialRequest} {entry.count}
                          </span>
                        {/each}
                      </div>
                    </td>
                  </tr>
                {/each}
              {/if}
            </tbody>
          </table>
        </div>
      </Card>
    {/if}

    <Card title="訂單詳情">
      <div class="overflow-x-auto rounded-lg border border-slate-200">
        <table class="min-w-full text-sm">
          <thead class="bg-slate-50 text-left text-xs font-semibold tracking-wide text-slate-600">
            <tr>
              <th class="px-3 py-2">訂單</th>
              <th class="px-3 py-2">廠區</th>
              <th class="px-3 py-2">流程狀態</th>
              <th class="px-3 py-2">配送狀態</th>
              <th class="px-3 py-2">餐點項目</th>
              <th class="px-3 py-2">下一步</th>
            </tr>
          </thead>
          <tbody>
            {#if !board || board.orders.length === 0}
              <tr>
                <td class="px-3 py-4 text-slate-500" colspan="6">沒有符合篩選的履約訂單。</td>
              </tr>
            {:else}
              {#each board.orders as order}
                {@const status = order.deliveryStatus as VendorFulfillmentDeliveryStatus}
                {@const terminal = isTerminal(status)}
                {@const nextStatus = nextDeliveryStatus(status)}
                <tr class="border-t border-slate-100">
                  <td class="px-3 py-2 align-top font-mono text-xs font-semibold text-slate-900">
                    {maskIdentifier(order.orderId)}
                  </td>
                  <td class="px-3 py-2 align-top">{order.plantId}</td>
                  <td class="px-3 py-2 align-top">{orderStatusLabel(order.orderStatus)}</td>
                  <td class="px-3 py-2 align-top">
                    <StateTag
                      label={friendlyDeliveryStatus(order.deliveryStatus)}
                      tone={deliveryStatusTone(order.deliveryStatus)}
                    />
                  </td>
                  <td class="px-3 py-2 align-top">
                    <ul class="grid gap-1 text-xs text-slate-700">
                      {#each order.lineItems as lineItem}
                        <li>
                          {lineItem.menuItemId} x{lineItem.quantity}
                          {#if lineItem.specialRequests.length > 0}
                            ({lineItem.specialRequests.join(", ")})
                          {/if}
                        </li>
                      {/each}
                    </ul>
                  </td>
                  <td class="px-3 py-2 align-top">
                    {#if terminal}
                      <span class="text-xs text-slate-500">終態</span>
                    {:else}
                      <Button
                        size="sm"
                        variant="primary"
                        loading={submittingByOrderId[order.orderId] === true}
                        onclick={() => advanceOrder(order)}
                      >
                        → {friendlyDeliveryStatus(nextStatus)}
                      </Button>
                    {/if}
                  </td>
                </tr>
              {/each}
            {/if}
          </tbody>
        </table>
      </div>
    </Card>
  </div>
{:else if viewMode === "audit"}
  <Card title="配送狀態稽核軌跡">
    <div class="overflow-x-auto rounded-lg border border-slate-200">
      <table class="min-w-full text-xs">
        <thead class="bg-slate-50 text-left font-semibold tracking-wide text-slate-600">
          <tr>
            <th class="px-3 py-2">時間</th>
            <th class="px-3 py-2">訂單</th>
            <th class="px-3 py-2">操作者</th>
            <th class="px-3 py-2">操作</th>
            <th class="px-3 py-2">狀態變更</th>
          </tr>
        </thead>
        <tbody>
          {#if !board || board.statusTransitions.length === 0}
            <tr>
              <td class="px-3 py-3 text-slate-500" colspan="5">目前沒有配送狀態稽核紀錄。</td>
            </tr>
          {:else}
            {#each board.statusTransitions as entry}
              <tr class="border-t border-slate-100">
                <td class="px-3 py-2">{formatTaipeiDateTime(entry.occurredAt)}</td>
                <td class="px-3 py-2 font-mono">{maskIdentifier(entry.orderId)}</td>
                <td class="px-3 py-2">{entry.actorId} ({entry.actorRole})</td>
                <td class="px-3 py-2">{entry.operationId}</td>
                <td class="px-3 py-2">
                  {friendlyDeliveryStatus(entry.fromStatus)} → {friendlyDeliveryStatus(entry.toStatus)}
                </td>
              </tr>
            {/each}
          {/if}
        </tbody>
      </table>
    </div>
  </Card>
{/if}
