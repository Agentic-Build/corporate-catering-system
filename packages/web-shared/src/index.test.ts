import { describe, it, expect } from "vitest";
import { taipeiISO, dayId, taipeiDateTime, buildDays } from "./date";
import { formatMinor } from "./money";
import { problemMessage } from "./problem";
import { formStr } from "./form";
import { statusEntry } from "./status";

describe("taipeiISO", () => {
  it("returns Asia/Taipei date, not UTC", () => {
    expect(taipeiISO(new Date("2026-05-29T17:00:00Z"))).toBe("2026-05-30");
    expect(taipeiISO(new Date("2026-05-29T15:59:00Z"))).toBe("2026-05-29");
  });

  it("dayId(+1) is tomorrow", () => {
    const today = taipeiISO();
    expect(dayId(0)).toBe(today);
    expect(dayId(1)).not.toBe(today);
  });
});

describe("taipeiDateTime", () => {
  it("renders a UTC instant as Asia/Taipei wall-clock", () => {
    // 09:00Z == 17:00 Taipei — the cutoff bug: a 17:00 cutoff must not show 09:00.
    expect(taipeiDateTime("2026-05-29T09:00:00Z")).toBe("2026-05-29 17:00");
  });
  it("rolls to the next Taipei day past 16:00Z", () => {
    expect(taipeiDateTime("2026-05-29T17:00:00Z")).toBe("2026-05-30 01:00");
  });
  it("returns '-' for missing or unparseable input", () => {
    expect(taipeiDateTime(undefined)).toBe("-");
    expect(taipeiDateTime(null)).toBe("-");
    expect(taipeiDateTime("")).toBe("-");
    expect(taipeiDateTime("not-a-date")).toBe("-");
  });
});

describe("buildDays", () => {
  // Sunday 2026-05-31 09:00Z == 17:00 Taipei, still 5/31 (Sun) in Taipei.
  const from = new Date("2026-05-31T09:00:00Z");

  it("returns 7 consecutive Taipei days starting today", () => {
    const days = buildDays(from);
    expect(days).toHaveLength(7);
    expect(days.map((d) => d.id)).toEqual([
      "2026-05-31",
      "2026-06-01",
      "2026-06-02",
      "2026-06-03",
      "2026-06-04",
      "2026-06-05",
      "2026-06-06",
    ]);
  });

  it("labels the first two days 今天/明天 with a weekday sub, rest get m/d(weekday) head and no sub", () => {
    const days = buildDays(from);
    expect(days[0].head).toBe("今天");
    expect(days[0].sub).toBe("5/31(日)");
    expect(days[1].head).toBe("明天");
    expect(days[1].sub).toBe("6/1(一)");
    expect(days[2].head).toBe("6/2(二)");
    expect(days[2].sub).toBeUndefined();
    expect(days[6].head).toBe("6/6(六)");
  });

  it("prepends an out-of-window selection so it is always present", () => {
    const days = buildDays(from, "2026-01-01");
    expect(days).toHaveLength(8);
    expect(days[0]).toEqual({ id: "2026-01-01", head: "2026-01-01" });
  });

  it("does not prepend when the selection is already inside the window", () => {
    const days = buildDays(from, "2026-06-02");
    expect(days).toHaveLength(7);
    expect(days[0].id).toBe("2026-05-31");
  });

  it("defaults `from` to now and still returns 7 days", () => {
    expect(buildDays()).toHaveLength(7);
  });
});

describe("formatMinor", () => {
  it("formats integer NTD with separators (no /100)", () => {
    expect(formatMinor(120)).toBe("NT$120");
    expect(formatMinor(12000)).toBe("NT$12,000");
    expect(formatMinor(0)).toBe("NT$0");
    expect(formatMinor(null)).toBe("NT$0");
    expect(formatMinor(undefined)).toBe("NT$0");
  });
});

describe("problemMessage", () => {
  it("prefers detail > title > string fallback", () => {
    expect(problemMessage({ detail: "扣款失敗", title: "Bad Request" })).toBe("扣款失敗");
    expect(problemMessage({ title: "Bad Request" })).toBe("Bad Request");
    expect(problemMessage({ message: "network down" })).toBe("network down");
    expect(problemMessage(new Error("boom"))).toBe("boom");
    expect(problemMessage("plain string")).toBe("plain string");
    expect(problemMessage(null)).toBe("未知錯誤");
    expect(problemMessage({})).toBe("未知錯誤");
    expect(problemMessage(42)).toBe("42");
    expect(problemMessage(() => {})).toBe("未知錯誤");
  });
});

describe("formStr", () => {
  it("returns the string value, or empty for missing/non-string fields", () => {
    const fd = new FormData();
    fd.append("name", "  taco  ");
    fd.append("doc", new File(["x"], "x.txt"));
    expect(formStr(fd, "name")).toBe("  taco  ");
    expect(formStr(fd, "missing")).toBe("");
    expect(formStr(fd, "doc")).toBe("");
  });
});

describe("statusEntry", () => {
  it("returns known mapping", () => {
    expect(statusEntry("order", "picked_up")).toEqual({ label: "已領取", tone: "success" });
    expect(statusEntry("dispute", "resolved_refund").label).toBe("已退款");
  });
  it("falls back to value + neutral when unknown", () => {
    expect(statusEntry("order", "weird")).toEqual({ label: "weird", tone: "neutral" });
  });
});
