import type {
  EmployeeOrderStatus,
  MenuHealthTag,
  MenuType,
  StorageArtifactClass,
  VendorFulfillmentDeliveryStatus,
  VendorMenuItemStatus
} from "../../../../../contract/generated/ts-client";

export const MENU_STATUS_OPTIONS: readonly VendorMenuItemStatus[] = [
  "LISTED",
  "PAUSED",
  "DELISTED"
];

export const DELIVERY_STATUS_OPTIONS: readonly VendorFulfillmentDeliveryStatus[] = [
  "PENDING_PREP",
  "PREPARING",
  "PACKED",
  "OUT_FOR_DELIVERY",
  "DELIVERED",
  "CANCELLED"
];

export const ORDER_STATUS_OPTIONS: readonly EmployeeOrderStatus[] = [
  "PENDING",
  "MODIFIED",
  "CANCELLED",
  "SOLD_OUT",
  "REFUND_PENDING",
  "REFUNDED",
  "FULFILLED"
];

export const MENU_TYPE_OPTIONS: readonly MenuType[] = [
  "BENTO",
  "BOWL",
  "NOODLE",
  "SALAD",
  "SNACK",
  "DRINK"
];

export const HEALTH_TAG_OPTIONS: readonly MenuHealthTag[] = [
  "LOW_CALORIE",
  "HIGH_PROTEIN",
  "VEGETARIAN",
  "VEGAN",
  "GLUTEN_FREE"
];

export const ARTIFACT_CLASS_OPTIONS: readonly StorageArtifactClass[] = [
  "COMPLIANCE_DOCUMENT",
  "MENU_IMAGE",
  "MENU_IMAGE_THUMBNAIL"
];

const MENU_STATUS_LABELS: Record<VendorMenuItemStatus, string> = {
  LISTED: "上架",
  PAUSED: "暫停",
  DELISTED: "下架"
};

export function menuStatusLabel(status: VendorMenuItemStatus): string {
  return MENU_STATUS_LABELS[status];
}

const MENU_STATUS_TONES: Record<VendorMenuItemStatus, "success" | "warning" | "neutral"> = {
  LISTED: "success",
  PAUSED: "warning",
  DELISTED: "neutral"
};

export function menuStatusTone(status: VendorMenuItemStatus): "success" | "warning" | "neutral" {
  return MENU_STATUS_TONES[status];
}

const DELIVERY_STATUS_LABELS: Record<VendorFulfillmentDeliveryStatus, string> = {
  PENDING_PREP: "待備餐",
  PREPARING: "備餐中",
  PACKED: "已打包",
  OUT_FOR_DELIVERY: "配送中",
  DELIVERED: "已送達",
  CANCELLED: "已取消"
};

export function deliveryStatusLabel(status: VendorFulfillmentDeliveryStatus): string {
  return DELIVERY_STATUS_LABELS[status];
}

type DeliveryTone = "info" | "warning" | "success" | "danger" | "pending";

const DELIVERY_STATUS_TONES: Record<VendorFulfillmentDeliveryStatus, DeliveryTone> = {
  PENDING_PREP: "pending",
  PREPARING: "info",
  PACKED: "info",
  OUT_FOR_DELIVERY: "warning",
  DELIVERED: "success",
  CANCELLED: "danger"
};

export function deliveryStatusTone(status: VendorFulfillmentDeliveryStatus): DeliveryTone {
  return DELIVERY_STATUS_TONES[status];
}

const ORDER_STATUS_LABELS: Record<EmployeeOrderStatus, string> = {
  PENDING: "待處理",
  MODIFIED: "已修改",
  CANCELLED: "已取消",
  SOLD_OUT: "售罄",
  REFUND_PENDING: "退款中",
  REFUNDED: "已退款",
  FULFILLED: "已履約"
};

export function orderStatusLabel(status: EmployeeOrderStatus): string {
  return ORDER_STATUS_LABELS[status];
}

const NEXT_DELIVERY_STATUS: Record<VendorFulfillmentDeliveryStatus, VendorFulfillmentDeliveryStatus> = {
  PENDING_PREP: "PREPARING",
  PREPARING: "PACKED",
  PACKED: "OUT_FOR_DELIVERY",
  OUT_FOR_DELIVERY: "DELIVERED",
  DELIVERED: "DELIVERED",
  CANCELLED: "CANCELLED"
};

export function nextDeliveryStatus(
  status: VendorFulfillmentDeliveryStatus
): VendorFulfillmentDeliveryStatus {
  return NEXT_DELIVERY_STATUS[status];
}

export function todayTaipeiIsoDate(): string {
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

export function addDaysIsoDate(dateIso: string, dayOffset: number): string {
  const base = new Date(`${dateIso}T00:00:00.000Z`);
  base.setUTCDate(base.getUTCDate() + dayOffset);
  return base.toISOString().slice(0, 10);
}

export function currentTaipeiContractDateTime(): string {
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

export function formatTaipeiDateTime(value: string | null | undefined): string {
  if (!value) {
    return "-";
  }
  const epochMs = Date.parse(value);
  if (Number.isNaN(epochMs)) {
    return value;
  }
  return new Date(epochMs).toLocaleString("zh-TW", {
    hour12: false,
    timeZone: "Asia/Taipei"
  });
}

export function generateMenuItemId(): string {
  const seed = `${Date.now().toString(36)}${Math.random().toString(36).slice(2, 10)}`;
  return `menu-${seed}`.slice(0, 32);
}

export function normalizeOptional(value: string): string | null {
  const trimmed = value.trim();
  return trimmed.length === 0 ? null : trimmed;
}

export function parseOptionalPositiveInt(
  value: string | number | null | undefined
): number | undefined {
  if (value === null || value === undefined) {
    return undefined;
  }
  if (typeof value === "number") {
    if (!Number.isFinite(value) || !Number.isInteger(value) || value <= 0) {
      throw new Error(`invalid positive integer: ${String(value)}`);
    }
    return value;
  }
  const trimmed = value.trim();
  if (!trimmed) {
    return undefined;
  }
  if (!/^[0-9]+$/.test(trimmed)) {
    throw new Error(`invalid positive integer: ${value}`);
  }
  const parsed = Number.parseInt(trimmed, 10);
  if (!Number.isSafeInteger(parsed) || parsed <= 0) {
    throw new Error(`invalid positive integer: ${value}`);
  }
  return parsed;
}

export function parseHealthTagsInput(input: string): {
  value: MenuHealthTag[];
  error?: string;
} {
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
    if (!HEALTH_TAG_OPTIONS.includes(value as MenuHealthTag)) {
      return {
        value: [],
        error: `健康標籤 ${value} 不支援，請使用 ${HEALTH_TAG_OPTIONS.join(", ")}。`
      };
    }
  }
  return { value: deduped as MenuHealthTag[] };
}

const RECENT_BATCH_STORAGE_KEY = "vendor.recentBatchIds";
const RECENT_BATCH_LIMIT = 12;

export function loadRecentBatchIds(): string[] {
  if (typeof window === "undefined") {
    return [];
  }
  try {
    const raw = window.localStorage.getItem(RECENT_BATCH_STORAGE_KEY);
    if (!raw) {
      return [];
    }
    const parsed: unknown = JSON.parse(raw);
    if (!Array.isArray(parsed)) {
      return [];
    }
    return parsed.filter((entry): entry is string => typeof entry === "string").slice(
      0,
      RECENT_BATCH_LIMIT
    );
  } catch {
    return [];
  }
}

export function pushRecentBatchId(batchId: string): string[] {
  if (typeof window === "undefined") {
    return [batchId];
  }
  const existing = loadRecentBatchIds();
  const next = [batchId, ...existing.filter((entry) => entry !== batchId)].slice(
    0,
    RECENT_BATCH_LIMIT
  );
  try {
    window.localStorage.setItem(RECENT_BATCH_STORAGE_KEY, JSON.stringify(next));
  } catch {
    // ignore storage failure
  }
  return next;
}

const ARTIFACT_TYPE_LABELS: Record<
  "DAILY_SUMMARY" | "PLANT_PARTITION_SHEET" | "LABELS" | "BASKET_LIST",
  string
> = {
  DAILY_SUMMARY: "每日彙總",
  PLANT_PARTITION_SHEET: "廠區分區表",
  LABELS: "餐盒標籤",
  BASKET_LIST: "配送籃清單"
};

export function artifactTypeLabel(
  type: "DAILY_SUMMARY" | "PLANT_PARTITION_SHEET" | "LABELS" | "BASKET_LIST"
): string {
  return ARTIFACT_TYPE_LABELS[type];
}
