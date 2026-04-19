/**
 * Conversions between UI-friendly values (ISO date, HH:mm) and
 * backend internal representations (epochDay, minuteOfDay, minor).
 *
 * Keep all epoch / minor / minute-of-day math here. UI code should never
 * touch these units directly.
 */

const MS_PER_DAY = 24 * 60 * 60 * 1000;

/** Convert ISO date "YYYY-MM-DD" → epoch day (days since 1970-01-01 UTC). */
export function isoDateToEpochDay(iso: string): number {
  if (!iso) return 0;
  const [y, m, d] = iso.split("-").map((p) => Number.parseInt(p, 10));
  const utcMs = Date.UTC(y, m - 1, d);
  return Math.floor(utcMs / MS_PER_DAY);
}

/** Convert epoch day → ISO date "YYYY-MM-DD". */
export function epochDayToIsoDate(epochDay: number): string {
  const date = new Date(epochDay * MS_PER_DAY);
  const y = date.getUTCFullYear();
  const m = `${date.getUTCMonth() + 1}`.padStart(2, "0");
  const d = `${date.getUTCDate()}`.padStart(2, "0");
  return `${y}-${m}-${d}`;
}

/** Convert "HH:mm" → minute-of-day (0..1439). */
export function timeToMinuteOfDay(time: string): number {
  if (!time) return 0;
  const [h, m] = time.split(":").map((p) => Number.parseInt(p, 10));
  return h * 60 + m;
}

/** Convert minute-of-day → "HH:mm". */
export function minuteOfDayToTime(minute: number): string {
  const h = Math.floor(minute / 60);
  const m = minute % 60;
  return `${h.toString().padStart(2, "0")}:${m.toString().padStart(2, "0")}`;
}

/** Convert TWD major units (number, e.g. 120) → minor (12000). */
export function majorToMinor(major: number): number {
  return Math.round(major * 100);
}

/** Convert minor (12000) → major number (120). */
export function minorToMajor(minor: number): number {
  return minor / 100;
}

export function todayIsoDate(timezone = "Asia/Taipei"): string {
  return new Date().toLocaleDateString("en-CA", { timeZone: timezone });
}
