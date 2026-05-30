import { describe, it, expect, vi, beforeEach } from "vitest";

const { mockClient } = vi.hoisted(() => ({
  mockClient: { GET: vi.fn(), POST: vi.fn(), DELETE: vi.fn() },
}));
vi.mock("$lib/server/env", () => ({ API_BASE_URL: "http://x" }));
vi.mock("@tbite/api-client", () => ({ createApiClient: () => mockClient }));

import { load, actions } from "./+page.server";

const USER = { id: "u1", plant: "tn-b" };
function loadEvent(user: unknown = USER) {
  return { locals: { user, apiToken: "t" }, url: new URL("http://h/menu/favorites") } as never;
}
function actionEvent(fd: FormData, user: unknown = USER) {
  return { request: { formData: async () => fd }, locals: { user, apiToken: "t" } } as never;
}
function form(entries: Array<[string, string]>): FormData {
  const fd = new FormData();
  for (const [k, v] of entries) fd.append(k, v);
  return fd;
}
beforeEach(() => {
  mockClient.GET.mockReset();
  mockClient.POST.mockReset();
  mockClient.DELETE.mockReset();
});

describe("favorites load", () => {
  it("redirects to login when unauthenticated", async () => {
    await expect(load(loadEvent(null))).rejects.toMatchObject({
      status: 303,
      location: "/login?return_to=%2Fmenu%2Ffavorites",
    });
  });
  it("returns chips and cursor", async () => {
    mockClient.GET.mockResolvedValue({ data: { chips: [{ id: "c" }], next_cursor: 2 } });
    expect(await load(loadEvent())).toMatchObject({
      chips: [{ id: "c" }],
      nextCursor: 2,
      error: undefined,
    });
  });
  it("defaults chips and surfaces error", async () => {
    mockClient.GET.mockResolvedValue({ error: { detail: "boom" } });
    const res = (await load(loadEvent())) as { chips: unknown[]; error?: string };
    expect(res.chips).toEqual([]);
    expect(res.error).toBe("boom");
  });
});

describe("loadMore action", () => {
  it("401 when unauthenticated", async () => {
    expect(await actions.loadMore!(actionEvent(form([]), null))).toMatchObject({ status: 401 });
  });
  it("error surfaces fail", async () => {
    mockClient.GET.mockResolvedValue({ error: { detail: "e" } });
    expect(await actions.loadMore!(actionEvent(form([["cursor", "c1"]])))).toMatchObject({
      status: 400,
    });
  });
  it("returns next page and defaults chips", async () => {
    mockClient.GET.mockResolvedValue({ data: {} });
    expect(await actions.loadMore!(actionEvent(form([["cursor", "c1"]])))).toEqual({
      chips: [],
      nextCursor: undefined,
    });
  });
});

describe("removeFavorite action", () => {
  it("401 when unauthenticated", async () => {
    expect(await actions.removeFavorite!(actionEvent(form([]), null))).toMatchObject({
      status: 401,
    });
  });
  it("400 when menu_item_id missing", async () => {
    expect(await actions.removeFavorite!(actionEvent(form([])))).toMatchObject({ status: 400 });
  });
  it("error surfaces fail", async () => {
    mockClient.DELETE.mockResolvedValue({ error: { detail: "e" } });
    expect(
      await actions.removeFavorite!(actionEvent(form([["menu_item_id", "m1"]]))),
    ).toMatchObject({
      status: 400,
    });
  });
  it("succeeds", async () => {
    mockClient.DELETE.mockResolvedValue({ data: {} });
    expect(await actions.removeFavorite!(actionEvent(form([["menu_item_id", "m1"]])))).toEqual({
      ok: true,
      removed: "m1",
    });
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
  it("creates an order using home day and user plant then redirects", async () => {
    mockClient.GET.mockResolvedValue({ data: { target_day: "2026-04-04" } });
    mockClient.POST.mockResolvedValue({ data: { order: { id: "o1" } } });
    await expect(
      actions.addToCart!(actionEvent(form([["menu_item_id", "m1"]]))),
    ).rejects.toMatchObject({
      status: 303,
      location: "/orders/o1",
    });
    expect(mockClient.POST).toHaveBeenCalledWith(
      "/api/employee/orders",
      expect.objectContaining({
        body: expect.objectContaining({ plant: "tn-b", supply_date: "2026-04-04" }),
      }),
    );
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
      data: { error: "bad" },
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
