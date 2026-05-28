import { describe, it, expect } from "vitest";
import { taipeiISO, dayId } from "./date";
import { formatMinor } from "./money";
import { problemMessage } from "./problem";
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
    expect(problemMessage("plain string")).toBe("plain string");
    expect(problemMessage(null)).toBe("未知錯誤");
    expect(problemMessage({})).toBe("[object Object]");
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
