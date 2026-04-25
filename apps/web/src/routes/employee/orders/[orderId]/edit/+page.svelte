<script lang="ts">
  import { onMount } from "svelte";
  import { goto } from "$app/navigation";

  import {
    PageHeader,
    Card,
    Button,
    MoneyAmount,
    FormField,
    Stepper,
    toasts
  } from "$lib/components/ui";
  import PlantGuard from "$lib/components/employee/plant-guard.svelte";
  import {
    configureEmployeeApi,
    describeApiError,
    findEmployeeOrderById,
    loadMenuItemNameMap,
    type EmployeeOrderView
  } from "$lib/employee/api";
  import { isEmployeeOrderEditable } from "$lib/employee/portal";
  import { friendlyOrderStatus, maskIdentifier } from "$lib/platform/labels";
  import { apiClient } from "$lib/platform/api";

  import type { PageData } from "./$types";

  let { data }: { data: PageData } = $props();

  const actor = $derived(data.actor);
  const plantId = $derived(actor?.scope.plantIds[0] ?? null);
  const role = $derived(actor?.role ?? null);
  const orderId = $derived(data.orderId);

  let order = $state<EmployeeOrderView | null>(null);
  let menuItemNameMap = $state<Record<string, string>>({});
  let quantities = $state<Record<string, number>>({});
  let loading = $state(true);
  let loadError = $state<string | null>(null);
  let submitting = $state(false);

  const originalTotalMinor = $derived(
    order ? order.lineItems.reduce((sum, li) => sum + li.pricePerUnit.amountMinor * li.quantity, 0) : 0
  );
  const newTotalMinor = $derived(
    order
      ? order.lineItems.reduce(
          (sum, li) =>
            sum + li.pricePerUnit.amountMinor * (quantities[li.menuItemId] ?? li.quantity),
          0
        )
      : 0
  );
  const diffMinor = $derived(newTotalMinor - originalTotalMinor);

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
      const initial: Record<string, number> = {};
      for (const lineItem of found.lineItems) {
        initial[lineItem.menuItemId] = lineItem.quantity;
      }
      quantities = initial;
      const ids = found.lineItems.map((li) => li.menuItemId);
      menuItemNameMap = await loadMenuItemNameMap(resolvedPlantId, ids);
    } catch (error) {
      loadError = describeApiError(error);
    } finally {
      loading = false;
    }
  }

  function setQuantity(menuItemId: string, next: number) {
    quantities = { ...quantities, [menuItemId]: Math.max(1, Math.min(20, next)) };
  }

  async function submit() {
    if (!order) return;
    if (!isEmployeeOrderEditable(order.status)) {
      toasts.error("此訂單狀態不可修改。");
      return;
    }
    const lineItems = order.lineItems.map((lineItem) => ({
      menuItemId: lineItem.menuItemId,
      quantity: Math.max(1, Math.min(20, quantities[lineItem.menuItemId] ?? lineItem.quantity))
    }));
    if (lineItems.some((lineItem) => lineItem.quantity < 1)) {
      toasts.error("修改後的訂單數量至少要為 1。");
      return;
    }
    submitting = true;
    try {
      await apiClient.employee.updateEmployeeOrder(order.orderId, {
        operation: "REPLACE_LINE_ITEMS",
        lineItems
      });
      toasts.success(`訂單已更新。`);
      await goto(`/employee/orders/${order.orderId}`);
    } catch (error) {
      toasts.error(describeApiError(error));
    } finally {
      submitting = false;
    }
  }

  function menuItemLabel(menuItemId: string): string {
    return menuItemNameMap[menuItemId] ?? `品項 ${maskIdentifier(menuItemId)}`;
  }
</script>

<PlantGuard role={role} plantId={plantId}>
  <PageHeader
    eyebrow={`修改訂單 ${maskIdentifier(orderId)}`}
    title="調整品項數量"
    description="截單前可修改數量。每品項 1–20 份。"
    breadcrumbs={data.breadcrumbs}
  >
    {#snippet actions()}
      <Button href={`/employee/orders/${orderId}`} variant="ghost">取消</Button>
    {/snippet}
  </PageHeader>

  {#if loading}
    <Card title="同步中">
      <p class="text-sm text-slate-600">訂單載入中...</p>
    </Card>
  {:else if loadError || !order}
    <Card variant="danger" title="無法修改">
      <p class="text-sm text-rose-900">{loadError ?? "訂單不存在"}</p>
    </Card>
  {:else if !isEmployeeOrderEditable(order.status)}
    <Card variant="warning" title="目前狀態不可修改">
      <p class="text-sm text-slate-700">
        訂單狀態為 {friendlyOrderStatus(order.status)}，只有待處理 / 已修改的訂單可再次調整。
      </p>
      <div>
        <Button href={`/employee/orders/${order.orderId}`} variant="secondary">返回訂單詳情</Button>
      </div>
    </Card>
  {:else}
    <Card title="品項數量">
      <div class="grid gap-3">
        {#each order.lineItems as lineItem (lineItem.menuItemId)}
          <div class="grid gap-2 rounded-lg border border-slate-200 bg-slate-50 p-3 md:grid-cols-[2fr,auto,auto] md:items-center">
            <div>
              <p class="text-sm font-semibold text-slate-900">{menuItemLabel(lineItem.menuItemId)}</p>
              <p class="text-xs text-slate-600">
                單價
                <MoneyAmount
                  amountMinor={lineItem.pricePerUnit.amountMinor}
                  currency={lineItem.pricePerUnit.currency}
                />
              </p>
            </div>
            <FormField label="數量（1–20）" for={`qty-${lineItem.menuItemId}`}>
              <Stepper
                value={quantities[lineItem.menuItemId] ?? lineItem.quantity}
                min={1}
                max={20}
                onchange={(next) => setQuantity(lineItem.menuItemId, next)}
                aria-label={`${menuItemLabel(lineItem.menuItemId)} 數量`}
              />
            </FormField>
            <div class="text-right">
              <p class="text-xs text-slate-500">小計</p>
              <MoneyAmount
                amountMinor={lineItem.pricePerUnit.amountMinor *
                  (quantities[lineItem.menuItemId] ?? lineItem.quantity)}
                currency={lineItem.pricePerUnit.currency}
              />
            </div>
          </div>
        {/each}
      </div>
      <div class="mt-3 grid gap-1 rounded-lg border border-slate-200 bg-white p-3 text-sm">
        <div class="flex flex-wrap items-center justify-between gap-2">
          <span class="text-slate-600">原金額</span>
          <MoneyAmount amountMinor={originalTotalMinor} currency={order.total.currency} />
        </div>
        <div class="flex flex-wrap items-center justify-between gap-2">
          <span class="text-slate-600">新金額</span>
          <MoneyAmount amountMinor={newTotalMinor} currency={order.total.currency} />
        </div>
        <div class="flex flex-wrap items-center justify-between gap-2 border-t border-slate-200 pt-1 font-semibold">
          <span class="text-slate-900">變動</span>
          <span class="inline-flex items-center gap-1">
            <span class={diffMinor > 0 ? "text-rose-700" : diffMinor < 0 ? "text-emerald-700" : "text-slate-700"}>
              {diffMinor > 0 ? "+" : diffMinor < 0 ? "−" : ""}
            </span>
            <MoneyAmount amountMinor={Math.abs(diffMinor)} currency={order.total.currency} />
          </span>
        </div>
      </div>
      <div class="flex flex-wrap gap-2">
        <Button variant="primary" onclick={submit} loading={submitting}>送出修改</Button>
        <Button href={`/employee/orders/${order.orderId}`} variant="ghost">
          取消並返回
        </Button>
      </div>
    </Card>
  {/if}
</PlantGuard>
