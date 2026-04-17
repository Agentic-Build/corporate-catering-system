<script lang="ts">
  import { browser } from "$app/environment";
  import { onDestroy, onMount } from "svelte";

  import { zhTW } from "$lib/i18n/zh-tw";
  import { apiClient, ensureApiClientConfigured } from "$lib/platform/api";
  import { normalizeApiFailure } from "$lib/platform/api/failure";

  interface Props {
    sectionId: string;
    actorDisplayName: string;
    actorId: string;
    provider: string;
    plantId: string | null;
    apiBearerToken: string | null;
  }

  let { sectionId, actorDisplayName, actorId, provider, plantId, apiBearerToken }: Props = $props();

  type VendorMenuPageResponse = Awaited<ReturnType<typeof apiClient.vendor.listVendorMenuItems>>;
  type VendorMenuItemView = VendorMenuPageResponse["items"][number];
  type VendorMenuItemStatus = VendorMenuItemView["status"];
  type VendorMenuUpsertRequest = Parameters<typeof apiClient.vendor.upsertVendorMenuItem>[1];
  type VendorOrderingPolicyView = Awaited<ReturnType<typeof apiClient.vendor.getVendorOrderingPolicy>>;
  type VendorFulfillmentBoardView = Awaited<ReturnType<typeof apiClient.vendor.listVendorFulfillmentBoard>>;
  type VendorFulfillmentOrderEntryView = VendorFulfillmentBoardView["orders"][number];
  type VendorDeliveryStatus = VendorFulfillmentOrderEntryView["deliveryStatus"];
  type VendorFulfillmentBatchView = Awaited<
    ReturnType<typeof apiClient.vendor.createVendorFulfillmentExportBatch>
  >;
  type VendorOrderPageResponse = Awaited<ReturnType<typeof apiClient.vendor.listVendorOrders>>;
  type VendorOrderView = VendorOrderPageResponse["items"][number];
  type VendorOrderStatus = VendorOrderView["status"];
  type UploadPlanRequest = Parameters<typeof apiClient.vendor.createVendorObjectStorageUploadPlan>[0];
  type UploadPlanResponse = Awaited<
    ReturnType<typeof apiClient.vendor.createVendorObjectStorageUploadPlan>
  >;
  type AccessLinkRequest = Parameters<typeof apiClient.vendor.createVendorObjectStorageAccessLink>[0];
  type AccessLinkResponse = Awaited<
    ReturnType<typeof apiClient.vendor.createVendorObjectStorageAccessLink>
  >;

  interface PortalNotification {
    id: number;
    kind: "success" | "error" | "info";
    message: string;
  }

  const sectionTitleById: Record<string, string> = {
    overview: zhTW.nav.sections.vendor.overview,
    fulfillment: zhTW.nav.sections.vendor.fulfillment,
    menu: zhTW.nav.sections.vendor.menu,
    docs: zhTW.nav.sections.vendor.docs
  };

  const sectionDescriptionById: Record<string, string> = {
    overview: zhTW.portal.vendor.sectionDescriptions.overview,
    fulfillment: zhTW.portal.vendor.sectionDescriptions.fulfillment,
    menu: zhTW.portal.vendor.sectionDescriptions.menu,
    docs: zhTW.portal.vendor.sectionDescriptions.docs
  };

  const menuStatusOptions: VendorMenuItemStatus[] = ["LISTED", "PAUSED", "DELISTED"];
  const deliveryStatusOptions: VendorDeliveryStatus[] = [
    "PENDING_PREP",
    "PREPARING",
    "PACKED",
    "OUT_FOR_DELIVERY",
    "DELIVERED",
    "CANCELLED"
  ];
  const orderStatusOptions: VendorOrderStatus[] = [
    "PENDING",
    "MODIFIED",
    "CANCELLED",
    "SOLD_OUT",
    "REFUND_PENDING",
    "REFUNDED",
    "FULFILLED"
  ];
  const menuTypeOptions = ["BENTO", "BOWL", "NOODLE", "SALAD", "SNACK", "DRINK"] as const;
  const healthTagOptions = [
    "LOW_CALORIE",
    "HIGH_PROTEIN",
    "VEGETARIAN",
    "VEGAN",
    "GLUTEN_FREE"
  ] as const;
  const artifactClassOptions: UploadPlanRequest["artifactClass"][] = [
    "COMPLIANCE_DOCUMENT",
    "MENU_IMAGE",
    "MENU_IMAGE_THUMBNAIL",
    "FULFILLMENT_DAILY_SUMMARY",
    "FULFILLMENT_PLANT_PARTITION_SHEET",
    "FULFILLMENT_LABELS",
    "FULFILLMENT_BASKET_LIST"
  ];

  const initialTaipeiDate = todayTaipeiIsoDate();

  let notifications = $state<PortalNotification[]>([]);
  let nextNotificationId = 1;
  const notificationTimeouts = new Set<ReturnType<typeof setTimeout>>();

  let menuItems = $state<VendorMenuItemView[]>([]);
  let menuPageMeta = $state<VendorMenuPageResponse["page"] | null>(null);
  let menuLoading = $state(false);
  let menuError = $state<string | null>(null);
  let menuFromDate = $state(initialTaipeiDate);
  let menuToDate = $state(addDaysIsoDate(initialTaipeiDate, 14));
  let menuStatusFilter = $state<"ALL" | VendorMenuItemStatus>("ALL");
  let menuSortOrder = $state<"asc" | "desc">("asc");
  let menuStatusUpdatingById = $state<Record<string, boolean>>({});

  let menuDraft = $state({
    menuItemId: generateMenuItemId(),
    deliveryDate: initialTaipeiDate,
    name: "",
    description: "",
    menuType: "BENTO" as VendorMenuUpsertRequest["menuType"],
    healthTagsCsv: "",
    imageUrl: "",
    currency: "TWD",
    amountMinor: 120,
    maxDailyQuantity: 30,
    preorderOpenDaysAheadOverride: "",
    modifyCancelCutoffMinuteOfDayOverride: ""
  });
  let menuDraftSubmitting = $state(false);

  let orderingPolicy = $state<VendorOrderingPolicyView | null>(null);
  let orderingPolicyLoading = $state(false);
  let orderingPolicySaving = $state(false);
  let orderingPolicyError = $state<string | null>(null);
  let orderingPolicyDraft = $state({
    preorderOpenDaysAhead: "",
    modifyCancelCutoffMinuteOfDay: ""
  });

  let fulfillmentBoard = $state<VendorFulfillmentBoardView | null>(null);
  let fulfillmentLoading = $state(false);
  let fulfillmentError = $state<string | null>(null);
  let fulfillmentDate = $state(initialTaipeiDate);
  let fulfillmentIncludeAuditTransitions = $state(true);
  let fulfillmentStatusDraftByOrderId = $state<Record<string, VendorDeliveryStatus>>({});
  let fulfillmentStatusSubmittingByOrderId = $state<Record<string, boolean>>({});

  let operationsOrders = $state<VendorOrderView[]>([]);
  let operationsPageMeta = $state<VendorOrderPageResponse["page"] | null>(null);
  let operationsLoading = $state(false);
  let operationsError = $state<string | null>(null);
  let operationsFromDate = $state(initialTaipeiDate);
  let operationsToDate = $state(addDaysIsoDate(initialTaipeiDate, 7));
  let operationsStatusFilter = $state<"ALL" | VendorOrderStatus>("ALL");
  let operationsSortOrder = $state<"asc" | "desc">("asc");

  let activeBatch = $state<VendorFulfillmentBatchView | null>(null);
  let recentBatchIds = $state<string[]>([]);
  let batchLookupId = $state("");
  let batchCreating = $state(false);
  let batchLookupLoading = $state(false);
  let batchLookupError = $state<string | null>(null);

  let uploadPlanDraft = $state({
    artifactClass: "COMPLIANCE_DOCUMENT" as UploadPlanRequest["artifactClass"],
    fileName: "vendor-compliance.pdf",
    mimeType: "application/pdf",
    sizeBytes: 180_000,
    thumbnailSizeBytes: "",
    locale: "zh-TW"
  });
  let uploadPlanLoading = $state(false);
  let uploadPlanError = $state<string | null>(null);
  let uploadPlanResult = $state<UploadPlanResponse | null>(null);

  let accessLinkDraft = $state({
    objectRef: "",
    locale: "zh-TW"
  });
  let accessLinkLoading = $state(false);
  let accessLinkError = $state<string | null>(null);
  let accessLinkResult = $state<AccessLinkResponse | null>(null);

  const showMenuSection = $derived(sectionId === "overview" || sectionId === "menu");
  const showFulfillmentSection = $derived(sectionId === "overview" || sectionId === "fulfillment");
  const showDocsSection = $derived(sectionId === "overview" || sectionId === "docs");

  const sectionTitle = $derived(sectionTitleById[sectionId] ?? sectionId);
  const sectionDescription = $derived(sectionDescriptionById[sectionId] ?? "");

  const fulfillmentOrderCount = $derived(fulfillmentBoard?.orders.length ?? 0);
  const fulfillmentPortionCount = $derived.by(() => {
    if (!fulfillmentBoard) {
      return 0;
    }
    let total = 0;
    for (const order of fulfillmentBoard.orders) {
      for (const lineItem of order.lineItems) {
        total += lineItem.quantity;
      }
    }
    return total;
  });

  onMount(() => {
    void bootstrapPortal();
  });

  onDestroy(() => {
    for (const timeout of notificationTimeouts) {
      clearTimeout(timeout);
    }
    notificationTimeouts.clear();
  });

  async function bootstrapPortal() {
    if (!plantId) {
      const message = "目前登入帳號沒有可用的廠區範圍，無法載入商家作業資料。";
      menuError = message;
      fulfillmentError = message;
      operationsError = message;
      orderingPolicyError = message;
      pushNotification("error", message);
      return;
    }

    try {
      ensureApiClientConfigured(apiBearerToken);
    } catch (error) {
      const failure = normalizeApiFailure(error);
      const message = failure.localizedMessage;
      menuError = message;
      fulfillmentError = message;
      operationsError = message;
      orderingPolicyError = message;
      pushNotification("error", message);
      return;
    }

    await Promise.all([
      refreshMenuItems(false),
      refreshOrderingPolicy(false),
      refreshFulfillmentBoard(false),
      refreshOperationsOrders(false)
    ]);
  }

  async function refreshMenuItems(notifyOnError: boolean) {
    if (menuLoading) {
      return;
    }

    menuLoading = true;
    menuError = null;
    try {
      const page = await apiClient.vendor.listVendorMenuItems(
        menuFromDate,
        menuToDate,
        menuStatusFilter === "ALL" ? undefined : menuStatusFilter,
        1,
        200,
        menuSortOrder
      );
      menuItems = page.items;
      menuPageMeta = page.page;
    } catch (error) {
      const failure = normalizeApiFailure(error);
      menuError = failure.localizedMessage;
      if (notifyOnError) {
        pushNotification("error", failure.localizedMessage);
      }
    } finally {
      menuLoading = false;
    }
  }

  async function submitMenuDraft() {
    if (menuDraftSubmitting) {
      return;
    }

    const menuItemId = menuDraft.menuItemId.trim();
    if (menuItemId.length === 0) {
      pushNotification("error", "請先填寫 menuItemId。");
      return;
    }

    const name = menuDraft.name.trim();
    const description = menuDraft.description.trim();
    if (name.length === 0 || description.length === 0) {
      pushNotification("error", "菜單名稱與描述不可為空。");
      return;
    }

    const healthTags = parseHealthTagsInput(menuDraft.healthTagsCsv);
    if (healthTags.error) {
      pushNotification("error", healthTags.error);
      return;
    }

    menuDraftSubmitting = true;
    try {
      await apiClient.vendor.upsertVendorMenuItem(menuItemId, {
        deliveryDate: menuDraft.deliveryDate,
        name,
        description,
        menuType: menuDraft.menuType,
        healthTags: healthTags.value,
        imageUrl: normalizeOptional(menuDraft.imageUrl) ?? undefined,
        price: {
          currency: menuDraft.currency.trim().toUpperCase(),
          amountMinor: menuDraft.amountMinor
        },
        maxDailyQuantity: menuDraft.maxDailyQuantity,
        preorderOpenDaysAheadOverride: parseOptionalPositiveInt(
          menuDraft.preorderOpenDaysAheadOverride
        ),
        modifyCancelCutoffMinuteOfDayOverride: parseOptionalPositiveInt(
          menuDraft.modifyCancelCutoffMinuteOfDayOverride
        )
      });
      pushNotification("success", `菜單 ${menuItemId} 已更新。`);
      await Promise.all([refreshMenuItems(false), refreshFulfillmentBoard(false)]);
    } catch (error) {
      pushNotification("error", normalizeApiFailure(error).localizedMessage);
    } finally {
      menuDraftSubmitting = false;
    }
  }

  async function updateMenuItemStatus(item: VendorMenuItemView, status: VendorMenuItemStatus) {
    if (menuStatusUpdatingById[item.menuItemId]) {
      return;
    }
    if (item.status === status) {
      return;
    }

    menuStatusUpdatingById = {
      ...menuStatusUpdatingById,
      [item.menuItemId]: true
    };
    try {
      await apiClient.vendor.updateVendorMenuItemStatus(item.menuItemId, { status });
      pushNotification("success", `菜單 ${item.menuItemId} 已切換為 ${menuStatusLabel(status)}。`);
      await refreshMenuItems(false);
    } catch (error) {
      pushNotification("error", normalizeApiFailure(error).localizedMessage);
    } finally {
      menuStatusUpdatingById = {
        ...menuStatusUpdatingById,
        [item.menuItemId]: false
      };
    }
  }

  function loadMenuItemIntoDraft(item: VendorMenuItemView) {
    menuDraft.menuItemId = item.menuItemId;
    menuDraft.deliveryDate = item.deliveryDate;
    menuDraft.name = item.name;
    menuDraft.description = item.description;
    menuDraft.menuType = item.menuType;
    menuDraft.healthTagsCsv = item.healthTags.join(", ");
    menuDraft.imageUrl = item.imageUrl ?? "";
    menuDraft.currency = item.price.currency;
    menuDraft.amountMinor = item.price.amountMinor;
    menuDraft.maxDailyQuantity = item.maxDailyQuantity;
    menuDraft.preorderOpenDaysAheadOverride = String(item.preorderOpenDaysAhead);
    menuDraft.modifyCancelCutoffMinuteOfDayOverride = String(
      item.modifyCancelCutoffMinuteOfDay
    );
  }

  function resetMenuDraft() {
    menuDraft = {
      menuItemId: generateMenuItemId(),
      deliveryDate: initialTaipeiDate,
      name: "",
      description: "",
      menuType: "BENTO",
      healthTagsCsv: "",
      imageUrl: "",
      currency: "TWD",
      amountMinor: 120,
      maxDailyQuantity: 30,
      preorderOpenDaysAheadOverride: "",
      modifyCancelCutoffMinuteOfDayOverride: ""
    };
  }

  async function refreshOrderingPolicy(notifyOnError: boolean) {
    if (orderingPolicyLoading) {
      return;
    }
    orderingPolicyLoading = true;
    orderingPolicyError = null;
    try {
      const policy = await apiClient.vendor.getVendorOrderingPolicy();
      orderingPolicy = policy;
      orderingPolicyDraft.preorderOpenDaysAhead = String(policy.preorderOpenDaysAhead);
      orderingPolicyDraft.modifyCancelCutoffMinuteOfDay = String(
        policy.modifyCancelCutoffMinuteOfDay
      );
    } catch (error) {
      const failure = normalizeApiFailure(error);
      orderingPolicyError = failure.localizedMessage;
      if (notifyOnError) {
        pushNotification("error", failure.localizedMessage);
      }
    } finally {
      orderingPolicyLoading = false;
    }
  }

  async function saveOrderingPolicy() {
    if (orderingPolicySaving) {
      return;
    }

    orderingPolicySaving = true;
    orderingPolicyError = null;
    try {
      const policy = await apiClient.vendor.upsertVendorOrderingPolicy({
        preorderOpenDaysAhead: parseOptionalPositiveInt(orderingPolicyDraft.preorderOpenDaysAhead),
        modifyCancelCutoffMinuteOfDay: parseOptionalPositiveInt(
          orderingPolicyDraft.modifyCancelCutoffMinuteOfDay
        )
      });
      orderingPolicy = policy;
      orderingPolicyDraft.preorderOpenDaysAhead = String(policy.preorderOpenDaysAhead);
      orderingPolicyDraft.modifyCancelCutoffMinuteOfDay = String(
        policy.modifyCancelCutoffMinuteOfDay
      );
      pushNotification("success", "訂購視窗設定已更新。");
    } catch (error) {
      const failure = normalizeApiFailure(error);
      orderingPolicyError = failure.localizedMessage;
      pushNotification("error", failure.localizedMessage);
    } finally {
      orderingPolicySaving = false;
    }
  }

  async function refreshFulfillmentBoard(notifyOnError: boolean) {
    if (fulfillmentLoading) {
      return;
    }
    fulfillmentLoading = true;
    fulfillmentError = null;
    try {
      const board = await apiClient.vendor.listVendorFulfillmentBoard(
        fulfillmentDate,
        plantId ?? undefined,
        fulfillmentIncludeAuditTransitions
      );
      fulfillmentBoard = board;
      hydrateFulfillmentStatusDraft(board.orders);
    } catch (error) {
      const failure = normalizeApiFailure(error);
      fulfillmentError = failure.localizedMessage;
      if (notifyOnError) {
        pushNotification("error", failure.localizedMessage);
      }
    } finally {
      fulfillmentLoading = false;
    }
  }

  function hydrateFulfillmentStatusDraft(orders: VendorFulfillmentOrderEntryView[]) {
    const nextDraft = { ...fulfillmentStatusDraftByOrderId };
    for (const order of orders) {
      if (!nextDraft[order.orderId]) {
        nextDraft[order.orderId] = nextDeliveryStatus(order.deliveryStatus);
      }
    }
    for (const orderId of Object.keys(nextDraft)) {
      if (!orders.some((order) => order.orderId === orderId)) {
        delete nextDraft[orderId];
      }
    }
    fulfillmentStatusDraftByOrderId = nextDraft;
  }

  async function advanceDeliveryStatus(order: VendorFulfillmentOrderEntryView) {
    if (fulfillmentStatusSubmittingByOrderId[order.orderId]) {
      return;
    }

    const toStatus =
      fulfillmentStatusDraftByOrderId[order.orderId] ?? nextDeliveryStatus(order.deliveryStatus);
    if (toStatus === order.deliveryStatus) {
      pushNotification("info", `訂單 ${order.orderId} 已是 ${deliveryStatusLabel(toStatus)}。`);
      return;
    }

    fulfillmentStatusSubmittingByOrderId = {
      ...fulfillmentStatusSubmittingByOrderId,
      [order.orderId]: true
    };
    try {
      const result = await apiClient.vendor.advanceVendorFulfillmentDeliveryStatus(order.orderId, {
        toStatus,
        occurredAt: currentTaipeiContractDateTime()
      });
      pushNotification(
        "success",
        `訂單 ${result.orderId} 已由 ${deliveryStatusLabel(result.fromStatus)} 轉為 ${deliveryStatusLabel(result.toStatus)}。`
      );
      await Promise.all([refreshFulfillmentBoard(false), refreshOperationsOrders(false)]);
    } catch (error) {
      pushNotification("error", normalizeApiFailure(error).localizedMessage);
    } finally {
      fulfillmentStatusSubmittingByOrderId = {
        ...fulfillmentStatusSubmittingByOrderId,
        [order.orderId]: false
      };
    }
  }

  async function createExportBatch() {
    if (batchCreating) {
      return;
    }
    batchCreating = true;
    batchLookupError = null;
    try {
      const batch = await apiClient.vendor.createVendorFulfillmentExportBatch({
        deliveryDate: fulfillmentDate
      });
      activeBatch = batch;
      batchLookupId = batch.batchId;
      recentBatchIds = [batch.batchId, ...recentBatchIds.filter((id) => id !== batch.batchId)].slice(
        0,
        12
      );
      pushNotification("success", `履約批次已建立：${batch.batchId}`);
    } catch (error) {
      const failure = normalizeApiFailure(error);
      batchLookupError = failure.localizedMessage;
      pushNotification("error", failure.localizedMessage);
    } finally {
      batchCreating = false;
    }
  }

  async function loadBatchById() {
    if (batchLookupLoading) {
      return;
    }
    const batchId = batchLookupId.trim();
    if (!batchId) {
      batchLookupError = "請輸入批次編號。";
      return;
    }

    batchLookupLoading = true;
    batchLookupError = null;
    try {
      const batch = await apiClient.vendor.getVendorFulfillmentExportBatch(batchId);
      activeBatch = batch;
      recentBatchIds = [batch.batchId, ...recentBatchIds.filter((id) => id !== batch.batchId)].slice(
        0,
        12
      );
    } catch (error) {
      const failure = normalizeApiFailure(error);
      batchLookupError = failure.localizedMessage;
      pushNotification("error", failure.localizedMessage);
    } finally {
      batchLookupLoading = false;
    }
  }

  function printBatch() {
    if (!activeBatch || !browser) {
      return;
    }
    window.print();
  }

  async function refreshOperationsOrders(notifyOnError: boolean) {
    if (!plantId || operationsLoading) {
      return;
    }
    operationsLoading = true;
    operationsError = null;
    try {
      const page = await apiClient.vendor.listVendorOrders(
        plantId,
        operationsFromDate,
        operationsToDate,
        1,
        200,
        "deliveryDate",
        operationsSortOrder,
        operationsStatusFilter === "ALL" ? undefined : operationsStatusFilter
      );
      operationsOrders = page.items;
      operationsPageMeta = page.page;
    } catch (error) {
      const failure = normalizeApiFailure(error);
      operationsError = failure.localizedMessage;
      if (notifyOnError) {
        pushNotification("error", failure.localizedMessage);
      }
    } finally {
      operationsLoading = false;
    }
  }

  async function createUploadPlan() {
    if (uploadPlanLoading) {
      return;
    }
    uploadPlanLoading = true;
    uploadPlanError = null;
    try {
      const thumbnailSizeBytes = parseOptionalPositiveInt(uploadPlanDraft.thumbnailSizeBytes);
      const locale = normalizeOptional(uploadPlanDraft.locale);
      const response = await apiClient.vendor.createVendorObjectStorageUploadPlan({
        artifactClass: uploadPlanDraft.artifactClass,
        fileName: uploadPlanDraft.fileName.trim(),
        mimeType: uploadPlanDraft.mimeType.trim(),
        sizeBytes: uploadPlanDraft.sizeBytes,
        thumbnailSizeBytes,
        locale: locale ?? undefined
      });
      uploadPlanResult = response;
      accessLinkDraft.objectRef = response.primary.objectRef;
      pushNotification("success", "已建立上傳計畫，可直接用 objectRef 追蹤。");
    } catch (error) {
      const failure = normalizeApiFailure(error);
      uploadPlanError = failure.localizedMessage;
      pushNotification("error", failure.localizedMessage);
    } finally {
      uploadPlanLoading = false;
    }
  }

  async function createAccessLink() {
    if (accessLinkLoading) {
      return;
    }

    const objectRef = accessLinkDraft.objectRef.trim();
    if (!objectRef) {
      accessLinkError = "請輸入 objectRef。";
      return;
    }

    accessLinkLoading = true;
    accessLinkError = null;
    try {
      const locale = normalizeOptional(accessLinkDraft.locale);
      const request: AccessLinkRequest = {
        objectRef,
        locale: locale ?? undefined
      };
      const response = await apiClient.vendor.createVendorObjectStorageAccessLink(request);
      accessLinkResult = response;
      pushNotification("success", "下載連結已產生。");
    } catch (error) {
      const failure = normalizeApiFailure(error);
      accessLinkError = failure.localizedMessage;
      pushNotification("error", failure.localizedMessage);
    } finally {
      accessLinkLoading = false;
    }
  }

  function pushNotification(kind: PortalNotification["kind"], message: string) {
    const notification = {
      id: nextNotificationId++,
      kind,
      message
    } satisfies PortalNotification;
    notifications = [notification, ...notifications].slice(0, 6);
    const timeout = setTimeout(() => {
      notifications = notifications.filter((entry) => entry.id !== notification.id);
      notificationTimeouts.delete(timeout);
    }, 6000);
    notificationTimeouts.add(timeout);
  }

  function menuStatusLabel(status: VendorMenuItemStatus): string {
    switch (status) {
      case "LISTED":
        return "上架";
      case "PAUSED":
        return "暫停";
      case "DELISTED":
        return "下架";
    }
  }

  function deliveryStatusLabel(status: VendorDeliveryStatus): string {
    switch (status) {
      case "PENDING_PREP":
        return "待備餐";
      case "PREPARING":
        return "備餐中";
      case "PACKED":
        return "已打包";
      case "OUT_FOR_DELIVERY":
        return "配送中";
      case "DELIVERED":
        return "已送達";
      case "CANCELLED":
        return "已取消";
    }
  }

  function orderStatusLabel(status: VendorOrderStatus): string {
    switch (status) {
      case "PENDING":
        return "待處理";
      case "MODIFIED":
        return "已修改";
      case "CANCELLED":
        return "已取消";
      case "SOLD_OUT":
        return "售罄";
      case "REFUND_PENDING":
        return "退款中";
      case "REFUNDED":
        return "已退款";
      case "FULFILLED":
        return "已履約";
    }
  }

  function nextDeliveryStatus(status: VendorDeliveryStatus): VendorDeliveryStatus {
    switch (status) {
      case "PENDING_PREP":
        return "PREPARING";
      case "PREPARING":
        return "PACKED";
      case "PACKED":
        return "OUT_FOR_DELIVERY";
      case "OUT_FOR_DELIVERY":
        return "DELIVERED";
      case "DELIVERED":
      case "CANCELLED":
        return status;
    }
  }

  function formatPrice(currency: string, amountMinor: number): string {
    const major = amountMinor / 100;
    return `${currency} ${major.toLocaleString("zh-TW", {
      minimumFractionDigits: 0,
      maximumFractionDigits: 2
    })}`;
  }

  function formatDateTime(value: string): string {
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) {
      return value;
    }
    return date.toLocaleString("zh-TW", { hour12: false, timeZone: "Asia/Taipei" });
  }

  function currentTaipeiContractDateTime(): string {
    const taipeiOffsetMinutes = 8 * 60;
    const taipeiDate = new Date(Date.now() + taipeiOffsetMinutes * 60_000);
    const year = taipeiDate.getUTCFullYear();
    const month = String(taipeiDate.getUTCMonth() + 1).padStart(2, "0");
    const day = String(taipeiDate.getUTCDate()).padStart(2, "0");
    const hour = String(taipeiDate.getUTCHours()).padStart(2, "0");
    const minute = String(taipeiDate.getUTCMinutes()).padStart(2, "0");
    const second = String(taipeiDate.getUTCSeconds()).padStart(2, "0");
    return `${year}-${month}-${day}T${hour}:${minute}:${second}+08:00`;
  }

  function normalizeOptional(value: string): string | null {
    const trimmed = value.trim();
    return trimmed.length === 0 ? null : trimmed;
  }

  function parseOptionalPositiveInt(value: string): number | undefined {
    const trimmed = value.trim();
    if (!trimmed) {
      return undefined;
    }
    const parsed = Number.parseInt(trimmed, 10);
    if (!Number.isFinite(parsed) || parsed <= 0) {
      throw new Error(`invalid positive integer: ${value}`);
    }
    return parsed;
  }

  function parseHealthTagsInput(input: string): { value: VendorMenuUpsertRequest["healthTags"]; error?: string } {
    const trimmed = input.trim();
    if (!trimmed) {
      return { value: [] };
    }
    const values = trimmed
      .split(",")
      .map((entry) => entry.trim().toUpperCase())
      .filter((entry) => entry.length > 0);
    const deduped = [...new Set(values)];
    for (const value of deduped) {
      if (!healthTagOptions.includes(value as (typeof healthTagOptions)[number])) {
        return {
          value: [],
          error: `健康標籤 ${value} 不支援，請使用 ${healthTagOptions.join(", ")}。`
        };
      }
    }
    return { value: deduped as VendorMenuUpsertRequest["healthTags"] };
  }

  function generateMenuItemId(): string {
    const seed = `${Date.now().toString(36)}${Math.random().toString(36).slice(2, 10)}`;
    return `menu-${seed}`.slice(0, 32);
  }

  function todayTaipeiIsoDate(): string {
    const parts = new Intl.DateTimeFormat("en-CA", {
      timeZone: "Asia/Taipei",
      year: "numeric",
      month: "2-digit",
      day: "2-digit"
    }).formatToParts(new Date());
    const year = parts.find((part) => part.type === "year")?.value ?? "1970";
    const month = parts.find((part) => part.type === "month")?.value ?? "01";
    const day = parts.find((part) => part.type === "day")?.value ?? "01";
    return `${year}-${month}-${day}`;
  }

  function addDaysIsoDate(date: string, dayOffset: number): string {
    const base = new Date(`${date}T00:00:00.000Z`);
    base.setUTCDate(base.getUTCDate() + dayOffset);
    return base.toISOString().slice(0, 10);
  }
</script>

<section class="grid gap-5">
  <header class="grid gap-3 rounded-xl border border-cyan-100 bg-cyan-50/80 p-4">
    <p class="text-xs font-semibold tracking-[0.14em] text-cyan-900">{zhTW.nav.portals.vendor}</p>
    <h2 class="text-2xl font-bold text-slate-950">{sectionTitle}</h2>
    <p class="text-sm text-slate-700">{sectionDescription}</p>
    <dl class="grid gap-2 text-xs text-slate-600 md:grid-cols-4">
      <div>
        <dt class="font-semibold text-slate-500">商家執行身分</dt>
        <dd>{actorDisplayName} ({actorId})</dd>
      </div>
      <div>
        <dt class="font-semibold text-slate-500">驗證供應商</dt>
        <dd>{provider}</dd>
      </div>
      <div>
        <dt class="font-semibold text-slate-500">廠區範圍</dt>
        <dd>{plantId ?? "-"}</dd>
      </div>
      <div>
        <dt class="font-semibold text-slate-500">體驗模式</dt>
        <dd>Desktop-first / Tablet-safe</dd>
      </div>
    </dl>
  </header>

  {#if notifications.length > 0}
    <ul class="grid gap-2">
      {#each notifications as notification (notification.id)}
        <li
          class={`rounded-lg border px-3 py-2 text-sm ${
            notification.kind === "success"
              ? "border-emerald-200 bg-emerald-50 text-emerald-900"
              : notification.kind === "error"
                ? "border-rose-200 bg-rose-50 text-rose-900"
                : "border-slate-200 bg-slate-50 text-slate-900"
          }`}
        >
          {notification.message}
        </li>
      {/each}
    </ul>
  {/if}

  {#if showMenuSection}
    <section class="grid gap-4 rounded-xl border border-slate-200 bg-white p-4 shadow-sm">
      <div class="flex flex-wrap items-center justify-between gap-2">
        <h3 class="text-lg font-semibold text-slate-900">菜單與訂購視窗</h3>
        <div class="flex flex-wrap gap-2 text-xs">
          <button
            type="button"
            class="rounded-md border border-slate-300 px-3 py-1.5 font-medium text-slate-700 hover:border-slate-500"
            onclick={() => refreshMenuItems(true)}
            disabled={menuLoading}
          >
            {menuLoading ? "載入菜單中..." : "重新載入菜單"}
          </button>
          <button
            type="button"
            class="rounded-md border border-slate-300 px-3 py-1.5 font-medium text-slate-700 hover:border-slate-500"
            onclick={() => refreshOrderingPolicy(true)}
            disabled={orderingPolicyLoading}
          >
            {orderingPolicyLoading ? "載入視窗設定中..." : "重新載入視窗設定"}
          </button>
        </div>
      </div>

      <div class="grid gap-3 rounded-lg border border-slate-200 bg-slate-50 p-3 lg:grid-cols-6">
        <label class="grid gap-1 text-xs text-slate-700">
          <span>起始日</span>
          <input
            class="rounded border border-slate-300 bg-white px-2 py-1.5"
            type="date"
            bind:value={menuFromDate}
          />
        </label>
        <label class="grid gap-1 text-xs text-slate-700">
          <span>結束日</span>
          <input
            class="rounded border border-slate-300 bg-white px-2 py-1.5"
            type="date"
            bind:value={menuToDate}
          />
        </label>
        <label class="grid gap-1 text-xs text-slate-700">
          <span>狀態</span>
          <select class="rounded border border-slate-300 bg-white px-2 py-1.5" bind:value={menuStatusFilter}>
            <option value="ALL">全部</option>
            {#each menuStatusOptions as status}
              <option value={status}>{menuStatusLabel(status)}</option>
            {/each}
          </select>
        </label>
        <label class="grid gap-1 text-xs text-slate-700">
          <span>排序</span>
          <select class="rounded border border-slate-300 bg-white px-2 py-1.5" bind:value={menuSortOrder}>
            <option value="asc">由舊到新</option>
            <option value="desc">由新到舊</option>
          </select>
        </label>
        <div class="grid items-end">
          <button
            type="button"
            class="rounded-md bg-slate-900 px-3 py-2 text-xs font-semibold text-white hover:bg-slate-800 disabled:opacity-60"
            onclick={() => refreshMenuItems(true)}
            disabled={menuLoading}
          >
            {menuLoading ? "套用篩選中..." : "套用篩選"}
          </button>
        </div>
        <div class="grid items-end text-xs text-slate-600">
          <span>共 {menuPageMeta?.totalItems ?? 0} 筆</span>
        </div>
      </div>

      {#if menuError}
        <p class="rounded-md border border-rose-200 bg-rose-50 px-3 py-2 text-sm text-rose-800">{menuError}</p>
      {/if}

      <div class="overflow-x-auto rounded-lg border border-slate-200">
        <table class="min-w-full text-sm">
          <thead class="bg-slate-50 text-left text-xs font-semibold tracking-wide text-slate-600">
            <tr>
              <th class="px-3 py-2">菜單</th>
              <th class="px-3 py-2">日期</th>
              <th class="px-3 py-2">價格</th>
              <th class="px-3 py-2">供應</th>
              <th class="px-3 py-2">狀態</th>
              <th class="px-3 py-2">操作</th>
            </tr>
          </thead>
          <tbody>
            {#if menuItems.length === 0}
              <tr>
                <td class="px-3 py-4 text-slate-500" colspan="6">目前篩選下沒有菜單資料。</td>
              </tr>
            {:else}
              {#each menuItems as item (item.menuItemId)}
                <tr class="border-t border-slate-100">
                  <td class="px-3 py-2 align-top">
                    <p class="font-semibold text-slate-900">{item.name}</p>
                    <p class="text-xs text-slate-600">{item.menuItemId}</p>
                    <p class="mt-1 text-xs text-slate-600 line-clamp-2">{item.description}</p>
                  </td>
                  <td class="px-3 py-2 align-top">{item.deliveryDate}</td>
                  <td class="px-3 py-2 align-top">{formatPrice(item.price.currency, item.price.amountMinor)}</td>
                  <td class="px-3 py-2 align-top">
                    <p class="text-xs text-slate-700">上限 {item.maxDailyQuantity}</p>
                    <p class="text-xs text-slate-700">剩餘 {item.remainingQuantity}</p>
                  </td>
                  <td class="px-3 py-2 align-top">
                    <span class="inline-flex rounded-full border border-slate-300 bg-slate-50 px-2 py-1 text-xs">
                      {menuStatusLabel(item.status)}
                    </span>
                  </td>
                  <td class="px-3 py-2 align-top">
                    <div class="grid gap-1">
                      <div class="flex flex-wrap gap-1">
                        {#each menuStatusOptions as status}
                          <button
                            type="button"
                            class={`rounded border px-2 py-1 text-xs ${
                              item.status === status
                                ? "border-emerald-500 bg-emerald-50 text-emerald-800"
                                : "border-slate-300 bg-white text-slate-700 hover:border-slate-500"
                            }`}
                            onclick={() => updateMenuItemStatus(item, status)}
                            disabled={menuStatusUpdatingById[item.menuItemId] === true}
                          >
                            {menuStatusLabel(status)}
                          </button>
                        {/each}
                      </div>
                      <button
                        type="button"
                        class="justify-self-start rounded border border-slate-300 px-2 py-1 text-xs text-slate-700 hover:border-slate-500"
                        onclick={() => loadMenuItemIntoDraft(item)}
                      >
                        載入到表單
                      </button>
                    </div>
                  </td>
                </tr>
              {/each}
            {/if}
          </tbody>
        </table>
      </div>

      <div class="grid gap-4 lg:grid-cols-2">
        <article class="grid gap-3 rounded-lg border border-slate-200 bg-slate-50 p-3">
          <div class="flex items-center justify-between">
            <h4 class="text-sm font-semibold text-slate-900">菜單建立 / 更新</h4>
            <button
              type="button"
              class="rounded border border-slate-300 px-2 py-1 text-xs text-slate-700 hover:border-slate-500"
              onclick={resetMenuDraft}
            >
              清空表單
            </button>
          </div>
          <div class="grid gap-2 md:grid-cols-2">
            <label class="grid gap-1 text-xs">
              <span>menuItemId</span>
              <input class="rounded border border-slate-300 bg-white px-2 py-1.5" bind:value={menuDraft.menuItemId} />
            </label>
            <label class="grid gap-1 text-xs">
              <span>配送日</span>
              <input
                class="rounded border border-slate-300 bg-white px-2 py-1.5"
                type="date"
                bind:value={menuDraft.deliveryDate}
              />
            </label>
            <label class="grid gap-1 text-xs md:col-span-2">
              <span>名稱</span>
              <input class="rounded border border-slate-300 bg-white px-2 py-1.5" bind:value={menuDraft.name} />
            </label>
            <label class="grid gap-1 text-xs md:col-span-2">
              <span>描述</span>
              <textarea
                class="rounded border border-slate-300 bg-white px-2 py-1.5"
                rows="2"
                bind:value={menuDraft.description}
              ></textarea>
            </label>
            <label class="grid gap-1 text-xs">
              <span>餐點類型</span>
              <select class="rounded border border-slate-300 bg-white px-2 py-1.5" bind:value={menuDraft.menuType}>
                {#each menuTypeOptions as menuType}
                  <option value={menuType}>{menuType}</option>
                {/each}
              </select>
            </label>
            <label class="grid gap-1 text-xs">
              <span>健康標籤（逗號分隔）</span>
              <input
                class="rounded border border-slate-300 bg-white px-2 py-1.5"
                placeholder={healthTagOptions.join(", ")}
                bind:value={menuDraft.healthTagsCsv}
              />
            </label>
            <label class="grid gap-1 text-xs md:col-span-2">
              <span>圖片 URL（選填）</span>
              <input class="rounded border border-slate-300 bg-white px-2 py-1.5" bind:value={menuDraft.imageUrl} />
            </label>
            <label class="grid gap-1 text-xs">
              <span>幣別</span>
              <input class="rounded border border-slate-300 bg-white px-2 py-1.5" bind:value={menuDraft.currency} />
            </label>
            <label class="grid gap-1 text-xs">
              <span>金額（minor）</span>
              <input
                class="rounded border border-slate-300 bg-white px-2 py-1.5"
                type="number"
                min="0"
                bind:value={menuDraft.amountMinor}
              />
            </label>
            <label class="grid gap-1 text-xs">
              <span>每日上限</span>
              <input
                class="rounded border border-slate-300 bg-white px-2 py-1.5"
                type="number"
                min="1"
                max="2000"
                bind:value={menuDraft.maxDailyQuantity}
              />
            </label>
            <label class="grid gap-1 text-xs">
              <span>預購開放天數 override（選填）</span>
              <input
                class="rounded border border-slate-300 bg-white px-2 py-1.5"
                type="number"
                min="1"
                max="7"
                bind:value={menuDraft.preorderOpenDaysAheadOverride}
              />
            </label>
            <label class="grid gap-1 text-xs">
              <span>前日截單分鐘 override（選填）</span>
              <input
                class="rounded border border-slate-300 bg-white px-2 py-1.5"
                type="number"
                min="900"
                max="1200"
                bind:value={menuDraft.modifyCancelCutoffMinuteOfDayOverride}
              />
            </label>
          </div>
          <button
            type="button"
            class="justify-self-start rounded-md bg-slate-900 px-3 py-2 text-xs font-semibold text-white hover:bg-slate-800 disabled:opacity-60"
            onclick={submitMenuDraft}
            disabled={menuDraftSubmitting}
          >
            {menuDraftSubmitting ? "送出中..." : "送出菜單更新"}
          </button>
        </article>

        <article class="grid gap-3 rounded-lg border border-slate-200 bg-slate-50 p-3">
          <h4 class="text-sm font-semibold text-slate-900">訂單窗口管理</h4>
          {#if orderingPolicyError}
            <p class="rounded border border-rose-200 bg-rose-50 px-2 py-1.5 text-xs text-rose-800">
              {orderingPolicyError}
            </p>
          {/if}
          {#if orderingPolicy}
            <dl class="grid gap-1 text-xs text-slate-700">
              <div class="flex justify-between gap-2">
                <dt>目前預購開放天數</dt>
                <dd class="font-semibold">{orderingPolicy.preorderOpenDaysAhead}</dd>
              </div>
              <div class="flex justify-between gap-2">
                <dt>目前前日截單分鐘</dt>
                <dd class="font-semibold">{orderingPolicy.modifyCancelCutoffMinuteOfDay}</dd>
              </div>
            </dl>
          {/if}
          <div class="grid gap-2 md:grid-cols-2">
            <label class="grid gap-1 text-xs">
              <span>預購開放天數</span>
              <input
                class="rounded border border-slate-300 bg-white px-2 py-1.5"
                type="number"
                min="1"
                max="7"
                bind:value={orderingPolicyDraft.preorderOpenDaysAhead}
              />
            </label>
            <label class="grid gap-1 text-xs">
              <span>前日截單分鐘（09:00-20:00）</span>
              <input
                class="rounded border border-slate-300 bg-white px-2 py-1.5"
                type="number"
                min="900"
                max="1200"
                bind:value={orderingPolicyDraft.modifyCancelCutoffMinuteOfDay}
              />
            </label>
          </div>
          <button
            type="button"
            class="justify-self-start rounded-md bg-emerald-700 px-3 py-2 text-xs font-semibold text-white hover:bg-emerald-600 disabled:opacity-60"
            onclick={saveOrderingPolicy}
            disabled={orderingPolicySaving}
          >
            {orderingPolicySaving ? "更新中..." : "更新訂購視窗"}
          </button>
        </article>
      </div>
    </section>
  {/if}

  {#if showFulfillmentSection}
    <section class="grid gap-4 rounded-xl border border-slate-200 bg-white p-4 shadow-sm">
      <div class="flex flex-wrap items-center justify-between gap-2">
        <h3 class="text-lg font-semibold text-slate-900">履約看板與配送執行</h3>
        <div class="flex flex-wrap gap-2 text-xs">
          <button
            type="button"
            class="rounded-md border border-slate-300 px-3 py-1.5 font-medium text-slate-700 hover:border-slate-500"
            onclick={() => refreshFulfillmentBoard(true)}
            disabled={fulfillmentLoading}
          >
            {fulfillmentLoading ? "載入看板中..." : "重新載入看板"}
          </button>
          <button
            type="button"
            class="rounded-md bg-slate-900 px-3 py-1.5 font-medium text-white hover:bg-slate-800 disabled:opacity-60"
            onclick={createExportBatch}
            disabled={batchCreating}
          >
            {batchCreating ? "建立中..." : "建立可列印批次"}
          </button>
        </div>
      </div>

      <div class="grid gap-3 rounded-lg border border-slate-200 bg-slate-50 p-3 lg:grid-cols-5">
        <label class="grid gap-1 text-xs text-slate-700">
          <span>配送日</span>
          <input
            class="rounded border border-slate-300 bg-white px-2 py-1.5"
            type="date"
            bind:value={fulfillmentDate}
          />
        </label>
        <label class="grid gap-1 text-xs text-slate-700">
          <span>Plant</span>
          <input
            class="rounded border border-slate-300 bg-slate-100 px-2 py-1.5 text-slate-500"
            value={plantId ?? "-"}
            readonly
          />
        </label>
        <label class="flex items-end gap-2 text-xs text-slate-700 lg:col-span-2">
          <input type="checkbox" bind:checked={fulfillmentIncludeAuditTransitions} />
          <span>含配送狀態稽核軌跡</span>
        </label>
        <button
          type="button"
          class="rounded-md bg-slate-900 px-3 py-2 text-xs font-semibold text-white hover:bg-slate-800 disabled:opacity-60"
          onclick={() => refreshFulfillmentBoard(true)}
          disabled={fulfillmentLoading}
        >
          {fulfillmentLoading ? "套用篩選中..." : "套用篩選"}
        </button>
      </div>

      {#if fulfillmentError}
        <p class="rounded-md border border-rose-200 bg-rose-50 px-3 py-2 text-sm text-rose-800">
          {fulfillmentError}
        </p>
      {/if}

      <div class="grid gap-3 md:grid-cols-3">
        <article class="rounded-lg border border-slate-200 bg-slate-50 p-3">
          <p class="text-xs text-slate-600">配送日</p>
          <p class="mt-1 text-lg font-semibold text-slate-900">{fulfillmentBoard?.deliveryDate ?? "-"}</p>
        </article>
        <article class="rounded-lg border border-slate-200 bg-slate-50 p-3">
          <p class="text-xs text-slate-600">履約訂單數</p>
          <p class="mt-1 text-lg font-semibold text-slate-900">{fulfillmentOrderCount}</p>
        </article>
        <article class="rounded-lg border border-slate-200 bg-slate-50 p-3">
          <p class="text-xs text-slate-600">份數總量</p>
          <p class="mt-1 text-lg font-semibold text-slate-900">{fulfillmentPortionCount}</p>
        </article>
      </div>

      <div class="overflow-x-auto rounded-lg border border-slate-200">
        <table class="min-w-full text-sm">
          <thead class="bg-slate-50 text-left text-xs font-semibold tracking-wide text-slate-600">
            <tr>
              <th class="px-3 py-2">Plant</th>
              <th class="px-3 py-2">訂單</th>
              <th class="px-3 py-2">份數</th>
              <th class="px-3 py-2">配送狀態分布</th>
              <th class="px-3 py-2">特殊需求</th>
            </tr>
          </thead>
          <tbody>
            {#if !fulfillmentBoard || fulfillmentBoard.plants.length === 0}
              <tr>
                <td class="px-3 py-4 text-slate-500" colspan="5">尚無履約匯總資料。</td>
              </tr>
            {:else}
              {#each fulfillmentBoard.plants as plant}
                <tr class="border-t border-slate-100">
                  <td class="px-3 py-2">{plant.plantId}</td>
                  <td class="px-3 py-2">{plant.orderCount}</td>
                  <td class="px-3 py-2">{plant.portionCount}</td>
                  <td class="px-3 py-2">
                    <div class="flex flex-wrap gap-1 text-xs">
                      {#each plant.deliveryStatusCounts as entry}
                        <span class="rounded-full border border-slate-300 bg-white px-2 py-0.5">
                          {deliveryStatusLabel(entry.status)} {entry.count}
                        </span>
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

      <div class="overflow-x-auto rounded-lg border border-slate-200">
        <table class="min-w-full text-sm">
          <thead class="bg-slate-50 text-left text-xs font-semibold tracking-wide text-slate-600">
            <tr>
              <th class="px-3 py-2">訂單</th>
              <th class="px-3 py-2">Plant</th>
              <th class="px-3 py-2">流程狀態</th>
              <th class="px-3 py-2">配送狀態</th>
              <th class="px-3 py-2">餐點項目</th>
              <th class="px-3 py-2">配送更新</th>
            </tr>
          </thead>
          <tbody>
            {#if !fulfillmentBoard || fulfillmentBoard.orders.length === 0}
              <tr>
                <td class="px-3 py-4 text-slate-500" colspan="6">目前沒有符合篩選的履約訂單。</td>
              </tr>
            {:else}
              {#each fulfillmentBoard.orders as order}
                <tr class="border-t border-slate-100">
                  <td class="px-3 py-2 align-top">
                    <p class="font-semibold text-slate-900">{order.orderId}</p>
                  </td>
                  <td class="px-3 py-2 align-top">{order.plantId}</td>
                  <td class="px-3 py-2 align-top">{orderStatusLabel(order.orderStatus)}</td>
                  <td class="px-3 py-2 align-top">{deliveryStatusLabel(order.deliveryStatus)}</td>
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
                    <div class="grid gap-1">
                      <select
                        class="rounded border border-slate-300 bg-white px-2 py-1 text-xs"
                        bind:value={fulfillmentStatusDraftByOrderId[order.orderId]}
                      >
                        {#each deliveryStatusOptions as status}
                          <option value={status}>{deliveryStatusLabel(status)}</option>
                        {/each}
                      </select>
                      <button
                        type="button"
                        class="rounded bg-emerald-700 px-2 py-1 text-xs font-semibold text-white hover:bg-emerald-600 disabled:opacity-60"
                        onclick={() => advanceDeliveryStatus(order)}
                        disabled={fulfillmentStatusSubmittingByOrderId[order.orderId] === true}
                      >
                        {fulfillmentStatusSubmittingByOrderId[order.orderId] === true
                          ? "更新中..."
                          : "送出狀態更新"}
                      </button>
                    </div>
                  </td>
                </tr>
              {/each}
            {/if}
          </tbody>
        </table>
      </div>

      {#if fulfillmentBoard && fulfillmentIncludeAuditTransitions}
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
              {#if fulfillmentBoard.statusTransitions.length === 0}
                <tr>
                  <td class="px-3 py-3 text-slate-500" colspan="5">目前沒有配送狀態稽核紀錄。</td>
                </tr>
              {:else}
                {#each fulfillmentBoard.statusTransitions as entry}
                  <tr class="border-t border-slate-100">
                    <td class="px-3 py-2">{formatDateTime(entry.occurredAt)}</td>
                    <td class="px-3 py-2">{entry.orderId}</td>
                    <td class="px-3 py-2">{entry.actorId} ({entry.actorRole})</td>
                    <td class="px-3 py-2">{entry.operationId}</td>
                    <td class="px-3 py-2">
                      {deliveryStatusLabel(entry.fromStatus)} → {deliveryStatusLabel(entry.toStatus)}
                    </td>
                  </tr>
                {/each}
              {/if}
            </tbody>
          </table>
        </div>
      {/if}

      <div class="grid gap-4 lg:grid-cols-2">
        <article class="grid gap-3 rounded-lg border border-slate-200 bg-slate-50 p-3">
          <h4 class="text-sm font-semibold text-slate-900">批次查詢與可列印輸出</h4>
          <div class="flex flex-wrap gap-2">
            <input
              class="min-w-[220px] flex-1 rounded border border-slate-300 bg-white px-2 py-1.5 text-xs"
              placeholder="fbatch-..."
              bind:value={batchLookupId}
            />
            <button
              type="button"
              class="rounded border border-slate-300 px-3 py-1.5 text-xs font-medium text-slate-700 hover:border-slate-500 disabled:opacity-60"
              onclick={loadBatchById}
              disabled={batchLookupLoading}
            >
              {batchLookupLoading ? "讀取中..." : "讀取批次"}
            </button>
            <button
              type="button"
              class="rounded border border-slate-300 px-3 py-1.5 text-xs font-medium text-slate-700 hover:border-slate-500 disabled:opacity-60"
              onclick={printBatch}
              disabled={!activeBatch}
            >
              列印批次
            </button>
          </div>
          {#if batchLookupError}
            <p class="rounded border border-rose-200 bg-rose-50 px-2 py-1 text-xs text-rose-800">
              {batchLookupError}
            </p>
          {/if}
          {#if recentBatchIds.length > 0}
            <div class="grid gap-1 text-xs text-slate-700">
              <p class="font-semibold text-slate-800">最近批次</p>
              <div class="flex flex-wrap gap-1">
                {#each recentBatchIds as batchId}
                  <button
                    type="button"
                    class="rounded-full border border-slate-300 bg-white px-2 py-0.5 hover:border-slate-500"
                    onclick={() => {
                      batchLookupId = batchId;
                      void loadBatchById();
                    }}
                  >
                    {batchId}
                  </button>
                {/each}
              </div>
            </div>
          {/if}
          {#if activeBatch}
            <div class="grid gap-2 rounded border border-slate-300 bg-white p-3 text-xs text-slate-700">
              <p class="font-semibold text-slate-900">批次編號：{activeBatch.batchId}</p>
              <p>Vendor：{activeBatch.vendorId}</p>
              <p>配送日：{activeBatch.deliveryDate}</p>
              <p>擷取時間：{formatDateTime(activeBatch.capturedAt)}</p>
              <p>建立者：{activeBatch.generatedByActorId}</p>
              <p>追蹤關聯：{activeBatch.batchId} / {activeBatch.deliveryDate}</p>
            </div>
          {/if}
        </article>

        <article class="grid gap-3 rounded-lg border border-slate-200 bg-slate-50 p-3">
          <h4 class="text-sm font-semibold text-slate-900">營運訂單篩選</h4>
          <div class="grid gap-2 md:grid-cols-2">
            <label class="grid gap-1 text-xs">
              <span>起始日</span>
              <input
                class="rounded border border-slate-300 bg-white px-2 py-1.5"
                type="date"
                bind:value={operationsFromDate}
              />
            </label>
            <label class="grid gap-1 text-xs">
              <span>結束日</span>
              <input
                class="rounded border border-slate-300 bg-white px-2 py-1.5"
                type="date"
                bind:value={operationsToDate}
              />
            </label>
            <label class="grid gap-1 text-xs">
              <span>訂單狀態</span>
              <select class="rounded border border-slate-300 bg-white px-2 py-1.5" bind:value={operationsStatusFilter}>
                <option value="ALL">全部</option>
                {#each orderStatusOptions as status}
                  <option value={status}>{orderStatusLabel(status)}</option>
                {/each}
              </select>
            </label>
            <label class="grid gap-1 text-xs">
              <span>排序</span>
              <select class="rounded border border-slate-300 bg-white px-2 py-1.5" bind:value={operationsSortOrder}>
                <option value="asc">由舊到新</option>
                <option value="desc">由新到舊</option>
              </select>
            </label>
          </div>
          <button
            type="button"
            class="justify-self-start rounded-md bg-slate-900 px-3 py-2 text-xs font-semibold text-white hover:bg-slate-800 disabled:opacity-60"
            onclick={() => refreshOperationsOrders(true)}
            disabled={operationsLoading}
          >
            {operationsLoading ? "套用篩選中..." : "套用營運篩選"}
          </button>
          {#if operationsError}
            <p class="rounded border border-rose-200 bg-rose-50 px-2 py-1 text-xs text-rose-800">
              {operationsError}
            </p>
          {/if}
          <p class="text-xs text-slate-600">共 {operationsPageMeta?.totalItems ?? 0} 筆</p>
          <div class="max-h-56 overflow-auto rounded border border-slate-200 bg-white">
            <table class="min-w-full text-xs">
              <thead class="bg-slate-50 text-left font-semibold text-slate-600">
                <tr>
                  <th class="px-2 py-1.5">訂單</th>
                  <th class="px-2 py-1.5">配送日</th>
                  <th class="px-2 py-1.5">狀態</th>
                </tr>
              </thead>
              <tbody>
                {#if operationsOrders.length === 0}
                  <tr>
                    <td class="px-2 py-2 text-slate-500" colspan="3">目前沒有符合篩選的訂單。</td>
                  </tr>
                {:else}
                  {#each operationsOrders as order}
                    <tr class="border-t border-slate-100">
                      <td class="px-2 py-1.5">{order.orderId}</td>
                      <td class="px-2 py-1.5">{order.deliveryDate}</td>
                      <td class="px-2 py-1.5">{orderStatusLabel(order.status)}</td>
                    </tr>
                  {/each}
                {/if}
              </tbody>
            </table>
          </div>
        </article>
      </div>
    </section>
  {/if}

  {#if showDocsSection}
    <section class="grid gap-4 rounded-xl border border-slate-200 bg-white p-4 shadow-sm">
      <h3 class="text-lg font-semibold text-slate-900">文件與物件儲存流程</h3>
      <p class="text-sm text-slate-600">
        此區使用 vendor 專屬 object-storage API，不含任何 admin-only 操作；所有輸出都保留 objectRef 供追蹤。
      </p>
      <div class="grid gap-4 xl:grid-cols-2">
        <article class="grid gap-3 rounded-lg border border-slate-200 bg-slate-50 p-3">
          <h4 class="text-sm font-semibold text-slate-900">建立上傳計畫</h4>
          <div class="grid gap-2 md:grid-cols-2">
            <label class="grid gap-1 text-xs">
              <span>Artifact Class</span>
              <select
                class="rounded border border-slate-300 bg-white px-2 py-1.5"
                bind:value={uploadPlanDraft.artifactClass}
              >
                {#each artifactClassOptions as artifactClass}
                  <option value={artifactClass}>{artifactClass}</option>
                {/each}
              </select>
            </label>
            <label class="grid gap-1 text-xs">
              <span>檔名</span>
              <input class="rounded border border-slate-300 bg-white px-2 py-1.5" bind:value={uploadPlanDraft.fileName} />
            </label>
            <label class="grid gap-1 text-xs">
              <span>MIME Type</span>
              <input class="rounded border border-slate-300 bg-white px-2 py-1.5" bind:value={uploadPlanDraft.mimeType} />
            </label>
            <label class="grid gap-1 text-xs">
              <span>大小（bytes）</span>
              <input
                class="rounded border border-slate-300 bg-white px-2 py-1.5"
                type="number"
                min="1"
                bind:value={uploadPlanDraft.sizeBytes}
              />
            </label>
            <label class="grid gap-1 text-xs">
              <span>縮圖大小（選填）</span>
              <input
                class="rounded border border-slate-300 bg-white px-2 py-1.5"
                type="number"
                min="1"
                bind:value={uploadPlanDraft.thumbnailSizeBytes}
              />
            </label>
            <label class="grid gap-1 text-xs">
              <span>Locale（選填）</span>
              <input class="rounded border border-slate-300 bg-white px-2 py-1.5" bind:value={uploadPlanDraft.locale} />
            </label>
          </div>
          <button
            type="button"
            class="justify-self-start rounded-md bg-slate-900 px-3 py-2 text-xs font-semibold text-white hover:bg-slate-800 disabled:opacity-60"
            onclick={createUploadPlan}
            disabled={uploadPlanLoading}
          >
            {uploadPlanLoading ? "建立中..." : "建立上傳計畫"}
          </button>
          {#if uploadPlanError}
            <p class="rounded border border-rose-200 bg-rose-50 px-2 py-1 text-xs text-rose-800">{uploadPlanError}</p>
          {/if}
          {#if uploadPlanResult}
            <div class="grid gap-1 rounded border border-slate-300 bg-white p-2 text-xs text-slate-700">
              <p class="font-semibold text-slate-900">primary objectRef: {uploadPlanResult.primary.objectRef}</p>
              <p>upload expires: {uploadPlanResult.primary.uploadExpiresAtEpochSeconds}</p>
              {#if uploadPlanResult.thumbnail}
                <p>thumbnail objectRef: {uploadPlanResult.thumbnail.objectRef}</p>
              {/if}
              <p class="break-all">upload url: {uploadPlanResult.primary.uploadUrl}</p>
            </div>
          {/if}
        </article>

        <article class="grid gap-3 rounded-lg border border-slate-200 bg-slate-50 p-3">
          <h4 class="text-sm font-semibold text-slate-900">建立存取連結</h4>
          <label class="grid gap-1 text-xs">
            <span>objectRef</span>
            <input
              class="rounded border border-slate-300 bg-white px-2 py-1.5"
              bind:value={accessLinkDraft.objectRef}
              placeholder="obj://..."
            />
          </label>
          <label class="grid gap-1 text-xs">
            <span>Locale（選填）</span>
            <input class="rounded border border-slate-300 bg-white px-2 py-1.5" bind:value={accessLinkDraft.locale} />
          </label>
          <button
            type="button"
            class="justify-self-start rounded-md bg-emerald-700 px-3 py-2 text-xs font-semibold text-white hover:bg-emerald-600 disabled:opacity-60"
            onclick={createAccessLink}
            disabled={accessLinkLoading}
          >
            {accessLinkLoading ? "產生中..." : "產生下載連結"}
          </button>
          {#if accessLinkError}
            <p class="rounded border border-rose-200 bg-rose-50 px-2 py-1 text-xs text-rose-800">{accessLinkError}</p>
          {/if}
          {#if accessLinkResult}
            <div class="grid gap-1 rounded border border-slate-300 bg-white p-2 text-xs text-slate-700">
              <p class="font-semibold text-slate-900">objectRef: {accessLinkResult.objectRef}</p>
              <p>download expires: {accessLinkResult.downloadExpiresAtEpochSeconds}</p>
              <p class="break-all">download url: {accessLinkResult.downloadUrl}</p>
            </div>
          {/if}
        </article>
      </div>
    </section>
  {/if}
</section>
