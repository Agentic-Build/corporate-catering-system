import { describe, it, expect, vi, beforeEach } from "vitest";

const { mockClient } = vi.hoisted(() => ({
  mockClient: { GET: vi.fn(), POST: vi.fn() },
}));
vi.mock("$lib/server/api", () => ({ apiFor: () => mockClient }));

import { actions, load } from "./+page.server";

const VENDOR = { id: "u1", role: "vendor_operator" };

function loadEvent(search = "", user: unknown = VENDOR) {
  return {
    locals: { user, apiToken: "t" },
    url: new URL("http://x/orders" + search),
    depends: vi.fn(),
  } as never;
}
function actionEvent(fd: FormData) {
  return { request: { formData: async () => fd }, locals: { user: VENDOR, apiToken: "t" } } as never;
}
function form(entries: Record<string, string | string[]>): FormData {
  const fd = new FormData();
  for (const [k, v] of Object.entries(entries)) {
    if (Array.isArray(v)) v.forEach((x) => fd.append(k, x));
    else fd.append(k, v);
  }
  return fd;
}

beforeEach(() => {
  mockClient.GET.mockReset();
  mockClient.POST.mockReset();
});

describe("orders load", () => {
  it("redirects unauthenticated", async () => {
    await expect(
      load({ locals: {}, url: new URL("http://x/orders"), depends: vi.fn() } as never),
    ).rejects.toMatchObject({ status: 303 });
  });

  it("redirects non-vendor", async () => {
    await expect(load(loadEvent("", { role: "employee" }))).rejects.toMatchObject({
      status: 303,
      location: "/login",
    });
  });

  it("registers app:orders dependency, groups by plant, builds itemsById and 7 days", async () => {
    const depends = vi.fn();
    mockClient.GET.mockImplementation((path: string) => {
      if (path === "/api/merchant/orders")
        return Promise.resolve({
          data: { items: [{ plant: "A", item_id: "i1" }, { plant: "A" }, { plant: "B" }] },
        });
      if (path === "/api/merchant/menu-items")
        return Promise.resolve({ data: { items: [{ id: "i1", name: "Rice" }] } });
      return Promise.resolve({ data: { items: [] } });
    });
    const res = (await load({
      locals: { user: VENDOR, apiToken: "t" },
      url: new URL("http://x/orders?date=2026-05-30"),
      depends,
    } as never)) as {
      byPlant: Record<string, unknown[]>;
      totalCount: number;
      itemsById: Record<string, { name: string }>;
      days: { id: string; label: string }[];
      date: string;
    };
    expect(depends).toHaveBeenCalledWith("app:orders");
    expect(res.date).toBe("2026-05-30");
    expect(res.totalCount).toBe(3);
    expect(res.byPlant.A).toHaveLength(2);
    expect(res.byPlant.B).toHaveLength(1);
    expect(res.itemsById.i1).toEqual({ name: "Rice" });
    expect(res.days).toHaveLength(7);
    expect(res.days[0]!.label).toBe("今天");
    expect(res.days[1]!.label).toBe("明天");
    expect(res.days[2]!.label).toMatch(/\d{2}-\d{2}/);
  });

  it("survives API failures (orders & menu-items) and missing data", async () => {
    mockClient.GET.mockImplementation((path: string) => {
      if (path === "/api/merchant/orders") return Promise.reject(new Error("boom"));
      return Promise.reject(new Error("boom"));
    });
    const res = (await load(loadEvent())) as { totalCount: number; itemsById: object };
    expect(res.totalCount).toBe(0);
    expect(res.itemsById).toEqual({});

    mockClient.GET.mockResolvedValue({ data: null });
    const res2 = (await load(loadEvent())) as { totalCount: number };
    expect(res2.totalCount).toBe(0);
  });
});

describe("orders.markReady", () => {
  it("fails when no orders selected", async () => {
    const res = await actions.markReady!(actionEvent(form({})));
    expect(res).toMatchObject({ status: 400, data: { error: "no orders selected" } });
  });

  it("marks selected orders ready", async () => {
    mockClient.POST.mockResolvedValue({ data: {} });
    const res = await actions.markReady!(actionEvent(form({ order_id: ["o1", "o2"] })));
    expect(res).toEqual({ success: true, count: 2 });
    expect(mockClient.POST).toHaveBeenCalledWith(
      "/api/merchant/orders/mark-ready",
      expect.objectContaining({ body: { order_ids: ["o1", "o2"] } }),
    );
  });

  it("returns 500 on API error", async () => {
    mockClient.POST.mockResolvedValue({ error: { detail: "x" } });
    const res = await actions.markReady!(actionEvent(form({ order_id: "o1" })));
    expect(res).toMatchObject({ status: 500 });
  });
});

describe("orders.markReadyManual", () => {
  it("fails when code empty", async () => {
    const res = await actions.markReadyManual!(actionEvent(form({ code: " ", date: "2026-05-30" })));
    expect(res).toMatchObject({ status: 400, data: { error: "請輸入訂單編號" } });
  });

  it("404s when no order matches the code", async () => {
    mockClient.GET.mockResolvedValue({ data: { items: [{ order_number: 5, id: "o5" }] } });
    const res = await actions.markReadyManual!(
      actionEvent(form({ code: "99", date: "2026-05-30" })),
    );
    expect(res).toMatchObject({ status: 404 });
  });

  it("404s when orders GET throws (empty list)", async () => {
    mockClient.GET.mockRejectedValue(new Error("boom"));
    const res = await actions.markReadyManual!(
      actionEvent(form({ code: "1", date: "2026-05-30" })),
    );
    expect(res).toMatchObject({ status: 404 });
  });

  it("rejects already-ready / picked_up / no_show orders", async () => {
    for (const status of ["ready", "picked_up", "no_show"]) {
      mockClient.GET.mockResolvedValue({ data: { items: [{ order_number: 7, id: "o7", status }] } });
      const res = await actions.markReadyManual!(
        actionEvent(form({ code: "7", date: "2026-05-30" })),
      );
      expect(res).toMatchObject({ status: 400, data: { error: expect.stringContaining("已出餐或已領取") } });
    }
  });

  it("rejects cancelled orders", async () => {
    mockClient.GET.mockResolvedValue({
      data: { items: [{ order_number: 8, id: "o8", status: "cancelled" }] },
    });
    const res = await actions.markReadyManual!(actionEvent(form({ code: "8", date: "2026-05-30" })));
    expect(res).toMatchObject({ status: 400, data: { error: expect.stringContaining("已取消") } });
  });

  it("marks a placed order ready", async () => {
    mockClient.GET.mockResolvedValue({
      data: { items: [{ order_number: 9, id: "o9", status: "placed" }] },
    });
    mockClient.POST.mockResolvedValue({ data: {} });
    const res = await actions.markReadyManual!(actionEvent(form({ code: "9", date: "2026-05-30" })));
    expect(res).toEqual({ success: true, count: 1 });
    expect(mockClient.POST).toHaveBeenCalledWith(
      "/api/merchant/orders/mark-ready",
      expect.objectContaining({ body: { order_ids: ["o9"] } }),
    );
  });

  it("returns 500 when mark-ready API errors", async () => {
    mockClient.GET.mockResolvedValue({
      data: { items: [{ order_number: 9, id: "o9", status: "cutoff" }] },
    });
    mockClient.POST.mockResolvedValue({ error: { detail: "x" } });
    const res = await actions.markReadyManual!(actionEvent(form({ code: "9", date: "2026-05-30" })));
    expect(res).toMatchObject({ status: 500 });
  });

  it("handles orders GET returning no data", async () => {
    mockClient.GET.mockResolvedValue({ data: null });
    const res = await actions.markReadyManual!(actionEvent(form({ code: "1", date: "2026-05-30" })));
    expect(res).toMatchObject({ status: 404 });
  });
});
