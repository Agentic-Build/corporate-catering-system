import { describe, it, expect, vi, beforeEach } from "vitest";

const { mockClient } = vi.hoisted(() => ({
  mockClient: { GET: vi.fn(), POST: vi.fn(), PUT: vi.fn() },
}));
vi.mock("$lib/server/api", () => ({ apiFor: () => mockClient }));

import { actions, load } from "./+page.server";

const VENDOR = { id: "u1", role: "vendor_operator" };

function loadEvent(user: unknown = VENDOR) {
  return { locals: { user, apiToken: "t" }, url: new URL("http://x/") } as never;
}
function actionEvent(fd: FormData, user: unknown = VENDOR) {
  return { request: { formData: async () => fd }, locals: { user, apiToken: "t" } } as never;
}
function form(entries: Record<string, string>): FormData {
  const fd = new FormData();
  for (const [k, v] of Object.entries(entries)) fd.append(k, v);
  return fd;
}

beforeEach(() => {
  mockClient.GET.mockReset();
  mockClient.POST.mockReset();
  mockClient.PUT.mockReset();
});

describe("home load", () => {
  it("redirects to /login with return_to when unauthenticated", async () => {
    await expect(
      load({ locals: {}, url: new URL("http://x/dashboard") } as never),
    ).rejects.toMatchObject({ status: 303, location: "/login?return_to=%2Fdashboard" });
  });

  it("redirects to /login for non-vendor users", async () => {
    await expect(load(loadEvent({ id: "u2", role: "employee" }))).rejects.toMatchObject({
      status: 303,
      location: "/login",
    });
  });

  it("aggregates stats and groups supply by date", async () => {
    const todayStr = "today";
    mockClient.GET.mockImplementation((path: string, opts?: { params?: { query?: { date?: string } } }) => {
      if (path === "/api/merchant/menu-items") {
        return Promise.resolve({ data: { items: [{ id: "i1", name: "A" }] } });
      }
      if (path === "/api/merchant/orders") {
        return Promise.resolve({
          data: {
            items: [
              { status: "picked_up", total_price_minor: 100 },
              { status: "placed", total_price_minor: 50 },
              { status: "cutoff", total_price_minor: 20 },
              { status: "cancelled", total_price_minor: 999 },
            ],
          },
        });
      }
      if (path === "/api/merchant/supply") {
        const date = opts?.params?.query?.date;
        // today's supply has capacity figures; others empty
        return Promise.resolve({
          data: { items: [{ capacity: 10, remain: 4 }] },
        });
      }
      return Promise.resolve({ data: { items: [] } });
    });
    void todayStr;

    const res = (await load(loadEvent())) as {
      stats: {
        totalCapacity: number;
        totalSold: number;
        todayOrderCount: number;
        pickedUp: number;
        pendingPrep: number;
        revenue: number;
      };
      items: unknown[];
      days: { id: string; head: string; offset: number }[];
    };

    expect(res.items).toHaveLength(1);
    expect(res.days).toHaveLength(7);
    expect(res.days[0]!.head).toBe("今天");
    expect(res.days[1]!.head).toBe("明天");
    expect(res.days[2]!.head).toContain("/");
    expect(res.stats.todayOrderCount).toBe(4);
    expect(res.stats.pickedUp).toBe(1);
    expect(res.stats.pendingPrep).toBe(2);
    expect(res.stats.revenue).toBe(170);
    expect(res.stats.totalCapacity).toBe(10);
    expect(res.stats.totalSold).toBe(6);
  });

  it("survives API failures (empty data) and missing data fields", async () => {
    mockClient.GET.mockImplementation((path: string) => {
      if (path === "/api/merchant/menu-items") return Promise.reject(new Error("boom"));
      if (path === "/api/merchant/orders") return Promise.reject(new Error("boom"));
      if (path === "/api/merchant/supply") return Promise.reject(new Error("boom"));
      return Promise.resolve({ data: null });
    });
    const res = (await load(loadEvent())) as {
      items: unknown[];
      stats: { revenue: number; todayOrderCount: number };
    };
    expect(res.items).toEqual([]);
    expect(res.stats.revenue).toBe(0);
    expect(res.stats.todayOrderCount).toBe(0);
  });

  it("handles GET returning no .data (items default to empty)", async () => {
    mockClient.GET.mockResolvedValue({});
    const res = (await load(loadEvent())) as { items: unknown[]; stats: { totalCapacity: number } };
    expect(res.items).toEqual([]);
    expect(res.stats.totalCapacity).toBe(0);
  });
});

describe("home actions.setSupply", () => {
  it("fails when item or date missing", async () => {
    const res = await actions.setSupply!(actionEvent(form({ item_id: "", date: "" })));
    expect(res).toMatchObject({ status: 400, data: { error: "缺少餐點或日期" } });
  });

  it("fails when capacity is negative", async () => {
    const res = await actions.setSupply!(
      actionEvent(form({ item_id: "i1", date: "2026-05-30", capacity: "-1" })),
    );
    expect(res).toMatchObject({ status: 400, data: { error: "上限數值無效" } });
  });

  it("saves supply using defaults and computed cutoff", async () => {
    mockClient.PUT.mockResolvedValue({ data: {} });
    const res = await actions.setSupply!(
      actionEvent(form({ item_id: "i1", date: "2026-05-30", capacity: "12" })),
    );
    expect(res).toEqual({ success: true });
    expect(mockClient.PUT).toHaveBeenCalledWith(
      "/api/merchant/supply/{itemID}/{date}",
      expect.objectContaining({
        params: { path: { itemID: "i1", date: "2026-05-30" } },
        body: {
          capacity: 12,
          pickup_window: "全天",
          eta_label: "全天",
          cutoff_at: "2026-05-29T17:00:00+08:00",
        },
      }),
    );
  });

  it("honors explicit pickup_window and cutoff_at", async () => {
    mockClient.PUT.mockResolvedValue({ data: {} });
    await actions.setSupply!(
      actionEvent(
        form({
          item_id: "i1",
          date: "2026-05-30",
          capacity: "5",
          pickup_window: "11:00-13:00",
          cutoff_at: "2026-05-29T10:00:00+08:00",
        }),
      ),
    );
    expect(mockClient.PUT).toHaveBeenCalledWith(
      "/api/merchant/supply/{itemID}/{date}",
      expect.objectContaining({
        body: expect.objectContaining({
          pickup_window: "11:00-13:00",
          cutoff_at: "2026-05-29T10:00:00+08:00",
        }),
      }),
    );
  });

  it("returns 500 on API error", async () => {
    mockClient.PUT.mockResolvedValue({ error: { detail: "nope" } });
    const res = await actions.setSupply!(
      actionEvent(form({ item_id: "i1", date: "2026-05-30", capacity: "1" })),
    );
    expect(res).toMatchObject({ status: 500 });
  });
});

describe("home actions.toggleSoldOut", () => {
  it("fails on missing item/date", async () => {
    const res = await actions.toggleSoldOut!(actionEvent(form({ item_id: "", date: "" })));
    expect(res).toMatchObject({ status: 400, data: { error: "缺少餐點或日期" } });
  });

  it("posts sold_out=true and succeeds", async () => {
    mockClient.POST.mockResolvedValue({ data: {} });
    const res = await actions.toggleSoldOut!(
      actionEvent(form({ item_id: "i1", date: "2026-05-30", sold_out: "true" })),
    );
    expect(res).toEqual({ success: true });
    expect(mockClient.POST).toHaveBeenCalledWith(
      "/api/merchant/supply/{itemID}/{date}/sold-out",
      expect.objectContaining({ body: { sold_out: true } }),
    );
  });

  it("posts sold_out=false when value is not 'true'", async () => {
    mockClient.POST.mockResolvedValue({ data: {} });
    await actions.toggleSoldOut!(
      actionEvent(form({ item_id: "i1", date: "2026-05-30", sold_out: "false" })),
    );
    expect(mockClient.POST).toHaveBeenCalledWith(
      "/api/merchant/supply/{itemID}/{date}/sold-out",
      expect.objectContaining({ body: { sold_out: false } }),
    );
  });

  it("returns 500 on API error", async () => {
    mockClient.POST.mockResolvedValue({ error: { detail: "x" } });
    const res = await actions.toggleSoldOut!(
      actionEvent(form({ item_id: "i1", date: "2026-05-30", sold_out: "true" })),
    );
    expect(res).toMatchObject({ status: 500 });
  });
});

describe("home actions.publishItem", () => {
  it("fails on missing item", async () => {
    const res = await actions.publishItem!(actionEvent(form({ item_id: "" })));
    expect(res).toMatchObject({ status: 400, data: { error: "缺少餐點" } });
  });

  it("publishes and succeeds", async () => {
    mockClient.POST.mockResolvedValue({ data: {} });
    const res = await actions.publishItem!(actionEvent(form({ item_id: "i1" })));
    expect(res).toEqual({ success: true });
    expect(mockClient.POST).toHaveBeenCalledWith(
      "/api/merchant/menu-items/{id}/publish",
      expect.objectContaining({ params: { path: { id: "i1" } } }),
    );
  });

  it("returns 500 on API error", async () => {
    mockClient.POST.mockResolvedValue({ error: { detail: "x" } });
    const res = await actions.publishItem!(actionEvent(form({ item_id: "i1" })));
    expect(res).toMatchObject({ status: 500 });
  });
});
