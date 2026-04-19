<script lang="ts">
  import { onMount, untrack } from "svelte";

  import { Button, Card, DataTable, FormField, PageHeader, StateTag, toasts } from "$lib/components/ui";
  import { zhTW } from "$lib/i18n/zh-tw";
  import { apiClient, ensureApiClientConfigured } from "$lib/platform/api";
  import { normalizeApiFailure } from "$lib/platform/api/failure";
  import {
    ORDER_STATUS_OPTIONS,
    addDaysIsoDate,
    orderStatusLabel,
    todayTaipeiIsoDate
  } from "$lib/vendor/helpers";
  import type { EmployeeOrderStatus } from "../../../../../../contract/generated/ts-client";

  import type { PageData } from "./$types";

  let { data }: { data: PageData } = $props();

  type OrderPage = Awaited<ReturnType<typeof apiClient.vendor.listVendorOrders>>;
  type Order = OrderPage["items"][number];

  const initialDate = todayTaipeiIsoDate();

  const plantOptions = $derived(data.actor?.scope.plantIds ?? []);
  let plantId = $state(untrack(() => data.actor?.scope.plantIds[0] ?? ""));
  let fromDate = $state(initialDate);
  let toDate = $state(addDaysIsoDate(initialDate, 7));
  let statusFilter = $state<"ALL" | EmployeeOrderStatus>("ALL");
  let orders = $state<Order[]>([]);
  let pageMeta = $state<OrderPage["page"] | null>(null);
  let loading = $state(false);
  let errorMessage = $state<string | null>(null);

  onMount(async () => {
    try {
      ensureApiClientConfigured(data.auth.apiBearerToken);
    } catch (error) {
      errorMessage = normalizeApiFailure(error).localizedMessage;
      return;
    }
    if (plantId) {
      await refresh();
    }
  });

  async function refresh() {
    const targetPlant = plantId.trim();
    if (!targetPlant) {
      toasts.error("請輸入 plantId。");
      return;
    }
    if (loading) return;
    loading = true;
    errorMessage = null;
    try {
      const result = await apiClient.vendor.listVendorOrders(
        targetPlant,
        fromDate,
        toDate,
        1,
        200,
        "deliveryDate",
        "asc",
        statusFilter === "ALL" ? undefined : statusFilter
      );
      orders = result.items;
      pageMeta = result.page;
    } catch (error) {
      const failure = normalizeApiFailure(error);
      errorMessage = failure.localizedMessage;
      toasts.error(failure.localizedMessage);
    } finally {
      loading = false;
    }
  }

  const columns = [
    { id: "orderId", label: "訂單" },
    { id: "plant", label: "廠區" },
    { id: "deliveryDate", label: "配送日" },
    { id: "status", label: "狀態" }
  ];
</script>

<PageHeader
  title={zhTW.vendor.orders.title}
  description={zhTW.vendor.orders.description}
  breadcrumbs={data.breadcrumbs}
/>

{#if errorMessage}
  <div class="mb-4 rounded-lg border border-rose-200 bg-rose-50 px-3 py-2 text-sm text-rose-900">
    {errorMessage}
  </div>
{/if}

<Card title="篩選條件">
  <div class="grid gap-3 md:grid-cols-5">
    <FormField label="廠區" required>
      {#if plantOptions.length > 0}
        <select class="rounded border border-slate-300 bg-white px-2 py-1.5" bind:value={plantId}>
          {#each plantOptions as option}
            <option value={option}>{option}</option>
          {/each}
        </select>
      {:else}
        <input class="rounded border border-slate-300 bg-white px-2 py-1.5" bind:value={plantId} />
      {/if}
    </FormField>
    <FormField label="起始日">
      <input type="date" class="rounded border border-slate-300 bg-white px-2 py-1.5" bind:value={fromDate} />
    </FormField>
    <FormField label="結束日">
      <input type="date" class="rounded border border-slate-300 bg-white px-2 py-1.5" bind:value={toDate} />
    </FormField>
    <FormField label={zhTW.vendor.orders.statusLabel}>
      <select class="rounded border border-slate-300 bg-white px-2 py-1.5" bind:value={statusFilter}>
        <option value="ALL">全部</option>
        {#each ORDER_STATUS_OPTIONS as status}
          <option value={status}>{orderStatusLabel(status)}</option>
        {/each}
      </select>
    </FormField>
    <div class="flex items-end">
      <Button variant="primary" onclick={refresh} loading={loading}>套用篩選</Button>
    </div>
  </div>
  <p class="text-xs text-slate-600">共 {pageMeta?.totalItems ?? 0} 筆</p>
</Card>

<section class="mt-4">
  <DataTable {columns} rows={orders} emptyLabel="沒有符合條件的訂單。">
    {#snippet row(order: Order)}
      <tr class="hover:bg-slate-50">
        <td class="px-3 py-2 font-mono text-xs">{order.orderId}</td>
        <td class="px-3 py-2">{order.plantId}</td>
        <td class="px-3 py-2 tabular-nums">{order.deliveryDate}</td>
        <td class="px-3 py-2"><StateTag label={orderStatusLabel(order.status)} /></td>
      </tr>
    {/snippet}
  </DataTable>
</section>
