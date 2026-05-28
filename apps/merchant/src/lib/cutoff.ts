/**
 * Default order cutoff for a supply day: 17:00 Asia/Taipei (UTC+8, no DST) on
 * the day BEFORE the pickup date, as an RFC3339 timestamp.
 *
 * The backend stores this value verbatim (quota setCapacity → time.Parse), so
 * both the "day before" and the +08:00 offset must be explicit here. A bare
 * `${date}T17:00:00Z` is 01:00 the NEXT Taipei day — the opposite of the
 * "前一日 17:00" cutoff the UI promises.
 *
 * @param supplyDate pickup date as YYYY-MM-DD
 */
export function defaultCutoffAt(supplyDate: string): string {
  const [y = 0, m = 1, d = 1] = supplyDate.split("-").map(Number);
  // Date.UTC arithmetic subtracts one calendar day independent of host timezone
  // (handles month/year rollover, e.g. the 1st → last day of the previous month).
  const prev = new Date(Date.UTC(y, m - 1, d - 1));
  const yy = prev.getUTCFullYear();
  const mm = String(prev.getUTCMonth() + 1).padStart(2, "0");
  const dd = String(prev.getUTCDate()).padStart(2, "0");
  return `${yy}-${mm}-${dd}T17:00:00+08:00`;
}
