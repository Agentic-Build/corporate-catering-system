import { apiClient, ensureApiClientConfigured, normalizeApiFailure } from "$lib/platform/api";

export type EmployeeOrderView = Awaited<
  ReturnType<typeof apiClient.employee.listEmployeeOrders>
>["items"][number];

export type EmployeeOrderLineItemView = EmployeeOrderView["lineItems"][number];

export type MenuDiscoveryItem = Awaited<
  ReturnType<typeof apiClient.employee.listEmployeeMenus>
>["items"][number];

export type MenuDiscoveryDay = Awaited<
  ReturnType<typeof apiClient.employee.listEmployeeMenus>
>["days"][number];

export type PayrollLedgerView = Awaited<
  ReturnType<typeof apiClient.employee.getEmployeeOrderPayrollLedger>
>;

export type PickupQrView = Awaited<
  ReturnType<typeof apiClient.employee.getEmployeePickupVerificationQr>
>;

export interface EmployeeGuardResult {
  plantId: string;
  apiBearerToken: string | null;
}

/**
 * Ensures the API client is configured and returns the active plant ID.
 * Throws if the actor is not an employee or has no plant scope.
 */
export function configureEmployeeApi(
  plantId: string | null,
  apiBearerToken: string | null
): string {
  if (!plantId) {
    throw new Error("目前登入帳號沒有可用的廠區範圍，無法載入員工訂餐資料。");
  }
  ensureApiClientConfigured(apiBearerToken);
  return plantId;
}

/**
 * Convenience: normalize caller errors into a localized string.
 */
export function describeApiError(error: unknown): string {
  return normalizeApiFailure(error).localizedMessage;
}

export function friendlyOrderStatus(status: string): string {
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

export function orderStatusTone(
  status: string
): "neutral" | "info" | "success" | "warning" | "danger" | "pending" {
  switch (status) {
    case "FULFILLED":
      return "success";
    case "PENDING":
    case "MODIFIED":
      return "info";
    case "SOLD_OUT":
    case "CANCELLED":
      return "danger";
    case "REFUND_PENDING":
      return "warning";
    case "REFUNDED":
      return "neutral";
    default:
      return "pending";
  }
}

export function todayTaipeiIsoDate(): string {
  return new Date().toLocaleDateString("en-CA", { timeZone: "Asia/Taipei" });
}

export function addDaysIsoDate(baseIsoDate: string, days: number): string {
  const [year, month, day] = baseIsoDate.split("-").map((value) => Number.parseInt(value, 10));
  const date = new Date(Date.UTC(year, month - 1, day + days));
  const nextYear = date.getUTCFullYear();
  const nextMonth = `${date.getUTCMonth() + 1}`.padStart(2, "0");
  const nextDay = `${date.getUTCDate()}`.padStart(2, "0");
  return `${nextYear}-${nextMonth}-${nextDay}`;
}

/**
 * Resolve menu item display names for a set of `menuItemId`s.
 *
 * OrderLineItem only carries `menuItemId` + price, not the menu name. To show
 * the user "排骨便當" rather than a raw ID, we join against `listEmployeeMenus`.
 * If an ID is not in the discovery window (e.g. item delisted, or historical
 * order), callers should fall back to `maskIdentifier(menuItemId)`.
 */
export async function loadMenuItemNameMap(
  plantId: string,
  menuItemIds: string[]
): Promise<Record<string, string>> {
  const unique = Array.from(new Set(menuItemIds.filter((id) => id.length > 0)));
  if (unique.length === 0) return {};

  // One broad call: include a wide window so most line items hit the cache.
  const today = todayTaipeiIsoDate();
  const fromDate = addDaysIsoDate(today, -60);
  const toDate = addDaysIsoDate(today, 30);

  try {
    const page = await apiClient.employee.listEmployeeMenus(
      plantId,
      "calendar",
      undefined,
      fromDate,
      toDate,
      1,
      500,
      "deliveryDate",
      "desc"
    );
    const map: Record<string, string> = {};
    for (const item of page.items) {
      map[item.menuItemId] = item.name;
    }
    return map;
  } catch {
    return {};
  }
}

/**
 * Compact text like "排骨便當 x2 · 雞腿便當 x1" — used as the human-facing
 * title for an order instead of the opaque `orderId`.
 */
export function summarizeOrderLineItems(
  lineItems: EmployeeOrderLineItemView[],
  nameMap: Record<string, string> = {}
): string {
  if (lineItems.length === 0) return "—";
  return lineItems
    .map((item) => {
      const name = nameMap[item.menuItemId] ?? `品項 ${item.menuItemId.slice(-4)}`;
      return `${name} x${item.quantity}`;
    })
    .join(" · ");
}

/**
 * Deep-link safe order lookup: walks pages of listEmployeeOrders until `orderId` is found
 * or the server reports no more pages. This avoids 404s when the target order is outside the
 * first page window. `maxPages` bounds total work so a malicious orderId cannot trigger infinite
 * pagination.
 */
export async function findEmployeeOrderById(
  orderId: string,
  options: { plantId: string; maxPages?: number; pageSize?: number } = {
    plantId: "",
    maxPages: 10,
    pageSize: 200
  }
): Promise<EmployeeOrderView | null> {
  const maxPages = options.maxPages ?? 10;
  const pageSize = options.pageSize ?? 200;

  for (let page = 1; page <= maxPages; page += 1) {
    const response = await apiClient.employee.listEmployeeOrders(
      options.plantId,
      undefined,
      undefined,
      page,
      pageSize,
      "deliveryDate",
      "desc"
    );

    const hit = response.items.find((order) => order.orderId === orderId);
    if (hit) return hit;

    if (response.items.length < pageSize) {
      return null;
    }
  }

  return null;
}
