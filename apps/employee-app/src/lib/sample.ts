// Static sample data for screens whose endpoints are not wired in this
// scaffold (notifications, the 7-day strip, category pills, profile rows).
// Real-data screens (home/orders/payroll/totp) fetch from `api.ts`.

export type PlateColor = "warm" | "green" | "cool" | "rose" | "stone";

/** Deterministic placeholder colour from a vendor id (until real images). */
export function plateColor(seed: string): PlateColor {
  const colors: PlateColor[] = ["warm", "green", "cool", "rose", "stone"];
  let h = 0;
  for (let i = 0; i < seed.length; i++) h = (h * 31 + seed.charCodeAt(i)) >>> 0;
  return colors[h % colors.length];
}

export interface DayChip {
  id: string;
  label: string;
  sub: string;
}

/** Next 7 days for the home date strip. */
export function buildDays(today = new Date()): DayChip[] {
  const wk = ["日", "一", "二", "三", "四", "五", "六"];
  const labels = ["今天", "明天"];
  const out: DayChip[] = [];
  for (let i = 0; i < 7; i++) {
    const d = new Date(today);
    d.setDate(today.getDate() + i);
    const m = d.getMonth() + 1;
    const day = d.getDate();
    const id = `${d.getFullYear()}-${String(m).padStart(2, "0")}-${String(day).padStart(2, "0")}`;
    out.push({
      id,
      label: labels[i] ?? `${m}/${day}`,
      sub: i < 2 ? `${m}/${day} ${wk[d.getDay()]}` : wk[d.getDay()],
    });
  }
  return out;
}

export const CATEGORIES = [
  { id: "all", glyph: "🍱", label: "全部" },
  { id: "hot", glyph: "🔥", label: "熱門" },
  { id: "healthy", glyph: "🥗", label: "健康" },
  { id: "veggie", glyph: "🌿", label: "素食" },
  { id: "noodle", glyph: "🍜", label: "麵食" },
  { id: "drink", glyph: "🧋", label: "飲品" },
];

export const QUICK_TAGS = ["健康標籤", "healthy", "hot", "veggie"];

export const PLANTS = [
  { id: "tn-a", label: "台南廠 A 區" },
  { id: "tn-b", label: "台南廠 B 區" },
  { id: "tn-c", label: "台南廠 C 區" },
  { id: "tn-d", label: "台南廠 D 區" },
];

export interface Notification {
  id: number;
  type: "ready" | "info" | "reminder";
  title: string;
  msg: string;
  time: string;
  unread: boolean;
}

// TODO: wire to API — no employee notifications endpoint exists yet; the
// NotifModal is an in-app list per the design doc's "範圍外" note.
export const NOTIFICATIONS: Notification[] = [
  {
    id: 1,
    type: "ready",
    title: "訂單可領取",
    msg: "稻禾家便當 · 椒麻雞腿便當已備妥",
    time: "5 分鐘前",
    unread: true,
  },
  {
    id: 2,
    type: "info",
    title: "今日菜單更新",
    msg: "綠源輕食新增 2 道菜品",
    time: "1 小時前",
    unread: true,
  },
  {
    id: 3,
    type: "reminder",
    title: "截單提醒",
    msg: "明日餐點將於今日 17:00 截單",
    time: "2 小時前",
    unread: false,
  },
];

// Rating / complaint tag vocabularies for the EntryDetailSheet.
export const GOOD_TAGS = ["份量足", "味道好", "新鮮", "包裝整齊", "準時"];
export const BAD_TAGS = ["不新鮮", "份量不足", "包裝破損", "送達延遲", "與訂單不符"];

/** Format minor-unit currency as `$NN`. */
export function money(minor: number): string {
  return `$${Math.round(minor)}`;
}
