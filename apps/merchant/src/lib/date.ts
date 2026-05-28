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
