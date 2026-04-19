<script lang="ts">
  import { onMount } from "svelte";

  import {
    PageHeader,
    Card,
    Button,
    EmptyState,
    StateTag,
    MoneyAmount,
    DateInput,
    FormField
  } from "$lib/components/ui";
  import PlantGuard from "$lib/components/employee/plant-guard.svelte";
  import {
    configureEmployeeApi,
    describeApiError,
    loadMenuItemNameMap,
    summarizeOrderLineItems,
    type EmployeeOrderView
  } from "$lib/employee/api";
  import { isPickupEligible } from "$lib/employee/portal";
  import { friendlyOrderStatus, orderStatusTone, maskIdentifier } from "$lib/platform/labels";
  import { apiClient } from "$lib/platform/api";

  import type { PageData } from "./$types";

  let { data }: { data: PageData } = $props();

  const actor = $derived(data.actor);
  const plantId = $derived(actor?.scope.plantIds[0] ?? null);
  const role = $derived(actor?.role ?? null);

  type SegmentKey = "ACTIVE" | "COMPLETED" | "DISPUTED";

  // Map segment → concrete backend status values we include in each bucket.
  const SEGMENT_STATUSES: Record<SegmentKey, string[]> = {
    ACTIVE: ["PENDING", "MODIFIED"],
    COMPLETED: ["FULFILLED", "REFUNDED", "CANCELLED"],
    DISPUTED: ["SOLD_OUT", "REFUND_PENDING"]
  };

  let segment = $state<SegmentKey>("ACTIVE");
  let fromDate = $state("");
  let toDate = $state("");
  let orders = $state<EmployeeOrderView[]>([]);
  let menuItemNameMap = $state<Record<string, string>>({});
  let loading = $state(false);
  let loadError = $state<string | null>(null);

  onMount(() => {
    if (role === "employee" && plantId) {
      void refresh(plantId, data.auth.apiBearerToken);
    }
  });

  async function refresh(resolvedPlantId: string, bearerToken: string | null) {
    loading = true;
    loadError = null;
    try {
      configureEmployeeApi(resolvedPlantId, bearerToken);
      // Server-side status filter only supports one status at a time; fetch all then filter client-side.
      const page = await apiClient.employee.listEmployeeOrders(
        resolvedPlantId,
        fromDate || undefined,
        toDate || undefined,
        1,
        200,
        "deliveryDate",
        "desc"
      );
      orders = page.items;
      const ids = orders.flatMap((o) => o.lineItems.map((li) => li.menuItemId));
      menuItemNameMap = await loadMenuItemNameMap(resolvedPlantId, ids);
    } catch (error) {
      loadError = describeApiError(error);
    } finally {
      loading = false;
    }
  }

  const filteredOrders = $derived(
    orders.filter((order) => SEGMENT_STATUSES[segment].includes(order.status))
  );

  function onFilterChange() {
    if (plantId) void refresh(plantId, data.auth.apiBearerToken);
  }
</script>

<PlantGuard role={role} plantId={plantId}>
  <PageHeader
    eyebrow="員工入口"
    title="我的訂單"
    description="列出所有預購中、已完成、有爭議的訂單。"
    breadcrumbs={data.breadcrumbs}
  />

  <Card title="篩選條件" description="切換狀態分群或調整配送日範圍，會即時更新結果。">
    <div class="grid gap-3 md:grid-cols-[1fr,auto,auto]">
      <div class="flex flex-wrap items-center gap-2" role="tablist" aria-label="訂單狀態分群">
        {#each [{ key: "ACTIVE", label: "進行中" }, { key: "COMPLETED", label: "已完成" }, { key: "DISPUTED", label: "有爭議" }] as option (option.key)}
          <button
            type="button"
            role="tab"
            aria-selected={segment === option.key}
            class={`rounded-full px-4 py-1.5 text-sm font-medium transition ${
              segment === option.key
                ? "bg-cyan-600 text-white shadow-sm"
                : "bg-slate-100 text-slate-700 hover:bg-slate-200"
            }`}
            onclick={() => (segment = option.key as SegmentKey)}
          >
            {option.label}
          </button>
        {/each}
      </div>
      <FormField label="起始配送日">
        <DateInput bind:value={fromDate} onchange={onFilterChange} />
      </FormField>
      <FormField label="結束配送日">
        <DateInput bind:value={toDate} onchange={onFilterChange} />
      </FormField>
    </div>
  </Card>

  {#if loading}
    <Card title="同步中">
      <p class="text-sm text-slate-600">訂單載入中...</p>
    </Card>
  {:else if loadError}
    <Card variant="danger" title="載入失敗">
      <p class="text-sm text-rose-900">{loadError}</p>
    </Card>
  {:else if filteredOrders.length === 0}
    <Card title="訂單">
      <EmptyState title="此分群沒有訂單" description="切換其他分群或前往菜單下單吧。">
        {#snippet actions()}
          <Button href="/employee/discover" variant="primary">瀏覽菜單 →</Button>
        {/snippet}
      </EmptyState>
    </Card>
  {:else}
    <Card title="訂單列表">
      <div class="grid gap-2">
        {#each filteredOrders as order (order.orderId)}
          {@const summary = summarizeOrderLineItems(order.lineItems, menuItemNameMap)}
          <article
            class="grid gap-2 rounded-xl border border-slate-200 bg-white p-3 shadow-sm md:grid-cols-[1fr,auto] md:items-center"
          >
            <div class="grid gap-1">
              <p class="text-xs text-slate-500">配送日 {order.deliveryDate} · 訂單 {maskIdentifier(order.orderId)}</p>
              <p class="text-sm font-semibold text-slate-900">{summary}</p>
              <div class="flex flex-wrap items-center gap-2">
                <StateTag
                  label={friendlyOrderStatus(order.status)}
                  tone={orderStatusTone(order.status)}
                />
                <MoneyAmount
                  amountMinor={order.total.amountMinor}
                  currency={order.total.currency}
                />
              </div>
            </div>
            <div class="flex flex-wrap gap-2 md:justify-end">
              <Button
                variant="ghost"
                size="sm"
                href={`/employee/orders/${order.orderId}`}
              >
                詳情
              </Button>
              {#if isPickupEligible(order.status)}
                <Button
                  variant="primary"
                  size="sm"
                  href={`/employee/orders/${order.orderId}/pickup`}
                >
                  領餐
                </Button>
              {/if}
            </div>
          </article>
        {/each}
      </div>
    </Card>
  {/if}
</PlantGuard>
