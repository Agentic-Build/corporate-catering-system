<script lang="ts">
  import { onMount } from "svelte";

  import { Button, Card, PageHeader, StateTag, toasts } from "$lib/components/ui";
  import { zhTW } from "$lib/i18n/zh-tw";
  import { apiClient, ensureApiClientConfigured } from "$lib/platform/api";
  import { normalizeApiFailure } from "$lib/platform/api/failure";
  import { minuteOfDayToTime } from "$lib/platform/time-formats";
  import {
    deliveryStatusLabel,
    todayTaipeiIsoDate
  } from "$lib/vendor/helpers";

  import type { PageData } from "./$types";

  let { data }: { data: PageData } = $props();

  const today = todayTaipeiIsoDate();
  const plantId = $derived(data.actor?.scope.plantIds[0] ?? null);

  type FulfillmentBoard = Awaited<
    ReturnType<typeof apiClient.vendor.listVendorFulfillmentBoard>
  >;
  type OrderingPolicy = Awaited<ReturnType<typeof apiClient.vendor.getVendorOrderingPolicy>>;

  let board = $state<FulfillmentBoard | null>(null);
  let policy = $state<OrderingPolicy | null>(null);
  let loading = $state(true);
  let errorMessage = $state<string | null>(null);

  const counts = $derived.by(() => {
    const result = { toPrepare: 0, toDeliver: 0, delivered: 0, portions: 0, cancelled: 0 };
    if (!board) return result;
    for (const order of board.orders) {
      const portions = order.lineItems.reduce((sum, li) => sum + li.quantity, 0);
      result.portions += portions;
      switch (order.deliveryStatus) {
        case "PENDING_PREP":
        case "PREPARING":
          result.toPrepare += portions;
          break;
        case "PACKED":
        case "OUT_FOR_DELIVERY":
          result.toDeliver += portions;
          break;
        case "DELIVERED":
          result.delivered += portions;
          break;
        case "CANCELLED":
          result.cancelled += portions;
          break;
      }
    }
    return result;
  });

  const upcomingByPlant = $derived.by(() => {
    if (!board) return [];
    return board.plants
      .map((plant) => {
        const remaining = plant.deliveryStatusCounts
          .filter((entry) =>
            entry.status === "PENDING_PREP" || entry.status === "PREPARING"
          )
          .reduce((sum, entry) => sum + entry.count, 0);
        return { plantId: plant.plantId, remaining };
      })
      .filter((entry) => entry.remaining > 0);
  });

  onMount(async () => {
    if (!plantId) {
      errorMessage = "目前登入帳號沒有可用的廠區範圍，無法載入商家資料。";
      loading = false;
      return;
    }
    try {
      ensureApiClientConfigured(data.auth.apiBearerToken);
    } catch (error) {
      errorMessage = normalizeApiFailure(error).localizedMessage;
      loading = false;
      return;
    }

    try {
      const [boardResult, policyResult] = await Promise.all([
        apiClient.vendor.listVendorFulfillmentBoard(today, plantId, false),
        apiClient.vendor.getVendorOrderingPolicy()
      ]);
      board = boardResult;
      policy = policyResult;
    } catch (error) {
      const failure = normalizeApiFailure(error);
      errorMessage = failure.localizedMessage;
      toasts.error(failure.localizedMessage);
    } finally {
      loading = false;
    }
  });
</script>

<PageHeader
  title={zhTW.vendor.today.title}
  description={zhTW.vendor.today.description}
  eyebrow={`${today}｜${data.actor?.displayName ?? ""}`}
  breadcrumbs={data.breadcrumbs}
>
  {#snippet actions()}
    <Button href="/vendor/today" variant="primary">{zhTW.vendor.today.actions.openBoard}</Button>
    <Button href="/vendor/batches/new">{zhTW.vendor.today.actions.createBatch}</Button>
    <Button href="/vendor/menu" variant="ghost">{zhTW.vendor.today.actions.updateMenu}</Button>
  {/snippet}
</PageHeader>

{#if errorMessage}
  <div class="mb-4 rounded-lg border border-rose-200 bg-rose-50 px-3 py-2 text-sm text-rose-900">
    {errorMessage}
  </div>
{/if}

<section class="grid gap-4">
  <div class="grid gap-3 md:grid-cols-3">
    <Card title={zhTW.vendor.today.summary.toPrepare}>
      <p class="text-3xl font-bold text-slate-900 tabular-nums">
        {loading ? "-" : counts.toPrepare}
      </p>
      <p class="text-xs text-slate-600">備餐中的份數（含待備餐與備餐中）</p>
    </Card>
    <Card title={zhTW.vendor.today.summary.toDeliver}>
      <p class="text-3xl font-bold text-slate-900 tabular-nums">
        {loading ? "-" : counts.toDeliver}
      </p>
      <p class="text-xs text-slate-600">已打包與配送中的份數</p>
    </Card>
    <Card title={zhTW.vendor.today.summary.delivered} variant="success">
      <p class="text-3xl font-bold text-slate-900 tabular-nums">
        {loading ? "-" : counts.delivered}
      </p>
      <p class="text-xs text-slate-600">{counts.cancelled > 0 ? `（已取消 ${counts.cancelled}）` : "今日送達總份數"}</p>
    </Card>
  </div>

  <div class="grid gap-4 md:grid-cols-2">
    <Card title={zhTW.vendor.today.upcomingCutoff} variant="warning">
      {#if loading}
        <p class="text-sm text-slate-600">{zhTW.common.pageLoading}</p>
      {:else if upcomingByPlant.length === 0}
        <p class="text-sm text-slate-600">目前沒有待備餐的訂單。</p>
      {:else}
        <ul class="grid gap-1 text-sm">
          {#each upcomingByPlant as entry}
            <li class="flex items-center justify-between gap-2">
              <span class="font-medium text-slate-800">{entry.plantId}</span>
              <span class="text-slate-600">剩 {entry.remaining} 份待備</span>
            </li>
          {/each}
        </ul>
      {/if}
      {#if policy}
        <p class="mt-3 text-xs text-slate-600">
          目前政策：預購開放 {policy.preorderOpenDaysAhead} 天｜前日截單時間 {minuteOfDayToTime(policy.modifyCancelCutoffMinuteOfDay)}
        </p>
      {/if}
    </Card>

    <Card title="今日配送狀態分布">
      {#if loading}
        <p class="text-sm text-slate-600">{zhTW.common.pageLoading}</p>
      {:else if !board || board.orders.length === 0}
        <p class="text-sm text-slate-600">今日尚無履約訂單。</p>
      {:else}
        <ul class="grid gap-1 text-sm">
          {#each board.plants as plant}
            <li class="grid gap-1 rounded border border-slate-200 bg-slate-50 p-2">
              <div class="flex items-center justify-between">
                <span class="font-semibold text-slate-800">{plant.plantId}</span>
                <span class="text-xs text-slate-600">訂單 {plant.orderCount}｜份數 {plant.portionCount}</span>
              </div>
              <div class="flex flex-wrap gap-1">
                {#each plant.deliveryStatusCounts as entry}
                  <StateTag label={`${deliveryStatusLabel(entry.status)} ${entry.count}`} tone="neutral" />
                {/each}
              </div>
            </li>
          {/each}
        </ul>
      {/if}
    </Card>
  </div>
</section>
