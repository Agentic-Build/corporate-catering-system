// Asia/Taipei date helpers. Fixed UTC+8 offset (no DST) avoids the off-by-one
// during 00:00–08:00 Taipei when running on a UTC server.

const TAIPEI_OFFSET_MS = 8 * 60 * 60 * 1000;
const DAY_MS = 24 * 60 * 60 * 1000;

/** Asia/Taipei calendar date (YYYY-MM-DD) for an instant (defaults to now). */
export function taipeiISO(instant: Date | number = Date.now()): string {
  const ms = typeof instant === "number" ? instant : instant.getTime();
  const t = new Date(ms + TAIPEI_OFFSET_MS);
  return `${t.getUTCFullYear()}-${String(t.getUTCMonth() + 1).padStart(2, "0")}-${String(t.getUTCDate()).padStart(2, "0")}`;
}

/** Taipei calendar date `addDays` away from today (YYYY-MM-DD). */
export function dayId(addDays = 0): string {
  return taipeiISO(Date.now() + addDays * DAY_MS);
}

const WEEKDAY = ["日", "一", "二", "三", "四", "五", "六"];

export interface DayOption {
  id: string;
  head: string;
  sub?: string;
}

/**
 * The next 7 Taipei days starting today. `from` is overridable for tests.
 * If `selectedISO` falls outside the window it is prepended so the caller's
 * current selection is always present.
 */
export function buildDays(from: Date = new Date(), selectedISO?: string): DayOption[] {
  const labels = ["今天", "明天"];
  const base = from.getTime();
  const out: DayOption[] = [];
  for (let i = 0; i < 7; i++) {
    const taipei = new Date(base + i * DAY_MS + TAIPEI_OFFSET_MS);
    const m = taipei.getUTCMonth() + 1;
    const day = taipei.getUTCDate();
    const w = WEEKDAY[taipei.getUTCDay()];
    const id = `${taipei.getUTCFullYear()}-${String(m).padStart(2, "0")}-${String(day).padStart(2, "0")}`;
    const head = labels[i] ?? `${m}/${day}(${w})`;
    out.push({ id, head, sub: i < 2 ? `${m}/${day}(${w})` : undefined });
  }
  if (selectedISO && !out.some((d) => d.id === selectedISO)) {
    out.unshift({ id: selectedISO, head: selectedISO });
  }
  return out;
}
