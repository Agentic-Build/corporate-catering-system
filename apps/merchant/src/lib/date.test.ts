import { describe, it, expect } from "vitest";
import { taipeiISO, dayId } from "./date";

describe("taipeiISO", () => {
  it("returns the Asia/Taipei calendar date, not the UTC date", () => {
    // 17:00 UTC is already the next day (01:00) in Taipei (UTC+8).
    expect(taipeiISO(new Date("2026-05-29T17:00:00Z"))).toBe("2026-05-30");
    // 15:59 UTC is still 23:59 the same day in Taipei.
    expect(taipeiISO(new Date("2026-05-29T15:59:00Z"))).toBe("2026-05-29");
  });

  it("accepts an epoch-millis instant", () => {
    expect(taipeiISO(Date.UTC(2026, 0, 1, 0, 0, 0))).toBe("2026-01-01");
  });
});

describe("dayId", () => {
  it("offsets from today by whole Taipei days", () => {
    const today = taipeiISO();
    expect(dayId(0)).toBe(today);
    // dayId(1) is a valid YYYY-MM-DD strictly after today.
    expect(dayId(1)).toMatch(/^\d{4}-\d{2}-\d{2}$/);
    expect(dayId(1) > today).toBe(true);
  });
});
