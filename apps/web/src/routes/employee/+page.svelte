<script lang="ts">
  import { onMount } from "svelte";

  import {
    PageHeader,
    Card,
    Button,
    EmptyState,
    StateTag,
    MoneyAmount,
    CountdownBadge
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
    type MenuDiscoveryItem
  } from "$lib/employee/api";
  import { isPickupEligible, taipeiDateMinuteToEpochMs } from "$lib/employee/portal";
  import { friendlyOrderStatus, orderStatusTone, maskIdentifier } from "$lib/platform/labels";
  import { apiClient } from "$lib/platform/api";

  import type { PageData } from "./$types";

  let { data }: { data: PageData } = $props();

  const actor = $derived(data.actor);
  const plantId = $derived(actor?.scope.plantIds[0] ?? null);
  const role = $derived(actor?.role ?? null);

  let loading = $state(true);
  let loadError = $state<string | null>(null);
  let orders = $state<EmployeeOrderView[]>([]);
  let upcomingMenus = $state<MenuDiscoveryItem[]>([]);
  let menuItemNameMap = $state<Record<string, string>>({});

  const today = todayTaipeiIsoDate();

  const todayPickupOrders = $derived(
    orders.filter(
      (order) => order.deliveryDate === today && isPickupEligible(order.status)
    )
  );
  const recentOrders = $derived(orders.slice(0, 5));

  onMount(() => {
    if (role === "employee" && plantId) {
      void loadHomeData(plantId, data.auth.apiBearerToken);
    } else {
      loading = false;
    }
  });

  async function loadHomeData(resolvedPlantId: string, bearerToken: string | null) {
    loading = true;
    loadError = null;
    try {
      configureEmployeeApi(resolvedPlantId, bearerToken);
      const fromDate = addDaysIsoDate(today, -14);
      const toDate = addDaysIsoDate(today, 7);
      const [ordersPage, menuPage] = await Promise.all([
        apiClient.employee.listEmployeeOrders(
          resolvedPlantId,
          fromDate,
          toDate,
          1,
          50,
          "deliveryDate",
          "desc"
        ),
        apiClient.employee.listEmployeeMenus(
          resolvedPlantId,
          "calendar",
          undefined,
          today,
          addDaysIsoDate(today, 2),
          1,
          50,
          "deliveryDate",
          "asc"
        )
      ]);
      orders = ordersPage.items;
      upcomingMenus = menuPage.items
        .filter((item) => item.preorderOpen)
        .slice(0, 3);
      const ids = orders.flatMap((o) => o.lineItems.map((li) => li.menuItemId));
      menuItemNameMap = await loadMenuItemNameMap(resolvedPlantId, ids);
    } catch (error) {
      loadError = describeApiError(error);
    } finally {
      loading = false;
    }
  }

  function menuCutoffEpochMs(item: MenuDiscoveryItem): number {
    return taipeiDateMinuteToEpochMs(item.cutoffDate, item.modifyCancelCutoffMinuteOfDay);
  }

  const heroOrder = $derived(todayPickupOrders[0] ?? null);
  const restTodayOrders = $derived(todayPickupOrders.slice(1));
</script>

<PlantGuard role={role} plantId={plantId}>
  <PageHeader
    eyebrow="員工入口"
    title={actor ? `您好，${actor.displayName}` : "今日"}
    description="這裡列出今天要處理的餐點，以及即將截單的菜單提醒。"
    breadcrumbs={data.breadcrumbs}
  />

  {#if loading}
    <Card title="同步中">
      <p class="text-sm text-slate-600">載入今日資料中...</p>
    </Card>
  {:else if loadError}
    <Card variant="danger" title="載入失敗">
      <p class="text-sm text-rose-900">{loadError}</p>
    </Card>
  {:else}
    <div class="grid gap-4">
      {#if heroOrder}
        {@const heroSummary = summarizeOrderLineItems(heroOrder.lineItems, menuItemNameMap)}
        <article class="grid gap-2 rounded-2xl border border-amber-300 bg-amber-50 p-4 shadow-sm md:p-5">
          <p class="text-xs font-semibold uppercase tracking-wide text-amber-700">今日待領取</p>
          <h2 class="text-lg font-bold text-slate-900 md:text-xl">
            {heroOrder.deliveryDate} · {heroSummary}
          </h2>
          <div class="flex flex-wrap items-center gap-2">
            <StateTag
              label={friendlyOrderStatus(heroOrder.status)}
              tone={orderStatusTone(heroOrder.status)}
            />
            <MoneyAmount
              amountMinor={heroOrder.total.amountMinor}
              currency={heroOrder.total.currency}
            />
            <span class="text-xs text-slate-500">訂單 {maskIdentifier(heroOrder.orderId)}</span>
          </div>
          <div>
            <Button
              variant="primary"
              href={`/employee/orders/${heroOrder.orderId}/pickup`}
            >
              顯示領餐 QR
            </Button>
          </div>
        </article>
        {#if restTodayOrders.length > 0}
          <Card title="其他今日待領取">
            <div class="grid gap-2">
              {#each restTodayOrders as order (order.orderId)}
                {@const summary = summarizeOrderLineItems(order.lineItems, menuItemNameMap)}
                <article class="grid gap-2 rounded-xl border border-amber-200 bg-amber-50/70 p-3">
                  <div class="flex flex-wrap items-center justify-between gap-2">
                    <div>
                      <p class="text-sm font-semibold text-slate-900">{summary}</p>
                      <p class="text-xs text-slate-500">
                        配送日 {order.deliveryDate} · 訂單 {maskIdentifier(order.orderId)}
                      </p>
                    </div>
                    <StateTag
                      label={friendlyOrderStatus(order.status)}
                      tone={orderStatusTone(order.status)}
                    />
                  </div>
                  <div class="flex flex-wrap items-center justify-between gap-2">
                    <MoneyAmount
                      amountMinor={order.total.amountMinor}
                      currency={order.total.currency}
                    />
                    <Button
                      variant="primary"
                      href={`/employee/orders/${order.orderId}/pickup`}
                    >
                      顯示領餐 QR
                    </Button>
                  </div>
                </article>
              {/each}
            </div>
          </Card>
        {/if}
      {:else}
        <Card title="今日待領取" description="當日可顯示領餐 QR 的訂單。">
          <EmptyState
            title="今日沒有待領取訂單"
            description="想好明天吃什麼了嗎？到菜單瀏覽區域下一筆吧。"
          >
            {#snippet actions()}
              <Button href="/employee/discover" variant="primary">瀏覽菜單 →</Button>
            {/snippet}
          </EmptyState>
        </Card>
      {/if}

      <Card title="即將截單" description="近三天內仍開放預購的熱門項目。">
        {#if upcomingMenus.length === 0}
          <EmptyState
            title="近期沒有即將截單的預購"
            description="可能還沒開放或已經截止，去菜單看更多日期吧。"
          >
            {#snippet actions()}
              <Button href="/employee/discover" variant="primary">瀏覽菜單</Button>
            {/snippet}
          </EmptyState>
        {:else}
          <div class="grid gap-2">
            {#each upcomingMenus as item (item.menuItemId)}
              <article class="grid gap-1 rounded-xl border border-slate-200 bg-white p-3 shadow-sm">
                <div class="flex flex-wrap items-center justify-between gap-2">
                  <div>
                    <p class="text-sm font-semibold text-slate-900">{item.name}</p>
                    <p class="text-xs text-slate-600">配送日：{item.deliveryDate}</p>
                  </div>
                  <div class="flex flex-wrap items-center gap-2">
                    <StateTag label={`剩 ${item.remainingQuantity}`} tone="info" />
                    <CountdownBadge deadlineEpochMs={menuCutoffEpochMs(item)} prefix="截單剩" />
                    <MoneyAmount
                      amountMinor={item.price.amountMinor}
                      currency={item.price.currency}
                    />
                  </div>
                </div>
              </article>
            {/each}
            <Button href="/employee/discover" variant="secondary">看全部菜單 →</Button>
          </div>
        {/if}
      </Card>

      <Card title="最近訂單" description="近 14 天內的訂單狀態一覽。">
        {#if recentOrders.length === 0}
          <EmptyState title="尚無訂單" description="第一次下單後會顯示在這裡。">
            {#snippet actions()}
              <Button href="/employee/discover" variant="primary">瀏覽菜單 →</Button>
            {/snippet}
          </EmptyState>
        {:else}
          <div class="grid gap-2">
            {#each recentOrders as order (order.orderId)}
              {@const summary = summarizeOrderLineItems(order.lineItems, menuItemNameMap)}
              <article class="flex flex-wrap items-center justify-between gap-2 rounded-lg border border-slate-200 bg-white p-3">
                <div>
                  <p class="text-sm font-semibold text-slate-900">{summary}</p>
                  <p class="text-xs text-slate-500">
                    配送日 {order.deliveryDate} · 訂單 {maskIdentifier(order.orderId)}
                  </p>
                </div>
                <div class="flex items-center gap-2">
                  <StateTag
                    label={friendlyOrderStatus(order.status)}
                    tone={orderStatusTone(order.status)}
                  />
                  <MoneyAmount
                    amountMinor={order.total.amountMinor}
                    currency={order.total.currency}
                  />
                  <Button
                    href={`/employee/orders/${order.orderId}`}
                    variant="ghost"
                    size="sm"
                  >
                    詳情
                  </Button>
                </div>
              </article>
            {/each}
            <div class="flex flex-wrap gap-2">
              <Button href="/employee/orders" variant="secondary">查看所有訂單</Button>
              <Button href="/employee/wallet" variant="ghost">查看扣款</Button>
            </div>
          </div>
        {/if}
      </Card>
    </div>
  {/if}
</PlantGuard>
