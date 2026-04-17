<script lang="ts">
  import { onDestroy, onMount } from "svelte";
  import QRCode from "qrcode";

  import { zhTW } from "$lib/i18n/zh-tw";
  import {
    countdownToEpoch,
    isEmployeeOrderEditable,
    isPickupEligible,
    isResolvedDisputeStatus,
    pickupQrSecondsRemaining,
    taipeiDateMinuteToEpochMs
  } from "$lib/employee/portal";
  import { apiClient, ensureApiClientConfigured } from "$lib/platform/api";
  import { normalizeApiFailure } from "$lib/platform/api/failure";

  interface Props {
    sectionId: string;
    actorDisplayName: string;
    actorId: string;
    provider: string;
    plantId: string | null;
  }

  let { sectionId, actorDisplayName, actorId, provider, plantId }: Props = $props();

  type MenuPageResponse = Awaited<ReturnType<typeof apiClient.employee.listEmployeeMenus>>;
  type MenuDiscoveryItem = MenuPageResponse["items"][number];
  type MenuDiscoveryDay = MenuPageResponse["days"][number];
  type EmployeeOrderPageResponse = Awaited<ReturnType<typeof apiClient.employee.listEmployeeOrders>>;
  type EmployeeOrderView = EmployeeOrderPageResponse["items"][number];
  type PayrollLedgerView = Awaited<ReturnType<typeof apiClient.employee.getEmployeeOrderPayrollLedger>>;
  type PickupQrView = Awaited<ReturnType<typeof apiClient.employee.getEmployeePickupVerificationQr>>;

  interface PortalNotification {
    id: number;
    kind: "success" | "error" | "info";
    message: string;
  }

  const sectionTitleById: Record<string, string> = {
    overview: zhTW.nav.sections.employee.overview,
    orders: zhTW.nav.sections.employee.orders,
    payroll: zhTW.nav.sections.employee.payroll
  };

  const initialTaipeiDate = todayTaipeiIsoDate();
  let nowEpochMs = $state(Date.now());
  let menuView = $state<"week" | "calendar">("week");
  let menuAnchorDate = $state(initialTaipeiDate);
  let menuFromDate = $state(initialTaipeiDate);
  let menuToDate = $state(addDaysIsoDate(initialTaipeiDate, 13));
  let orderNote = $state("");

  let menuDays = $state<MenuDiscoveryDay[]>([]);
  let menuItems = $state<MenuDiscoveryItem[]>([]);
  let menusLoading = $state(false);
  let menuError = $state<string | null>(null);

  let orders = $state<EmployeeOrderView[]>([]);
  let ordersLoading = $state(false);
  let orderError = $state<string | null>(null);

  let notifications = $state<PortalNotification[]>([]);
  let nextNotificationId = 1;
  const notificationTimeouts = new Set<ReturnType<typeof setTimeout>>();

  let orderDraftQuantities = $state<Record<string, number>>({});
  let orderEditQuantities = $state<Record<string, number>>({});
  let orderCancelReasons = $state<Record<string, string>>({});

  let selectedPayrollOrderId = $state<string | null>(null);
  let payrollLedgersByOrderId = $state<Record<string, PayrollLedgerView>>({});
  let payrollLoadingByOrderId = $state<Record<string, boolean>>({});
  let payrollErrorByOrderId = $state<Record<string, string>>({});
  let disputeReasons = $state<Record<string, string>>({});
  let disputeSubmittingByOrderId = $state<Record<string, boolean>>({});

  let activePickupOrderId = $state<string | null>(null);
  let pickupQr = $state<PickupQrView | null>(null);
  let pickupQrImageDataUrl = $state<string | null>(null);
  let pickupQrLoading = $state(false);
  let pickupQrError = $state<string | null>(null);

  let clockTimer: ReturnType<typeof setInterval> | null = null;
  let pickupQrRefreshTimer: ReturnType<typeof setTimeout> | null = null;

  const showOrdersSection = $derived(sectionId === "overview" || sectionId === "orders");
  const showPayrollSection = $derived(sectionId === "overview" || sectionId === "payroll");
  const pickupQrSeconds = $derived.by(() =>
    pickupQr ? pickupQrSecondsRemaining(nowEpochMs, pickupQr) : 0
  );
  const selectedPayrollLedger = $derived.by(() => {
    if (!selectedPayrollOrderId) {
      return null;
    }

    return payrollLedgersByOrderId[selectedPayrollOrderId] ?? null;
  });
  const payrollSummary = $derived.by(() => {
    const ledgers = Object.values(payrollLedgersByOrderId);
    let netAmountMinor = 0;
    let openDisputeCount = 0;
    let resolvedDisputeCount = 0;
    let currency = "TWD";

    for (const ledger of ledgers) {
      netAmountMinor += ledger.netAmountMinor;
      currency = ledger.currency;
      for (const dispute of ledger.disputes) {
        if (isResolvedDisputeStatus(dispute.status)) {
          resolvedDisputeCount += 1;
        } else {
          openDisputeCount += 1;
        }
      }
    }

    return {
      loadedOrderCount: ledgers.length,
      netAmountMinor,
      currency,
      openDisputeCount,
      resolvedDisputeCount
    };
  });

  onMount(() => {
    nowEpochMs = Date.now();
    clockTimer = setInterval(() => {
      nowEpochMs = Date.now();
    }, 1000);
    void bootstrapPortal();
  });

  onDestroy(() => {
    if (clockTimer) {
      clearInterval(clockTimer);
    }
    if (pickupQrRefreshTimer) {
      clearTimeout(pickupQrRefreshTimer);
    }
    for (const timeout of notificationTimeouts) {
      clearTimeout(timeout);
    }
    notificationTimeouts.clear();
  });

  $effect(() => {
    if (!showPayrollSection || orders.length === 0) {
      return;
    }

    if (!selectedPayrollOrderId || !orders.some((order) => order.orderId === selectedPayrollOrderId)) {
      const firstOrder = orders[0];
      selectedPayrollOrderId = firstOrder.orderId;
      if (!payrollLedgersByOrderId[firstOrder.orderId]) {
        void loadPayrollLedger(firstOrder.orderId, false);
      }
      return;
    }

    if (
      selectedPayrollOrderId &&
      !payrollLedgersByOrderId[selectedPayrollOrderId] &&
      !payrollLoadingByOrderId[selectedPayrollOrderId]
    ) {
      void loadPayrollLedger(selectedPayrollOrderId, false);
    }
  });

  $effect(() => {
    if (!activePickupOrderId || !pickupQr || pickupQrLoading) {
      return;
    }

    if (pickupQrSeconds <= 0) {
      void refreshPickupQr(activePickupOrderId, false);
    }
  });

  async function bootstrapPortal() {
    if (!plantId) {
      const message = "目前登入帳號沒有可用的廠區範圍，無法載入員工訂餐資料。";
      pushNotification("error", message);
      menuError = message;
      orderError = message;
      return;
    }

    try {
      ensureApiClientConfigured();
    } catch (error) {
      const failure = normalizeApiFailure(error);
      menuError = failure.localizedMessage;
      orderError = failure.localizedMessage;
      pushNotification("error", failure.localizedMessage);
      return;
    }

    await Promise.all([refreshMenus(false), refreshOrders(false)]);
  }

  async function refreshMenus(notifyOnError: boolean) {
    if (!plantId) {
      return;
    }

    menusLoading = true;
    menuError = null;
    try {
      const page = await apiClient.employee.listEmployeeMenus(
        plantId,
        menuView,
        menuView === "week" ? menuAnchorDate : undefined,
        menuView === "calendar" ? menuFromDate : undefined,
        menuView === "calendar" ? menuToDate : undefined,
        1,
        200,
        "deliveryDate",
        "asc"
      );
      menuDays = page.days;
      menuItems = page.items;
    } catch (error) {
      const failure = normalizeApiFailure(error);
      menuError = failure.localizedMessage;
      if (notifyOnError) {
        pushNotification("error", failure.localizedMessage);
      }
    } finally {
      menusLoading = false;
    }
  }

  async function refreshOrders(notifyOnError: boolean) {
    if (!plantId) {
      return;
    }

    ordersLoading = true;
    orderError = null;
    try {
      const page = await apiClient.employee.listEmployeeOrders(
        plantId,
        undefined,
        undefined,
        1,
        200,
        "deliveryDate",
        "desc"
      );
      orders = page.items;
      hydrateOrderDraftState(page.items);
      if (activePickupOrderId && !page.items.some((order) => order.orderId === activePickupOrderId)) {
        clearPickupQrState();
      }
    } catch (error) {
      const failure = normalizeApiFailure(error);
      orderError = failure.localizedMessage;
      if (notifyOnError) {
        pushNotification("error", failure.localizedMessage);
      }
    } finally {
      ordersLoading = false;
    }
  }

  function hydrateOrderDraftState(latestOrders: EmployeeOrderView[]) {
    const nextEditQuantities = { ...orderEditQuantities };
    const nextCancelReasons = { ...orderCancelReasons };

    for (const order of latestOrders) {
      for (const lineItem of order.lineItems) {
        const key = orderEditQuantityKey(order.orderId, lineItem.menuItemId);
        if (nextEditQuantities[key] === undefined) {
          nextEditQuantities[key] = lineItem.quantity;
        }
      }
      if (nextCancelReasons[order.orderId] === undefined) {
        nextCancelReasons[order.orderId] = "行程調整取消";
      }
    }

    orderEditQuantities = nextEditQuantities;
    orderCancelReasons = nextCancelReasons;
  }

  async function placeOrder(item: MenuDiscoveryItem) {
    if (!plantId) {
      return;
    }

    const quantity = orderDraftQuantity(item);
    if (quantity < 1 || quantity > item.remainingQuantity) {
      pushNotification("error", "下單數量超過可用庫存，請調整後再試。");
      return;
    }

    try {
      await apiClient.employee.createEmployeeOrder({
        plantId: plantId,
        deliveryDate: item.deliveryDate,
        lineItems: [
          {
            menuItemId: item.menuItemId,
            quantity,
            specialRequests: []
          }
        ],
        employeeNote: normalizedOptional(orderNote)
      });
      pushNotification("success", `已建立訂單：${item.name} x ${quantity}`);
      orderDraftQuantities = {
        ...orderDraftQuantities,
        [item.menuItemId]: 1
      };
      orderNote = "";
      await Promise.all([refreshMenus(false), refreshOrders(false)]);
    } catch (error) {
      const failure = normalizeApiFailure(error);
      pushNotification("error", failure.localizedMessage);
    }
  }

  async function replaceOrderLineItems(order: EmployeeOrderView) {
    if (!isEmployeeOrderEditable(order.status)) {
      pushNotification("error", "此訂單狀態不可修改。");
      return;
    }

    const lineItems = order.lineItems.map((lineItem) => ({
      menuItemId: lineItem.menuItemId,
      quantity: normalizedQuantity(
        orderEditQuantities[orderEditQuantityKey(order.orderId, lineItem.menuItemId)] ??
          lineItem.quantity
      )
    }));
    if (lineItems.some((lineItem) => lineItem.quantity < 1)) {
      pushNotification("error", "修改後的訂單數量至少要為 1。");
      return;
    }

    try {
      await apiClient.employee.updateEmployeeOrder(order.orderId, {
        operation: "REPLACE_LINE_ITEMS",
        lineItems
      });
      pushNotification("success", `訂單 ${order.orderId} 已更新。`);
      await Promise.all([refreshMenus(false), refreshOrders(false), loadPayrollLedger(order.orderId, false)]);
    } catch (error) {
      const failure = normalizeApiFailure(error);
      pushNotification("error", failure.localizedMessage);
    }
  }

  async function cancelOrder(order: EmployeeOrderView) {
    if (!isEmployeeOrderEditable(order.status)) {
      pushNotification("error", "此訂單狀態不可取消。");
      return;
    }

    const cancelReason = (orderCancelReasons[order.orderId] ?? "").trim();
    if (cancelReason.length < 5) {
      pushNotification("error", "取消原因至少需要 5 個字。");
      return;
    }

    try {
      await apiClient.employee.updateEmployeeOrder(order.orderId, {
        operation: "CANCEL",
        cancelReason
      });
      pushNotification("success", `訂單 ${order.orderId} 已取消。`);
      if (activePickupOrderId === order.orderId) {
        clearPickupQrState();
      }
      await Promise.all([refreshMenus(false), refreshOrders(false), loadPayrollLedger(order.orderId, false)]);
    } catch (error) {
      const failure = normalizeApiFailure(error);
      pushNotification("error", failure.localizedMessage);
    }
  }

  async function loadPayrollLedger(orderId: string, notifyOnError: boolean) {
    payrollLoadingByOrderId = {
      ...payrollLoadingByOrderId,
      [orderId]: true
    };
    payrollErrorByOrderId = {
      ...payrollErrorByOrderId,
      [orderId]: ""
    };

    try {
      const ledger = await apiClient.employee.getEmployeeOrderPayrollLedger(orderId);
      payrollLedgersByOrderId = {
        ...payrollLedgersByOrderId,
        [orderId]: ledger
      };
    } catch (error) {
      const failure = normalizeApiFailure(error);
      payrollErrorByOrderId = {
        ...payrollErrorByOrderId,
        [orderId]: failure.localizedMessage
      };
      if (notifyOnError) {
        pushNotification("error", failure.localizedMessage);
      }
    } finally {
      payrollLoadingByOrderId = {
        ...payrollLoadingByOrderId,
        [orderId]: false
      };
    }
  }

  async function submitDispute(orderId: string) {
    const reason = (disputeReasons[orderId] ?? "").trim();
    if (!reason) {
      pushNotification("error", "申訴原因不可為空白。");
      return;
    }

    disputeSubmittingByOrderId = {
      ...disputeSubmittingByOrderId,
      [orderId]: true
    };

    try {
      await apiClient.employee.createEmployeeOrderDispute(orderId, { reason });
      disputeReasons = {
        ...disputeReasons,
        [orderId]: ""
      };
      pushNotification("success", `已提交薪資申訴：${orderId}`);
      await loadPayrollLedger(orderId, false);
    } catch (error) {
      const failure = normalizeApiFailure(error);
      pushNotification("error", failure.localizedMessage);
    } finally {
      disputeSubmittingByOrderId = {
        ...disputeSubmittingByOrderId,
        [orderId]: false
      };
    }
  }

  async function activatePickupQr(orderId: string) {
    activePickupOrderId = orderId;
    await refreshPickupQr(orderId, true);
  }

  async function refreshPickupQr(orderId: string, notifyOnError: boolean) {
    pickupQrLoading = true;
    pickupQrError = null;
    try {
      const payload = await apiClient.employee.getEmployeePickupVerificationQr(orderId);
      const imageDataUrl = await QRCode.toDataURL(payload.verificationCode, {
        width: 360,
        margin: 1,
        errorCorrectionLevel: "M",
        color: {
          dark: "#0f172a",
          light: "#ffffff"
        }
      });
      if (activePickupOrderId !== orderId) {
        return;
      }

      pickupQr = payload;
      pickupQrImageDataUrl = imageDataUrl;
      schedulePickupQrRefresh(orderId, payload.secondsUntilRefresh);
    } catch (error) {
      const failure = normalizeApiFailure(error);
      pickupQrError = failure.localizedMessage;
      if (notifyOnError) {
        pushNotification("error", failure.localizedMessage);
      }
    } finally {
      pickupQrLoading = false;
    }
  }

  async function verifyPickupForActiveOrder() {
    if (!activePickupOrderId || !pickupQr) {
      return;
    }
    const orderId = activePickupOrderId;

    try {
      await apiClient.employee.verifyPickupOrder(orderId, {
        verificationCode: pickupQr.verificationCode
      });
      pushNotification("success", `訂單 ${orderId} 已完成領餐核銷。`);
      clearPickupQrState();
      await Promise.all([refreshMenus(false), refreshOrders(false), loadPayrollLedger(orderId, false)]);
    } catch (error) {
      const failure = normalizeApiFailure(error);
      pushNotification("error", failure.localizedMessage);
    }
  }

  function clearPickupQrState() {
    if (pickupQrRefreshTimer) {
      clearTimeout(pickupQrRefreshTimer);
      pickupQrRefreshTimer = null;
    }
    activePickupOrderId = null;
    pickupQr = null;
    pickupQrImageDataUrl = null;
    pickupQrError = null;
  }

  function schedulePickupQrRefresh(orderId: string, secondsUntilRefresh: number) {
    if (pickupQrRefreshTimer) {
      clearTimeout(pickupQrRefreshTimer);
    }
    pickupQrRefreshTimer = setTimeout(() => {
      if (activePickupOrderId === orderId) {
        void refreshPickupQr(orderId, false);
      }
    }, Math.max(1000, secondsUntilRefresh * 1000));
  }

  function orderDraftQuantity(item: MenuDiscoveryItem): number {
    const draft = orderDraftQuantities[item.menuItemId];
    if (draft !== undefined) {
      return normalizedQuantity(Math.min(draft, Math.max(1, item.remainingQuantity || 1)));
    }

    if (item.remainingQuantity === 0) {
      return 0;
    }

    return 1;
  }

  function updateOrderDraftQuantity(menuItemId: string, rawValue: string, remainingQuantity: number) {
    const parsed = normalizedQuantity(Number.parseInt(rawValue, 10));
    const upperBound = Math.max(1, Math.min(20, remainingQuantity));
    orderDraftQuantities = {
      ...orderDraftQuantities,
      [menuItemId]: Math.min(parsed, upperBound)
    };
  }

  function updateOrderLineItemQuantity(orderId: string, menuItemId: string, rawValue: string) {
    const parsed = normalizedQuantity(Number.parseInt(rawValue, 10));
    orderEditQuantities = {
      ...orderEditQuantities,
      [orderEditQuantityKey(orderId, menuItemId)]: parsed
    };
  }

  function updateCancelReason(orderId: string, reason: string) {
    orderCancelReasons = {
      ...orderCancelReasons,
      [orderId]: reason
    };
  }

  function updateDisputeReason(orderId: string, reason: string) {
    disputeReasons = {
      ...disputeReasons,
      [orderId]: reason
    };
  }

  function orderEditQuantity(orderId: string, menuItemId: string, fallback: number): number {
    return (
      orderEditQuantities[orderEditQuantityKey(orderId, menuItemId)] ??
      normalizedQuantity(fallback)
    );
  }

  function orderEditQuantityKey(orderId: string, menuItemId: string): string {
    return `${orderId}:${menuItemId}`;
  }

  function menuCutoffCountdown(item: MenuDiscoveryItem) {
    const cutoffEpochMs = taipeiDateMinuteToEpochMs(
      item.cutoffDate,
      item.modifyCancelCutoffMinuteOfDay
    );
    return countdownToEpoch(nowEpochMs, cutoffEpochMs);
  }

  function formatMoney(currency: string, amountMinor: number): string {
    return new Intl.NumberFormat("zh-TW", {
      style: "currency",
      currency,
      minimumFractionDigits: 0,
      maximumFractionDigits: 2
    }).format(amountMinor / 100);
  }

  function friendlyStatus(status: string): string {
    const map: Record<string, string> = {
      PENDING: "待處理",
      MODIFIED: "已修改",
      CANCELLED: "已取消",
      SOLD_OUT: "售罄",
      REFUND_PENDING: "退款中",
      REFUNDED: "已退款",
      FULFILLED: "已領餐",
      OPEN: "已建立",
      IN_REVIEW: "審查中",
      RESOLVED_REFUND_APPROVED: "已結案（退款）",
      RESOLVED_REJECTED: "已結案（駁回）"
    };
    return map[status] ?? status;
  }

  function pushNotification(kind: PortalNotification["kind"], message: string) {
    const notification: PortalNotification = {
      id: nextNotificationId++,
      kind,
      message
    };
    notifications = [notification, ...notifications].slice(0, 5);
    const timeout = setTimeout(() => {
      notifications = notifications.filter((item) => item.id !== notification.id);
      notificationTimeouts.delete(timeout);
    }, 6000);
    notificationTimeouts.add(timeout);
  }

  function normalizedQuantity(value: number): number {
    if (!Number.isFinite(value) || value < 1) {
      return 1;
    }
    return Math.min(20, Math.floor(value));
  }

  function normalizedOptional(value: string): string | undefined {
    const normalized = value.trim();
    return normalized.length === 0 ? undefined : normalized;
  }

  function onMenuFilterSubmit() {
    void refreshMenus(true);
  }

  function sectionTitle(): string {
    return sectionTitleById[sectionId] ?? sectionId;
  }

  function todayTaipeiIsoDate(): string {
    return new Date().toLocaleDateString("en-CA", { timeZone: "Asia/Taipei" });
  }

  function addDaysIsoDate(baseIsoDate: string, days: number): string {
    const [year, month, day] = baseIsoDate.split("-").map((value) => Number.parseInt(value, 10));
    const date = new Date(Date.UTC(year, month - 1, day + days));
    const nextYear = date.getUTCFullYear();
    const nextMonth = `${date.getUTCMonth() + 1}`.padStart(2, "0");
    const nextDay = `${date.getUTCDate()}`.padStart(2, "0");
    return `${nextYear}-${nextMonth}-${nextDay}`;
  }
</script>

<section class="grid gap-5">
  <header class="rounded-2xl border border-cyan-100 bg-cyan-50/70 p-4">
    <p class="text-xs font-semibold tracking-[0.14em] text-cyan-800">員工入口 MVP</p>
    <h2 class="mt-1 text-xl font-bold text-slate-950">{sectionTitle()}</h2>
    <p class="mt-2 text-sm text-slate-700">{zhTW.portal.employee.sectionDescriptions[sectionId as keyof typeof zhTW.portal.employee.sectionDescriptions] ?? zhTW.portal.employee.lead}</p>
    <p class="mt-2 text-xs text-slate-600">Actor: {actorDisplayName} ({actorId}) | Provider: {provider} | Plant: {plantId ?? "N/A"}</p>
  </header>

  {#if notifications.length > 0}
    <aside class="grid gap-2">
      {#each notifications as notification (notification.id)}
        <div class={`rounded-xl border px-3 py-2 text-sm ${notification.kind === "success" ? "border-emerald-300 bg-emerald-50 text-emerald-900" : notification.kind === "error" ? "border-rose-300 bg-rose-50 text-rose-900" : "border-cyan-300 bg-cyan-50 text-cyan-900"}`}>
          {notification.message}
        </div>
      {/each}
    </aside>
  {/if}

  {#if showOrdersSection}
    <section class="grid gap-4 rounded-2xl border border-slate-200 bg-white p-4 shadow-sm">
      <header class="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h3 class="text-lg font-semibold text-slate-900">菜單瀏覽與下單</h3>
          <p class="text-sm text-slate-600">支援週檢視與日曆檢視，庫存與截單倒數即時呈現。</p>
        </div>
        <div class="flex items-center gap-2">
          <button
            class={`rounded-lg border px-3 py-2 text-sm font-medium ${menuView === "week" ? "border-cyan-700 bg-cyan-700 text-white" : "border-slate-300 bg-white text-slate-700"}`}
            type="button"
            onclick={() => {
              menuView = "week";
            }}
          >
            週檢視
          </button>
          <button
            class={`rounded-lg border px-3 py-2 text-sm font-medium ${menuView === "calendar" ? "border-cyan-700 bg-cyan-700 text-white" : "border-slate-300 bg-white text-slate-700"}`}
            type="button"
            onclick={() => {
              menuView = "calendar";
            }}
          >
            日曆檢視
          </button>
          <button
            class="rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm font-medium text-slate-700"
            type="button"
            onclick={() => {
              void refreshMenus(true);
            }}
          >
            重新載入
          </button>
        </div>
      </header>

      <form
        class="grid gap-3 rounded-xl border border-slate-200 bg-slate-50 p-3 md:grid-cols-4"
        onsubmit={(event) => {
          event.preventDefault();
          onMenuFilterSubmit();
        }}
      >
        {#if menuView === "week"}
          <label class="grid gap-1 text-sm text-slate-700 md:col-span-2">
            <span class="font-medium">週起始日（台北）</span>
            <input class="rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm" type="date" bind:value={menuAnchorDate} />
          </label>
        {:else}
          <label class="grid gap-1 text-sm text-slate-700">
            <span class="font-medium">起始日（台北）</span>
            <input class="rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm" type="date" bind:value={menuFromDate} />
          </label>
          <label class="grid gap-1 text-sm text-slate-700">
            <span class="font-medium">結束日（台北）</span>
            <input class="rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm" type="date" bind:value={menuToDate} />
          </label>
        {/if}
        <label class="grid gap-1 text-sm text-slate-700 md:col-span-2">
          <span class="font-medium">訂單備註（可選）</span>
          <input class="rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm" type="text" maxlength={200} placeholder="例如：午休晚 10 分鐘取餐" bind:value={orderNote} />
        </label>
        <div class="md:col-span-4">
          <button class="rounded-lg bg-slate-900 px-4 py-2 text-sm font-semibold text-white" type="submit">
            套用條件
          </button>
        </div>
      </form>

      {#if menusLoading}
        <p class="text-sm text-slate-600">菜單載入中...</p>
      {:else if menuError}
        <p class="text-sm text-rose-700">{menuError}</p>
      {:else if menuItems.length === 0}
        <p class="text-sm text-slate-600">目前查詢條件沒有可下單菜單。</p>
      {:else}
        <div class="grid gap-3 md:grid-cols-2">
          {#each menuItems as item (item.menuItemId)}
            {@const cutoff = menuCutoffCountdown(item)}
            <article class="grid gap-3 rounded-xl border border-slate-200 bg-white p-3 shadow-sm">
              <div class="flex items-start justify-between gap-2">
                <div>
                  <h4 class="text-base font-semibold text-slate-900">{item.name}</h4>
                  <p class="text-xs text-slate-600">{item.description}</p>
                  <p class="mt-1 text-xs text-slate-500">配送日：{item.deliveryDate} | 菜單編號：{item.menuItemId}</p>
                </div>
                <p class="text-sm font-semibold text-slate-900">{formatMoney(item.price.currency, item.price.amountMinor)}</p>
              </div>
              <div class="grid gap-1 text-xs text-slate-700">
                <p>剩餘庫存：<span class="font-semibold">{item.remainingQuantity}</span></p>
                <p>預購狀態：<span class={`font-semibold ${item.preorderOpen ? "text-emerald-700" : "text-rose-700"}`}>{item.preorderOpen ? "可下單" : "已關閉"}</span></p>
                <p>截單時間：前一天 {`${Math.floor(item.modifyCancelCutoffMinuteOfDay / 60)}`.padStart(2, "0")}:{`${item.modifyCancelCutoffMinuteOfDay % 60}`.padStart(2, "0")}（台北）</p>
                <p class={`font-medium ${cutoff.expired ? "text-rose-700" : "text-amber-700"}`}>截單{cutoff.label}</p>
              </div>
              <div class="flex items-center gap-2">
                <input
                  class="w-24 rounded-lg border border-slate-300 px-2 py-1 text-sm"
                  type="number"
                  min="1"
                  max={Math.max(1, Math.min(20, item.remainingQuantity))}
                  value={orderDraftQuantity(item)}
                  disabled={!item.preorderOpen || item.remainingQuantity === 0}
                  oninput={(event) =>
                    updateOrderDraftQuantity(
                      item.menuItemId,
                      (event.currentTarget as HTMLInputElement).value,
                      item.remainingQuantity
                    )}
                />
                <button
                  class="rounded-lg bg-cyan-700 px-3 py-2 text-sm font-semibold text-white disabled:cursor-not-allowed disabled:bg-slate-300"
                  type="button"
                  disabled={!item.preorderOpen || item.remainingQuantity === 0}
                  onclick={() => {
                    void placeOrder(item);
                  }}
                >
                  立即下單
                </button>
              </div>
            </article>
          {/each}
        </div>
      {/if}

      <div class="grid gap-2 rounded-xl border border-slate-200 bg-slate-50 p-3">
        <h4 class="text-sm font-semibold text-slate-900">週 / 日曆分組預覽</h4>
        <div class="grid gap-2 md:grid-cols-2">
          {#each menuDays as day (day.deliveryDate)}
            <div class="rounded-lg border border-slate-200 bg-white p-2 text-xs text-slate-700">
              <p class="font-semibold">{day.deliveryDate}</p>
              <p>{day.items.length} 筆可見項目</p>
            </div>
          {/each}
        </div>
      </div>
    </section>

    <section class="grid gap-4 rounded-2xl border border-slate-200 bg-white p-4 shadow-sm">
      <header class="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h3 class="text-lg font-semibold text-slate-900">訂單管理與領餐核銷</h3>
          <p class="text-sm text-slate-600">可在截單前修改或取消，並查看 30 秒更新的取餐 QR。</p>
        </div>
        <button
          class="rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm font-medium text-slate-700"
          type="button"
          onclick={() => {
            void refreshOrders(true);
          }}
        >
          重新載入訂單
        </button>
      </header>

      {#if ordersLoading}
        <p class="text-sm text-slate-600">訂單載入中...</p>
      {:else if orderError}
        <p class="text-sm text-rose-700">{orderError}</p>
      {:else if orders.length === 0}
        <p class="text-sm text-slate-600">目前尚無訂單。</p>
      {:else}
        <div class="grid gap-3">
          {#each orders as order (order.orderId)}
            <article class="grid gap-3 rounded-xl border border-slate-200 bg-white p-3 shadow-sm">
              <div class="flex flex-wrap items-center justify-between gap-2">
                <div>
                  <p class="text-sm font-semibold text-slate-900">{order.orderId}</p>
                  <p class="text-xs text-slate-600">配送日：{order.deliveryDate} | 狀態：{friendlyStatus(order.status)}</p>
                </div>
                <p class="text-sm font-semibold text-slate-900">{formatMoney(order.total.currency, order.total.amountMinor)}</p>
              </div>

              <div class="grid gap-2">
                {#each order.lineItems as lineItem (lineItem.menuItemId)}
                  <div class="flex items-center justify-between rounded-lg border border-slate-200 bg-slate-50 px-2 py-2">
                    <div>
                      <p class="text-sm font-medium text-slate-900">{lineItem.menuItemId}</p>
                      <p class="text-xs text-slate-600">單價 {formatMoney(lineItem.pricePerUnit.currency, lineItem.pricePerUnit.amountMinor)}</p>
                    </div>
                    <input
                      class="w-20 rounded-lg border border-slate-300 px-2 py-1 text-sm"
                      type="number"
                      min="1"
                      max="20"
                      value={orderEditQuantity(order.orderId, lineItem.menuItemId, lineItem.quantity)}
                      disabled={!isEmployeeOrderEditable(order.status)}
                      oninput={(event) =>
                        updateOrderLineItemQuantity(
                          order.orderId,
                          lineItem.menuItemId,
                          (event.currentTarget as HTMLInputElement).value
                        )}
                    />
                  </div>
                {/each}
              </div>

              {#if isEmployeeOrderEditable(order.status)}
                <div class="grid gap-2 md:grid-cols-[2fr,auto,auto]">
                  <input
                    class="rounded-lg border border-slate-300 px-3 py-2 text-sm"
                    type="text"
                    value={orderCancelReasons[order.orderId] ?? ""}
                    maxlength={200}
                    oninput={(event) =>
                      updateCancelReason(order.orderId, (event.currentTarget as HTMLInputElement).value)}
                    placeholder="取消原因（至少 5 字）"
                  />
                  <button
                    class="rounded-lg bg-slate-900 px-3 py-2 text-sm font-semibold text-white"
                    type="button"
                    onclick={() => {
                      void replaceOrderLineItems(order);
                    }}
                  >
                    送出修改
                  </button>
                  <button
                    class="rounded-lg bg-rose-700 px-3 py-2 text-sm font-semibold text-white"
                    type="button"
                    onclick={() => {
                      void cancelOrder(order);
                    }}
                  >
                    取消訂單
                  </button>
                </div>
              {/if}

              {#if isPickupEligible(order.status)}
                <div class="grid gap-2 rounded-lg border border-amber-200 bg-amber-50 p-2">
                  <div class="flex flex-wrap items-center gap-2">
                    <button
                      class="rounded-lg bg-amber-700 px-3 py-2 text-sm font-semibold text-white"
                      type="button"
                      onclick={() => {
                        void activatePickupQr(order.orderId);
                      }}
                    >
                      顯示領餐 QR
                    </button>
                    {#if activePickupOrderId === order.orderId && pickupQr}
                      <p class="text-xs font-semibold text-amber-900">QR 更新倒數：{pickupQrSeconds} 秒</p>
                    {/if}
                  </div>

                  {#if activePickupOrderId === order.orderId}
                    {#if pickupQrLoading}
                      <p class="text-xs text-amber-900">QR 產生中...</p>
                    {:else if pickupQrError}
                      <p class="text-xs text-rose-700">{pickupQrError}</p>
                    {:else if pickupQrImageDataUrl && pickupQr}
                      <div class="grid gap-2 rounded-lg border border-slate-200 bg-white p-3">
                        <img class="mx-auto w-full max-w-[280px] rounded-lg border border-slate-200 bg-white p-2" src={pickupQrImageDataUrl} alt="pickup verification qr" />
                        <p class="break-all rounded-md bg-slate-900 px-2 py-2 text-center text-[11px] text-white">{pickupQr.verificationCode}</p>
                        <div class="flex flex-wrap gap-2">
                          <button
                            class="rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm font-medium text-slate-700"
                            type="button"
                            onclick={() => {
                              void refreshPickupQr(order.orderId, true);
                            }}
                          >
                            立即刷新 QR
                          </button>
                          <button
                            class="rounded-lg bg-emerald-700 px-3 py-2 text-sm font-semibold text-white"
                            type="button"
                            onclick={() => {
                              void verifyPickupForActiveOrder();
                            }}
                          >
                            完成領餐核銷
                          </button>
                        </div>
                        <p class="text-[11px] text-slate-600">此 QR 每 {pickupQr.refreshIntervalSeconds} 秒更新一次，請在手機全螢幕顯示供現場掃描。</p>
                      </div>
                    {/if}
                  {/if}
                </div>
              {/if}
            </article>
          {/each}
        </div>
      {/if}
    </section>
  {/if}

  {#if showPayrollSection}
    <section class="grid gap-4 rounded-2xl border border-slate-200 bg-white p-4 shadow-sm">
      <header>
        <h3 class="text-lg font-semibold text-slate-900">薪資扣款可視化與申訴追蹤</h3>
        <p class="text-sm text-slate-600">檢視扣款流水、提交申訴並追蹤處理狀態。</p>
      </header>

      <div class="grid gap-3 md:grid-cols-3">
        <article class="rounded-xl border border-slate-200 bg-slate-50 p-3">
          <p class="text-xs text-slate-500">已載入帳務訂單</p>
          <p class="mt-1 text-xl font-bold text-slate-900">{payrollSummary.loadedOrderCount}</p>
        </article>
        <article class="rounded-xl border border-slate-200 bg-slate-50 p-3">
          <p class="text-xs text-slate-500">已載入淨扣款總額</p>
          <p class="mt-1 text-xl font-bold text-slate-900">{formatMoney(payrollSummary.currency, payrollSummary.netAmountMinor)}</p>
        </article>
        <article class="rounded-xl border border-slate-200 bg-slate-50 p-3">
          <p class="text-xs text-slate-500">申訴狀態</p>
          <p class="mt-1 text-sm font-semibold text-slate-900">進行中 {payrollSummary.openDisputeCount} / 已結案 {payrollSummary.resolvedDisputeCount}</p>
        </article>
      </div>

      <div class="grid gap-4 lg:grid-cols-[1.1fr,2fr]">
        <aside class="grid gap-2 rounded-xl border border-slate-200 bg-slate-50 p-3">
          <p class="text-sm font-semibold text-slate-900">選擇訂單</p>
          {#if orders.length === 0}
            <p class="text-xs text-slate-600">尚無可查看的訂單。</p>
          {:else}
            {#each orders as order (order.orderId)}
              <button
                class={`rounded-lg border px-3 py-2 text-left text-sm ${selectedPayrollOrderId === order.orderId ? "border-cyan-700 bg-cyan-700 text-white" : "border-slate-300 bg-white text-slate-700"}`}
                type="button"
                onclick={() => {
                  selectedPayrollOrderId = order.orderId;
                  void loadPayrollLedger(order.orderId, true);
                }}
              >
                <div class="font-semibold">{order.orderId}</div>
                <div class={`text-xs ${selectedPayrollOrderId === order.orderId ? "text-cyan-100" : "text-slate-500"}`}>{friendlyStatus(order.status)} | {order.deliveryDate}</div>
              </button>
            {/each}
          {/if}
        </aside>

        <div class="grid gap-3 rounded-xl border border-slate-200 bg-white p-3">
          {#if !selectedPayrollOrderId}
            <p class="text-sm text-slate-600">選擇訂單後可查看薪資扣款明細。</p>
          {:else if payrollLoadingByOrderId[selectedPayrollOrderId]}
            <p class="text-sm text-slate-600">薪資明細載入中...</p>
          {:else if payrollErrorByOrderId[selectedPayrollOrderId]}
            <p class="text-sm text-rose-700">{payrollErrorByOrderId[selectedPayrollOrderId]}</p>
          {:else if selectedPayrollLedger}
            <article class="grid gap-3">
              <div class="rounded-lg border border-slate-200 bg-slate-50 p-3">
                <p class="text-xs text-slate-500">訂單 {selectedPayrollLedger.orderId}</p>
                <p class="text-sm text-slate-700">配送日：{selectedPayrollLedger.deliveryDate}</p>
                <p class="text-base font-semibold text-slate-900">淨扣款：{formatMoney(selectedPayrollLedger.currency, selectedPayrollLedger.netAmountMinor)}</p>
              </div>

              <div class="grid gap-2">
                <h4 class="text-sm font-semibold text-slate-900">薪資流水</h4>
                {#if selectedPayrollLedger.ledgerEntries.length === 0}
                  <p class="text-xs text-slate-600">目前沒有流水資料。</p>
                {:else}
                  {#each selectedPayrollLedger.ledgerEntries as entry (entry.ledgerEntryId)}
                    <div class="rounded-lg border border-slate-200 bg-white p-2 text-xs text-slate-700">
                      <p class="font-semibold">{entry.kind} | {formatMoney(entry.amount.currency, entry.amount.amountMinor)}</p>
                      <p>時間：{entry.occurredAt}</p>
                      <p>來源：{entry.sourceEventKind} ({entry.sourceEventReference})</p>
                    </div>
                  {/each}
                {/if}
              </div>

              <div class="grid gap-2 rounded-lg border border-slate-200 bg-slate-50 p-3">
                <h4 class="text-sm font-semibold text-slate-900">提交扣款申訴</h4>
                <textarea
                  class="min-h-20 rounded-lg border border-slate-300 px-3 py-2 text-sm"
                  placeholder="請輸入申訴原因"
                  value={disputeReasons[selectedPayrollLedger.orderId] ?? ""}
                  oninput={(event) =>
                    updateDisputeReason(
                      selectedPayrollLedger.orderId,
                      (event.currentTarget as HTMLTextAreaElement).value
                    )}
                ></textarea>
                <button
                  class="w-fit rounded-lg bg-rose-700 px-3 py-2 text-sm font-semibold text-white disabled:cursor-not-allowed disabled:bg-slate-300"
                  type="button"
                  disabled={disputeSubmittingByOrderId[selectedPayrollLedger.orderId] === true}
                  onclick={() => {
                    void submitDispute(selectedPayrollLedger.orderId);
                  }}
                >
                  {disputeSubmittingByOrderId[selectedPayrollLedger.orderId] ? "提交中..." : "送出申訴"}
                </button>
              </div>

              <div class="grid gap-2">
                <h4 class="text-sm font-semibold text-slate-900">申訴狀態追蹤</h4>
                {#if selectedPayrollLedger.disputes.length === 0}
                  <p class="text-xs text-slate-600">目前無申訴紀錄。</p>
                {:else}
                  {#each selectedPayrollLedger.disputes as dispute (dispute.disputeId)}
                    <article class="grid gap-2 rounded-lg border border-slate-200 bg-white p-2">
                      <div class="flex flex-wrap items-center justify-between gap-2">
                        <p class="text-xs font-semibold text-slate-900">{dispute.disputeId}</p>
                        <p class={`text-xs font-semibold ${isResolvedDisputeStatus(dispute.status) ? "text-emerald-700" : "text-amber-700"}`}>{friendlyStatus(dispute.status)}</p>
                      </div>
                      <p class="text-xs text-slate-600">負責人：{dispute.ownerActorId} | 更新：{dispute.updatedAt}</p>
                      <div class="grid gap-1">
                        {#each dispute.trace as event, index (`${dispute.disputeId}:${index}`)}
                          <div class="rounded border border-slate-200 bg-slate-50 px-2 py-1 text-[11px] text-slate-700">
                            <p>{event.occurredAt} | {event.actorId} | {friendlyStatus(event.status)}</p>
                            {#if event.note}
                              <p class="text-slate-600">備註：{event.note}</p>
                            {/if}
                          </div>
                        {/each}
                      </div>
                    </article>
                  {/each}
                {/if}
              </div>
            </article>
          {/if}
        </div>
      </div>
    </section>
  {/if}
</section>
