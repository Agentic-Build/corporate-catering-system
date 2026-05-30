import { describe, it, expect, vi, beforeEach } from "vitest";

const { mockClient } = vi.hoisted(() => ({ mockClient: { GET: vi.fn(), POST: vi.fn() } }));
vi.mock("$lib/server/env", () => ({ API_BASE_URL: "http://x" }));
vi.mock("@tbite/api-client", () => ({ createApiClient: () => mockClient }));

import { load, actions } from "./+page.server";

const USER = { id: "u1", plant: "tn-c" };
function loadEvent(user: unknown = USER, search = "") {
  return { locals: { user, apiToken: "t" }, url: new URL("http://h/menu/recommendations" + search) } as never;
}
function actionEvent(fd: FormData, user: unknown = USER, search = "") {
  return {
    request: { formData: async () => fd },
    locals: { user, apiToken: "t" },
    url: new URL("http://h/menu/recommendations" + search),
  } as never;
}
function form(entries: Array<[string, string]>): FormData {
  const fd = new FormData();
  for (const [k, v] of entries) fd.append(k, v);
  return fd;
}
beforeEach(() => {
  mockClient.GET.mockReset();
  mockClient.POST.mockReset();
});

describe("recommendations load", () => {
  it("redirects to login when unauthenticated", async () => {
    await expect(load(loadEvent(null))).rejects.toMatchObject({
      status: 303,
      location: "/login?return_to=%2Fmenu%2Frecommendations",
    });
  });
  it("returns chips and includes day query when present", async () => {
    mockClient.GET.mockResolvedValue({ data: { chips: [{ id: "c" }], next_cursor: 1 } });
    const res = await load(loadEvent(USER, "?day=2026-05-05"));
    expect(res).toMatchObject({ chips: [{ id: "c" }], nextCursor: 1, day: "2026-05-05", error: undefined });
    expect(mockClient.GET).toHaveBeenCalledWith(
      "/api/employee/recommendations",
      expect.objectContaining({ params: { query: { day: "2026-05-05", limit: 20 } } }),
    );
  });
  it("omits day query when absent and surfaces error", async () => {
    mockClient.GET.mockResolvedValue({ error: { detail: "boom" } });
    const res = await load(loadEvent());
    expect(res.error).toBe("boom");
    expect(res.day).toBeUndefined();
    expect(mockClient.GET).toHaveBeenCalledWith(
      "/api/employee/recommendations",
      expect.objectContaining({ params: { query: { limit: 20 } } }),
    );
  });
});

describe("loadMore action", () => {
  it("401 when unauthenticated", async () => {
    expect(await actions.loadMore!(actionEvent(form([]), null))).toMatchObject({ status: 401 });
  });
  it("error surfaces fail", async () => {
    mockClient.GET.mockResolvedValue({ error: { detail: "e" } });
    expect(await actions.loadMore!(actionEvent(form([["cursor", "2"]])))).toMatchObject({ status: 400 });
  });
  it("returns next page with day and defaults cursor", async () => {
    mockClient.GET.mockResolvedValue({ data: { chips: [], next_cursor: 7 } });
    const res = await actions.loadMore!(actionEvent(form([]), USER, "?day=2026-06-06"));
    expect(res).toEqual({ chips: [], nextCursor: 7 });
    expect(mockClient.GET).toHaveBeenCalledWith(
      "/api/employee/recommendations",
      expect.objectContaining({ params: { query: { day: "2026-06-06", cursor: 0, limit: 20 } } }),
    );
  });
});

describe("addToCart action", () => {
  it("redirects to login when unauthenticated", async () => {
    await expect(actions.addToCart!(actionEvent(form([]), null))).rejects.toMatchObject({
      status: 303,
      location: "/login",
    });
  });
  it("400 when menu_item_id missing", async () => {
    expect(await actions.addToCart!(actionEvent(form([])))).toMatchObject({ status: 400 });
  });
  it("creates order with home day and user plant", async () => {
    mockClient.GET.mockResolvedValue({ data: { target_day: "2026-07-07" } });
    mockClient.POST.mockResolvedValue({ data: { order: { id: "o1" } } });
    await expect(actions.addToCart!(actionEvent(form([["menu_item_id", "m1"]])))).rejects.toMatchObject({
      status: 303,
      location: "/orders/o1",
    });
  });
  it("falls back to taipei day and default plant", async () => {
    mockClient.GET.mockResolvedValue({ data: {} });
    mockClient.POST.mockResolvedValue({ data: { order: { id: "o2" } } });
    await expect(
      actions.addToCart!(actionEvent(form([["menu_item_id", "m1"]]), { id: "u2" })),
    ).rejects.toMatchObject({ status: 303, location: "/orders/o2" });
    expect(mockClient.POST).toHaveBeenCalledWith(
      "/api/employee/orders",
      expect.objectContaining({ body: expect.objectContaining({ plant: "tn-a" }) }),
    );
  });
  it("400 on order error", async () => {
    mockClient.GET.mockResolvedValue({ data: { target_day: "d" } });
    mockClient.POST.mockResolvedValue({ error: { detail: "bad" } });
    expect(await actions.addToCart!(actionEvent(form([["menu_item_id", "m1"]])))).toMatchObject({
      status: 400,
    });
  });
  it("500 when order id missing", async () => {
    mockClient.GET.mockResolvedValue({ data: { target_day: "d" } });
    mockClient.POST.mockResolvedValue({ data: { order: {} } });
    expect(await actions.addToCart!(actionEvent(form([["menu_item_id", "m1"]])))).toMatchObject({
      status: 500,
    });
  });
});
