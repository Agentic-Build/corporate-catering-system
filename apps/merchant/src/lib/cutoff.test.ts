import { describe, it, expect } from "vitest";
import { defaultCutoffAt } from "./cutoff";

describe("defaultCutoffAt", () => {
  it("is 17:00 +08:00 on the day before the pickup date", () => {
    expect(defaultCutoffAt("2026-05-30")).toBe("2026-05-29T17:00:00+08:00");
  });

  it("rolls back across month boundaries", () => {
    expect(defaultCutoffAt("2026-06-01")).toBe("2026-05-31T17:00:00+08:00");
  });

  it("rolls back across year boundaries", () => {
    expect(defaultCutoffAt("2026-01-01")).toBe("2025-12-31T17:00:00+08:00");
  });

  it("represents an instant before midnight Taipei on the pickup day, not after", () => {
    // Regression: a bare ...T17:00:00Z resolves to 01:00 the next Taipei day,
    // letting employees order past the intended cutoff.
    const cutoff = new Date(defaultCutoffAt("2026-05-30")).getTime();
    const pickupMidnightTaipei = new Date("2026-05-30T00:00:00+08:00").getTime();
    expect(cutoff).toBeLessThan(pickupMidnightTaipei);
  });
});
