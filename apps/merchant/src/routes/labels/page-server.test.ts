import { describe, it, expect, vi, beforeEach } from "vitest";

const { mockClient } = vi.hoisted(() => ({ mockClient: { GET: vi.fn() } }));
vi.mock("$lib/server/api", () => ({ apiFor: () => mockClient }));

import { load } from "./+page.server";

const VENDOR = { id: "u1", role: "vendor_operator" };

function loadEvent(search = "", user: unknown = VENDOR) {
  return { locals: { user, apiToken: "t" }, url: new URL("http://x/labels" + search) } as never;
}

beforeEach(() => {
  mockClient.GET.mockReset();
  vi.useRealTimers();
});

describe("labels load", () => {
  it("redirects unauthenticated", async () => {
    await expect(
      load({ locals: {}, url: new URL("http://x/labels") } as never),
    ).rejects.toMatchObject({
      status: 303,
      location: "/login?return_to=%2Flabels",
    });
  });

  it("redirects non-vendor", async () => {
    await expect(load(loadEvent("", { role: "employee" }))).rejects.toMatchObject({
      status: 303,
      location: "/login",
    });
  });

  it("builds itemsById and 7-day picker, uses explicit date param", async () => {
    mockClient.GET.mockImplementation((path: string) => {
      if (path === "/api/merchant/orders")
        return Promise.resolve({ data: { items: [{ id: "o1" }, { id: "o2" }] } });
      if (path === "/api/merchant/menu-items")
        return Promise.resolve({ data: { items: [{ id: "i1", name: "Rice" }] } });
      return Promise.resolve({ data: { items: [] } });
    });
    const res = (await load(loadEvent("?date=2026-05-30"))) as {
      date: string;
      totalCount: number;
      itemsById: Record<string, { name: string }>;
      days: { id: string; label: string }[];
    };
    expect(res.date).toBe("2026-05-30");
    expect(res.totalCount).toBe(2);
    expect(res.itemsById.i1).toEqual({ name: "Rice" });
    expect(res.days).toHaveLength(7);
    expect(res.days[0]!.label).toBe("今天");
    expect(res.days[1]!.label).toBe("明天");
    expect(res.days[2]!.label).toMatch(/\d{2}-\d{2}/);
  });

  it("defaults date to Taipei today when no param", async () => {
    mockClient.GET.mockResolvedValue({ data: { items: [] } });
    const res = (await load(loadEvent())) as { date: string };
    expect(res.date).toMatch(/^\d{4}-\d{2}-\d{2}$/);
  });

  it("survives API failures and missing data on both calls", async () => {
    mockClient.GET.mockRejectedValue(new Error("boom"));
    let res = (await load(loadEvent())) as { totalCount: number; itemsById: object };
    expect(res.totalCount).toBe(0);
    expect(res.itemsById).toEqual({});

    mockClient.GET.mockResolvedValue({ data: null });
    res = (await load(loadEvent())) as { totalCount: number; itemsById: object };
    expect(res.totalCount).toBe(0);
    expect(res.itemsById).toEqual({});
  });
});
