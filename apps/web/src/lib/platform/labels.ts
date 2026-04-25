/**
 * Central friendly-label mapping.
 *
 * Rule: any enum, event name, or technical key that appears in the UI
 * must be funneled through this module so that:
 * - Backend can rename enums without breaking UI text
 * - Localization is consistent
 * - New enum values fall back to human-readable defaults
 */

import type { StateTag as _StateTag } from "$lib/components/ui";

type Tone = "neutral" | "info" | "success" | "warning" | "danger" | "pending";

// ---------------------------------------------------------------------------
// Employee order lifecycle
// ---------------------------------------------------------------------------

const ORDER_STATUS_LABEL: Record<string, string> = {
  PENDING: "待處理",
  MODIFIED: "已修改",
  CANCELLED: "已取消",
  SOLD_OUT: "售罄",
  REFUND_PENDING: "退款中",
  REFUNDED: "已退款",
  FULFILLED: "已領餐"
};

const ORDER_STATUS_TONE: Record<string, Tone> = {
  PENDING: "info",
  MODIFIED: "info",
  CANCELLED: "danger",
  SOLD_OUT: "danger",
  REFUND_PENDING: "warning",
  REFUNDED: "neutral",
  FULFILLED: "success"
};

export function friendlyOrderStatus(status: string): string {
  return ORDER_STATUS_LABEL[status] ?? status;
}

export function orderStatusTone(status: string): Tone {
  return ORDER_STATUS_TONE[status] ?? "pending";
}

// ---------------------------------------------------------------------------
// Order timeline events
// ---------------------------------------------------------------------------

const ORDER_EVENT_LABEL: Record<string, string> = {
  ORDER_CREATED: "訂單建立",
  ORDER_MODIFIED: "訂單修改",
  ORDER_CANCELLED: "訂單取消",
  PICKUP_VERIFIED: "領餐核銷",
  PICKUP_REQUESTED: "請求領餐",
  REFUND_INITIATED: "退款發起",
  REFUND_COMPLETED: "退款完成",
  DISPUTE_OPENED: "申訴提交",
  DISPUTE_OWNER_ASSIGNED: "申訴指派",
  DISPUTE_RESOLVED_REFUND: "申訴結案（退款）",
  DISPUTE_RESOLVED_REJECTED: "申訴結案（駁回）",
  SOLD_OUT: "售罄",
  PAYROLL_SETTLED: "月結完成"
};

export function friendlyOrderEvent(eventType: string): string {
  return ORDER_EVENT_LABEL[eventType] ?? humanizeEnum(eventType);
}

// ---------------------------------------------------------------------------
// Payroll ledger entry kinds
// ---------------------------------------------------------------------------

const LEDGER_KIND_LABEL: Record<string, string> = {
  DEDUCTION: "薪資扣款",
  ADJUSTMENT_DEBIT: "調整扣款",
  ADJUSTMENT_CREDIT: "調整退款",
  REFUND: "退款"
};

/** Whether a kind represents money flowing OUT of the employee (positive debit). */
const LEDGER_KIND_IS_DEBIT: Record<string, boolean> = {
  DEDUCTION: true,
  ADJUSTMENT_DEBIT: true,
  ADJUSTMENT_CREDIT: false,
  REFUND: false
};

export function friendlyLedgerKind(kind: string): string {
  return LEDGER_KIND_LABEL[kind] ?? humanizeEnum(kind);
}

export function ledgerKindIsDebit(kind: string): boolean {
  return LEDGER_KIND_IS_DEBIT[kind] ?? true;
}

// ---------------------------------------------------------------------------
// Payroll dispute status
// ---------------------------------------------------------------------------

const DISPUTE_STATUS_LABEL: Record<string, string> = {
  OPEN: "已提交",
  IN_REVIEW: "審查中",
  RESOLVED_REFUND_APPROVED: "已結案（退款）",
  RESOLVED_REJECTED: "已結案（駁回）"
};

const DISPUTE_STATUS_TONE: Record<string, Tone> = {
  OPEN: "info",
  IN_REVIEW: "warning",
  RESOLVED_REFUND_APPROVED: "success",
  RESOLVED_REJECTED: "neutral"
};

export function friendlyDisputeStatus(status: string): string {
  return DISPUTE_STATUS_LABEL[status] ?? status;
}

export function disputeStatusTone(status: string): Tone {
  return DISPUTE_STATUS_TONE[status] ?? "pending";
}

// ---------------------------------------------------------------------------
// Menu type (BENTO / BOWL / ...)
// ---------------------------------------------------------------------------

const MENU_TYPE_LABEL: Record<string, string> = {
  BENTO: "便當",
  BOWL: "丼飯",
  NOODLE: "麵食",
  SALAD: "沙拉",
  SNACK: "輕食",
  DRINK: "飲料"
};

export function friendlyMenuType(menuType: string): string {
  return MENU_TYPE_LABEL[menuType] ?? menuType;
}

// ---------------------------------------------------------------------------
// Health tags (VEGAN / LOW_CALORIE / ...)
// ---------------------------------------------------------------------------

const HEALTH_TAG_LABEL: Record<string, string> = {
  LOW_CALORIE: "低卡",
  HIGH_PROTEIN: "高蛋白",
  VEGETARIAN: "素食",
  VEGAN: "純素",
  GLUTEN_FREE: "無麩質"
};

export function friendlyHealthTag(tag: string): string {
  return HEALTH_TAG_LABEL[tag] ?? humanizeEnum(tag);
}

// ---------------------------------------------------------------------------
// Vendor fulfillment delivery status
// ---------------------------------------------------------------------------

const DELIVERY_STATUS_LABEL: Record<string, string> = {
  PENDING_PREP: "待備餐",
  PREPARING: "備餐中",
  PACKED: "已打包",
  OUT_FOR_DELIVERY: "配送中",
  DELIVERED: "已送達",
  CANCELLED: "已取消"
};

const DELIVERY_STATUS_TONE: Record<string, Tone> = {
  PENDING_PREP: "pending",
  PREPARING: "info",
  PACKED: "info",
  OUT_FOR_DELIVERY: "warning",
  DELIVERED: "success",
  CANCELLED: "danger"
};

export function friendlyDeliveryStatus(status: string): string {
  return DELIVERY_STATUS_LABEL[status] ?? status;
}

export function deliveryStatusTone(status: string): Tone {
  return DELIVERY_STATUS_TONE[status] ?? "pending";
}

// ---------------------------------------------------------------------------
// Vendor menu item status (LISTED / PAUSED / DELISTED)
// ---------------------------------------------------------------------------

const MENU_ITEM_STATUS_LABEL: Record<string, string> = {
  LISTED: "上架中",
  PAUSED: "暫停接單",
  DELISTED: "永久下架"
};

const MENU_ITEM_STATUS_TONE: Record<string, Tone> = {
  LISTED: "success",
  PAUSED: "warning",
  DELISTED: "neutral"
};

export function friendlyMenuItemStatus(status: string): string {
  return MENU_ITEM_STATUS_LABEL[status] ?? status;
}

export function menuItemStatusTone(status: string): Tone {
  return MENU_ITEM_STATUS_TONE[status] ?? "pending";
}

// ---------------------------------------------------------------------------
// Vendor compliance status (PendingReview / FixRequested / Active / ...)
// ---------------------------------------------------------------------------

const COMPLIANCE_STATUS_LABEL: Record<string, string> = {
  PendingReview: "審核中",
  PENDING_REVIEW: "審核中",
  FixRequested: "要求補件",
  FIX_REQUESTED: "要求補件",
  Active: "通過",
  APPROVED: "通過",
  Rejected: "拒絕",
  REJECTED: "拒絕",
  Suspended: "停權",
  SUSPENDED: "停權"
};

const COMPLIANCE_STATUS_TONE: Record<string, Tone> = {
  PendingReview: "pending",
  PENDING_REVIEW: "pending",
  FixRequested: "warning",
  FIX_REQUESTED: "warning",
  Active: "success",
  APPROVED: "success",
  Rejected: "danger",
  REJECTED: "danger",
  Suspended: "danger",
  SUSPENDED: "danger"
};

export function friendlyComplianceStatus(status: string): string {
  return COMPLIANCE_STATUS_LABEL[status] ?? status;
}

export function complianceStatusTone(status: string): Tone {
  return COMPLIANCE_STATUS_TONE[status] ?? "pending";
}

// ---------------------------------------------------------------------------
// Anomaly alert
// ---------------------------------------------------------------------------

const ANOMALY_STATUS_LABEL: Record<string, string> = {
  OPEN: "開放中",
  ACKNOWLEDGED: "已確認",
  REMEDIATION_IN_PROGRESS: "處理中",
  ESCALATED: "已升級",
  CLOSED: "已結束"
};

const ANOMALY_STATUS_TONE: Record<string, Tone> = {
  OPEN: "danger",
  ACKNOWLEDGED: "warning",
  REMEDIATION_IN_PROGRESS: "info",
  ESCALATED: "danger",
  CLOSED: "success"
};

export function friendlyAnomalyStatus(status: string): string {
  return ANOMALY_STATUS_LABEL[status] ?? status;
}

export function anomalyStatusTone(status: string): Tone {
  return ANOMALY_STATUS_TONE[status] ?? "pending";
}

const ANOMALY_RULE_KIND_LABEL: Record<string, string> = {
  EXPIRY_RISK: "文件到期風險",
  ON_TIME_DEGRADATION: "準時率下降",
  SATISFACTION_DROP: "滿意度下滑",
  COMPLAINT_SPIKE: "客訴暴增"
};

export function friendlyAnomalyRuleKind(kind: string): string {
  return ANOMALY_RULE_KIND_LABEL[kind] ?? humanizeEnum(kind);
}

const ANOMALY_SEVERITY_LABEL: Record<string, string> = {
  WARNING: "注意",
  CRITICAL: "嚴重"
};

const ANOMALY_SEVERITY_TONE: Record<string, Tone> = {
  WARNING: "warning",
  CRITICAL: "danger"
};

export function friendlyAnomalySeverity(severity: string): string {
  return ANOMALY_SEVERITY_LABEL[severity] ?? severity;
}

export function anomalySeverityTone(severity: string): Tone {
  return ANOMALY_SEVERITY_TONE[severity] ?? "pending";
}

// ---------------------------------------------------------------------------
// Vendor category
// ---------------------------------------------------------------------------

const VENDOR_CATEGORY_LABEL: Record<string, string> = {
  RESTAURANT: "餐廳",
  BEVERAGE: "飲料",
  DESSERT: "甜點",
  HEALTHY_MEAL: "健康餐",
  SNACK: "輕食"
};

export function friendlyVendorCategory(category: string): string {
  return VENDOR_CATEGORY_LABEL[category] ?? humanizeEnum(category);
}

// ---------------------------------------------------------------------------
// Artifact class (object storage)
// ---------------------------------------------------------------------------

const ARTIFACT_CLASS_LABEL: Record<string, string> = {
  COMPLIANCE_DOCUMENT: "合規文件",
  MENU_IMAGE: "菜單圖片",
  MENU_IMAGE_THUMBNAIL: "菜單縮圖"
};

export function friendlyArtifactClass(cls: string): string {
  return ARTIFACT_CLASS_LABEL[cls] ?? humanizeEnum(cls);
}

// ---------------------------------------------------------------------------
// Generic enum humanizer fallback
// ---------------------------------------------------------------------------

/**
 * Fallback humanizer: turns `ORDER_MUTATION` → `Order Mutation`,
 * used when no explicit label exists. Callers can then wrap in parentheses
 * or append to signal "this is a raw backend value".
 */
export function humanizeEnum(value: string): string {
  if (!value) return "";
  return value
    .toLowerCase()
    .split(/[_\s]+/)
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(" ");
}

// ---------------------------------------------------------------------------
// Masked identifier for UI display (e.g. show last 4 chars of orderId)
// ---------------------------------------------------------------------------

export function maskIdentifier(id: string, visibleTail = 4): string {
  if (!id || id.length <= visibleTail) return id;
  return `…${id.slice(-visibleTail)}`;
}
