// Shared plant + day helpers. The home +page.server.ts has its own copy for
// server-side day computation; this module is the client-side equivalent the
// header LocationBar uses so plant/day selection works on every route.

export interface PlantOption {
  id: string;
  label: string;
}
export interface DayOption {
  id: string;
  head: string;
  sub?: string;
}

/** Next 7 days starting today, matching the home route's buildDays(). */
export function buildDays(today = new Date(), selectedISO?: string): DayOption[] {
  const wk = ["日", "一", "二", "三", "四", "五", "六"];
  const labels = ["今天", "明天"];
  const out: DayOption[] = [];
  for (let i = 0; i < 7; i++) {
    const d = new Date(today);
    d.setDate(today.getDate() + i);
    const m = d.getMonth() + 1;
    const day = d.getDate();
    const w = wk[d.getDay()];
    const head = labels[i] ?? `${m}/${day}(${w})`;
    const id = `${d.getFullYear()}-${String(m).padStart(2, "0")}-${String(day).padStart(2, "0")}`;
    out.push({ id, head, sub: i < 2 ? `${m}/${day}(${w})` : undefined });
  }
  if (selectedISO && !out.find((d) => d.id === selectedISO)) {
    out.unshift({ id: selectedISO, head: selectedISO });
  }
  return out;
}
