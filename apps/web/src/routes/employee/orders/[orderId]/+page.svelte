<script lang="ts">
  import { onMount } from "svelte";

  import {
    PageHeader,
    Card,
    Button,
    StateTag,
    MoneyAmount,
    DataTable,
    EmptyState
  } from "$lib/components/ui";
  import PlantGuard from "$lib/components/employee/plant-guard.svelte";
  import {
    configureEmployeeApi,
    describeApiError,
    findEmployeeOrderById,
    loadMenuItemNameMap,
    summarizeOrderLineItems,
    type EmployeeOrderView
  } from "$lib/employee/api";
  import { isEmployeeOrderEditable, isPickupEligible } from "$lib/employee/portal";
  import {
    friendlyOrderStatus,
    orderStatusTone,
    friendlyOrderEvent,
    maskIdentifier
  } from "$lib/platform/labels";

  import type { PageData } from "./$types";

  let { data }: { data: PageData } = $props();

  const actor = $derived(data.actor);
  const plantId = $derived(actor?.scope.plantIds[0] ?? null);
  const role = $derived(actor?.role ?? null);
  const orderId = $derived(data.orderId);

  let order = $state<EmployeeOrderView | null>(null);
  let menuItemNameMap = $state<Record<string, string>>({});
  let loading = $state(true);
  let loadError = $state<string | null>(null);

  const lineItemColumns = [
    { id: "menuItemName", label: "品項" },
    { id: "quantity", label: "數量" },
    { id: "price", label: "單價" },
    { id: "subtotal", label: "小計" }
  ];

  const headerTitle = $derived(
    order ? `${order.deliveryDate} · ${summarizeOrderLineItems(order.lineItems, menuItemNameMap)}` : orderId
  );

  onMount(() => {
    if (role === "employee" && plantId) {
      void loadOrder(plantId, data.auth.apiBearerToken);
    }
  });

  async function loadOrder(resolvedPlantId: string, bearerToken: string | null) {
    loading = true;
    loadError = null;
    try {
      configureEmployeeApi(resolvedPlantId, bearerToken);
      const found = await findEmployeeOrderById(orderId, { plantId: resolvedPlantId });
      order = found;
      if (!found) {
        loadError = `找不到訂單 ${orderId}。可能已超過 2000 筆查詢範圍，或此訂單不屬於你的廠區。`;
        return;
      }
      const ids = found.lineItems.map((li) => li.menuItemId);
      menuItemNameMap = await loadMenuItemNameMap(resolvedPlantId, ids);
    } catch (error) {
      loadError = describeApiError(error);
    } finally {
      loading = false;
    }
  }

  function menuItemLabel(menuItemId: string): string {
    const name = menuItemNameMap[menuItemId];
    if (name) return name;
    return `品項 ${maskIdentifier(menuItemId)}`;
  }
</script>

<PlantGuard role={role} plantId={plantId}>
  <PageHeader
    eyebrow={`訂單 ${maskIdentifier(orderId)}`}
    title={headerTitle}
    description="檢視品項、時間軸與可進行的動作。"
    breadcrumbs={data.breadcrumbs}
  >
    {#snippet actions()}
      <Button href="/employee/orders" variant="ghost">返回列表</Button>
    {/snippet}
  </PageHeader>

  {#if loading}
    <Card title="同步中">
      <p class="text-sm text-slate-600">訂單載入中...</p>
    </Card>
  {:else if loadError || !order}
    <Card variant="danger" title="載入失敗">
      <p class="text-sm text-rose-900">{loadError ?? "訂單不存在"}</p>
    </Card>
  {:else}
    <Card title="訂單概要">
      <div class="grid gap-2 md:grid-cols-4">
        <div>
          <p class="text-xs text-slate-500">狀態</p>
          <StateTag
            label={friendlyOrderStatus(order.status)}
            tone={orderStatusTone(order.status)}
          />
        </div>
        <div>
          <p class="text-xs text-slate-500">配送日</p>
          <p class="text-sm font-semibold text-slate-900">{order.deliveryDate}</p>
        </div>
        <div>
          <p class="text-xs text-slate-500">建立時間</p>
          <p class="text-sm text-slate-700">{order.createdAt ?? "-"}</p>
        </div>
        <div>
          <p class="text-xs text-slate-500">訂單金額</p>
          <MoneyAmount
            amountMinor={order.total.amountMinor}
            currency={order.total.currency}
          />
        </div>
      </div>
      <p class="mt-2 text-xs text-slate-500">訂單編號：{order.orderId}</p>
    </Card>

    <Card title="品項">
      <DataTable columns={lineItemColumns} rows={order.lineItems} emptyLabel="沒有品項">
        {#snippet row(item: EmployeeOrderView["lineItems"][number])}
          <tr>
            <td class="px-3 py-2 font-medium text-slate-900">
              {menuItemLabel(item.menuItemId)}
              {#if !menuItemNameMap[item.menuItemId]}
                <span class="ml-1 text-xs font-normal text-slate-400">(menuItemId: {maskIdentifier(item.menuItemId)})</span>
              {/if}
            </td>
            <td class="px-3 py-2 text-slate-700">{item.quantity}</td>
            <td class="px-3 py-2">
              <MoneyAmount
                amountMinor={item.pricePerUnit.amountMinor}
                currency={item.pricePerUnit.currency}
              />
            </td>
            <td class="px-3 py-2">
              <MoneyAmount
                amountMinor={item.pricePerUnit.amountMinor * item.quantity}
                currency={item.pricePerUnit.currency}
              />
            </td>
          </tr>
        {/snippet}
      </DataTable>
    </Card>

    <Card title="時間軸">
      {#if order.timeline.length === 0}
        <EmptyState title="尚無事件" description="訂單產生後會自動記錄狀態變化。" />
      {:else}
        <ol class="grid gap-2">
          {#each order.timeline as event, index (`${event.eventType}-${index}`)}
            <li class="grid gap-1 rounded-lg border border-slate-200 bg-slate-50 p-3">
              <div class="flex flex-wrap items-center justify-between gap-2">
                <p class="text-sm font-semibold text-slate-900">{friendlyOrderEvent(event.eventType)}</p>
                <StateTag
                  label={friendlyOrderStatus(event.status)}
                  tone={orderStatusTone(event.status)}
                />
              </div>
              <p class="text-xs text-slate-500">{event.occurredAt}</p>
            </li>
          {/each}
        </ol>
      {/if}
    </Card>

    <Card title="可用動作">
      <div class="flex flex-wrap gap-2">
        {#if isEmployeeOrderEditable(order.status)}
          <Button
            href={`/employee/orders/${order.orderId}/edit`}
            variant="primary"
          >
            修改訂單
          </Button>
          <Button
            href={`/employee/orders/${order.orderId}/cancel`}
            variant="danger"
          >
            取消訂單
          </Button>
        {/if}
        {#if isPickupEligible(order.status)}
          <Button
            href={`/employee/orders/${order.orderId}/pickup`}
            variant="primary"
          >
            顯示領餐 QR
          </Button>
        {/if}
        <Button
          href={`/employee/orders/${order.orderId}/dispute`}
          variant="secondary"
        >
          提交申訴
        </Button>
      </div>
    </Card>
  {/if}
</PlantGuard>
