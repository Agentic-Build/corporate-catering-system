import { describe, it, expect, vi, beforeEach } from "vitest";

const { mockClient } = vi.hoisted(() => ({ mockClient: { GET: vi.fn() } }));
vi.mock("$lib/server/api", () => ({ apiFor: () => mockClient }));

import { load } from "./+page.server";

const VENDOR = { id: "u1", role: "vendor_operator" };

function loadEvent(search = "", user: unknown = VENDOR) {
  return { locals: { user, apiToken: "t" }, url: new URL("http://x/prep-sheet" + search) } as never;
}

beforeEach(() => {
  mockClient.GET.mockReset();
});

describe("prep-sheet load", () => {
  it("redirects unauthenticated", async () => {
    await expect(load({ locals: {}, url: new URL("http://x/prep-sheet") } as never)).rejects.toMatchObject({
      status: 303,
    });
  });

  it("redirects non-vendor", async () => {
    await expect(load(loadEvent("", { role: "employee" }))).rejects.toMatchObject({
      status: 303,
      location: "/login",
    });
  });

  it("returns the sheet for the requested date", async () => {
    mockClient.GET.mockResolvedValue({
      data: { date: "2026-05-30", total_orders: 3, total_portions: 9, plants: [{ code: "A" }] },
    });
    const res = (await load(loadEvent("?date=2026-05-30"))) as {
      date: string;
      sheet: { total_orders: number; plants: unknown[] };
    };
    expect(res.date).toBe("2026-05-30");
    expect(res.sheet.total_orders).toBe(3);
    expect(res.sheet.plants).toHaveLength(1);
  });

  it("uses a default empty sheet on API throw and missing data", async () => {
    mockClient.GET.mockRejectedValueOnce(new Error("boom"));
    let res = (await load(loadEvent("?date=2026-05-30"))) as { sheet: { total_orders: number; plants: unknown[] } };
    expect(res.sheet.total_orders).toBe(0);
    expect(res.sheet.plants).toEqual([]);

    mockClient.GET.mockResolvedValueOnce({ data: null });
    res = (await load(loadEvent("?date=2026-05-30"))) as { sheet: { total_orders: number } };
    expect(res.sheet.total_orders).toBe(0);
  });
});
