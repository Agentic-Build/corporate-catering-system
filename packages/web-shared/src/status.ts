// Status label + tone catalogue. Audited from ~10 svelte files across the three
// apps. When two apps disagreed on a label, the admin (treasury-side) wording
// wins. The `<StatusTag>` component below renders these via `@tbite/ui`'s
// `StateTag`.

export type Tone = "success" | "warning" | "danger" | "info" | "pending" | "neutral";

interface Entry {
  label: string;
  tone: Tone;
}

type Table = Record<string, Entry>;

const order: Table = {
  draft: { label: "草稿", tone: "neutral" },
  placed: { label: "已預訂", tone: "info" },
  cutoff: { label: "已截單", tone: "warning" },
  cancelled: { label: "已取消", tone: "neutral" },
  ready: { label: "備餐完成", tone: "success" },
  picked_up: { label: "已領取", tone: "success" },
  no_show: { label: "未領取", tone: "danger" },
  refunded: { label: "已退款", tone: "warning" },
};

// Conflict note: employee/disputes labels "open" as 處理中; admin labels it 待處理.
// We pick admin wording — disputes flow from employee → admin.
const dispute: Table = {
  open: { label: "待處理", tone: "warning" },
  resolved_refund: { label: "已退款", tone: "success" },
  resolved_reject: { label: "已駁回", tone: "neutral" },
  cancelled: { label: "已取消", tone: "neutral" },
};

// Conflict note: employee/complaints labels "open" as 處理中; admin doesn't list it.
// We keep employee wording since admin treats only the "escalated" subset.
const complaint: Table = {
  open: { label: "處理中", tone: "warning" },
  vendor_responded: { label: "商家已回覆", tone: "info" },
  escalated: { label: "已升級福委會", tone: "danger" },
  resolved: { label: "已結案", tone: "success" },
};

const complaint_category: Table = {
  wrong_item: { label: "送錯餐點", tone: "neutral" },
  missing_item: { label: "餐點短缺", tone: "neutral" },
  quality: { label: "餐點品質", tone: "neutral" },
  portion: { label: "份量不足", tone: "neutral" },
  hygiene: { label: "衛生問題", tone: "neutral" },
  other: { label: "其他", tone: "neutral" },
};

const vendor: Table = {
  approved: { label: "已核准", tone: "success" },
  pending: { label: "待審", tone: "warning" },
  suspended: { label: "停權", tone: "danger" },
  terminated: { label: "已終止", tone: "neutral" },
};

const vendor_document: Table = {
  pending: { label: "待審", tone: "warning" },
  approved: { label: "已核准", tone: "success" },
  rejected: { label: "已駁回", tone: "danger" },
  expired: { label: "已過期", tone: "neutral" },
};

const anomaly_status: Table = {
  open: { label: "未處理", tone: "warning" },
  triaged: { label: "處理中", tone: "info" },
  closed: { label: "已關閉", tone: "neutral" },
};

const anomaly_severity: Table = {
  low: { label: "low", tone: "neutral" },
  medium: { label: "medium", tone: "info" },
  high: { label: "high", tone: "warning" },
  critical: { label: "critical", tone: "danger" },
};

const settlement: Table = {
  closed: { label: "已關帳", tone: "success" },
  void: { label: "已作廢", tone: "neutral" },
};

const payroll_batch: Table = {
  draft: { label: "草稿", tone: "neutral" },
  locked: { label: "已鎖定", tone: "warning" },
  exported: { label: "已匯出", tone: "success" },
  closed: { label: "已關閉", tone: "neutral" },
};

export const STATUS_TABLES = {
  order,
  dispute,
  complaint,
  complaint_category,
  vendor,
  vendor_document,
  anomaly_status,
  anomaly_severity,
  settlement,
  payroll_batch,
} as const;

export type StatusKind = keyof typeof STATUS_TABLES;

export function statusEntry(kind: StatusKind, value: string): Entry {
  return STATUS_TABLES[kind][value] ?? { label: value, tone: "neutral" };
}
