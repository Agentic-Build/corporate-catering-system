/**
 * Helpers for generating monthly settlement cycle suggestions.
 *
 * Settlement cycles are named by `YYYY-MM` in Taipei time. The default close
 * target is the previous month relative to "now". We also surface the prior
 * few months in case the operator missed a close.
 */

function taipeiYearMonth(date: Date): { year: number; month: number } {
  // `en-CA` produces `YYYY-MM-DD` format; we slice the year/month to avoid
  // timezone DST offsets on the client.
  const iso = date.toLocaleDateString("en-CA", { timeZone: "Asia/Taipei" });
  const [y, m] = iso.split("-").map((p) => Number.parseInt(p, 10));
  return { year: y, month: m };
}

function cycleKey(year: number, month: number): string {
  return `${year}-${`${month}`.padStart(2, "0")}`;
}

function shiftMonth(year: number, month: number, delta: number): { year: number; month: number } {
  const base = year * 12 + (month - 1) + delta;
  return { year: Math.floor(base / 12), month: (base % 12) + 1 };
}

export interface CycleSuggestion {
  cycleKey: string;
  label: string;
  hint?: string;
}

/**
 * Return the current month's previous-month cycle as default plus 3 earlier
 * fallback cycles. First entry is the operator's recommended cycle to close.
 */
export function suggestSettlementCycleKeys(now = new Date()): CycleSuggestion[] {
  const { year, month } = taipeiYearMonth(now);
  const suggestions: CycleSuggestion[] = [];
  const primary = shiftMonth(year, month, -1);
  suggestions.push({
    cycleKey: cycleKey(primary.year, primary.month),
    label: `${cycleKey(primary.year, primary.month)}（上月，建議）`,
    hint: "本系統預設的下一個待關帳週期。"
  });
  for (let i = 2; i <= 4; i += 1) {
    const fallback = shiftMonth(year, month, -i);
    suggestions.push({
      cycleKey: cycleKey(fallback.year, fallback.month),
      label: `${cycleKey(fallback.year, fallback.month)}（前 ${i} 個月）`
    });
  }
  return suggestions;
}
