import { describe, it, expect, vi, beforeEach } from "vitest";

const { mockClient } = vi.hoisted(() => ({
  mockClient: { GET: vi.fn(), POST: vi.fn(), PUT: vi.fn(), DELETE: vi.fn() },
}));
vi.mock("$lib/server/env", () => ({ API_BASE_URL: "http://x" }));
vi.mock("@tbite/api-client", () => ({ createApiClient: () => mockClient }));

import { load, actions } from "./+page.server";

const USER = { id: "u1", role: "employee", plant: "tn-a" };

function loadEvent(
  opts: {
    user?: unknown;
    search?: string;
    plants?: Array<{ id: string }>;
  } = {},
) {
  const search = opts.search ?? "";
  return {
    locals: { user: "user" in opts ? opts.user : USER, apiToken: "t" },
    url: new URL("http://h/" + search),
    parent: async () => ({ plants: opts.plants ?? [{ id: "tn-a" }] }),
    depends: vi.fn(),
  } as never;
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

describe("home load", () => {
  it("redirects to login when unauthenticated, preserving path+query", async () => {
    await expect(load(loadEvent({ user: null, search: "?day=2026-01-01" }))).rejects.toMatchObject({
      status: 303,
      location: "/login?return_to=%2F%3Fday%3D2026-01-01",
    });
  });

  it("loads home and derives favorites with no filter", async () => {
    mockClient.GET.mockResolvedValue({
      data: {
        target_day: "2026-01-02",
        has_ordered: true,
        order_summary: { order_id: "o1" },
        reorder_chips: [{ tags: ["a"] }],
        favorite_chips: [{ menu_item_id: "m1", tags: ["b"] }],
        recommend_chips: null,
        day_menu: [{ tags: ["a", "c"] }],
      },
    });
    const res = (await load(loadEvent())) as Record<string, unknown>;
    expect(res.selectedDay).toBe("2026-01-02");
    expect(res.favoriteIds).toEqual(["m1"]);
    expect(res.filterActive).toBe(false);
    expect(res.filteredMenu).toBeUndefined();
    expect(res.tagPool).toEqual(["a", "c"]);
    expect(res.selectedPlant).toBe("tn-a");
  });

  it("uses day override and fetches filtered menu when filter active", async () => {
    mockClient.GET.mockImplementation((path: string) =>
      Promise.resolve(
        path === "/api/employee/home"
          ? {
              data: {
                target_day: "2026-03-03",
                has_ordered: false,
                favorite_chips: [],
                day_menu: [{ tags: ["x"] }],
              },
            }
          : { data: { items: [{ tags: ["y"] }] } },
      ),
    );
    const res = (await load(
      loadEvent({
        search:
          "?day=2026-03-03&q=rice&tags=t1&price_min=10&price_max=50&in_stock=1&sort=price_asc",
      }),
    )) as Record<string, unknown>;
    expect(res.filterActive).toBe(true);
    expect(res.filteredMenu).toEqual([{ tags: ["y"] }]);
    expect(res.tagPool).toEqual(["x", "y"]);
    expect((res.menuFilter as { sort: string }).sort).toBe("price_asc");
  });

  it("falls back to plants[0] when user has no plant and no query plant", async () => {
    mockClient.GET.mockResolvedValue({
      data: { target_day: "d", has_ordered: false, favorite_chips: [], day_menu: [] },
    });
    const res = (await load(
      loadEvent({ user: { id: "u2", role: "employee" }, plants: [{ id: "first" }] }),
    )) as Record<string, unknown>;
    expect(res.selectedPlant).toBe("first");
  });

  it("falls back to empty plant id when nothing available", async () => {
    mockClient.GET.mockResolvedValue({
      data: { target_day: "d", has_ordered: false, favorite_chips: [], day_menu: [] },
    });
    const res = (await load(
      loadEvent({ user: { id: "u2", role: "employee" }, plants: [] }),
    )) as Record<string, unknown>;
    expect(res.selectedPlant).toBe("");
  });

  it("surfaces home error and skips filtered menu fetch when home errored but filter inactive", async () => {
    mockClient.GET.mockResolvedValue({ error: { detail: "home boom" } });
    const res = (await load(loadEvent())) as Record<string, unknown>;
    expect(res.error).toBe("home boom");
    expect((res.home as { target_day: string }).target_day).toBeDefined();
  });

  it("uses home error and does not overwrite with later menu error", async () => {
    mockClient.GET.mockImplementation((path: string) =>
      Promise.resolve(
        path === "/api/employee/home"
          ? { error: { detail: "home err" } }
          : { error: { detail: "menu err" } },
      ),
    );
    const res = (await load(loadEvent({ search: "?q=x" }))) as Record<string, unknown>;
    expect(res.error).toBe("home err");
  });

  it("surfaces filtered-menu error when home succeeded", async () => {
    mockClient.GET.mockImplementation((path: string) =>
      Promise.resolve(
        path === "/api/employee/home"
          ? { data: { target_day: "d", has_ordered: false, favorite_chips: [], day_menu: [] } }
          : { error: { detail: "menu only" } },
      ),
    );
    const res = (await load(loadEvent({ search: "?q=x" }))) as Record<string, unknown>;
    expect(res.error).toBe("menu only");
  });

  it("home empty-response branch (no data, no error) yields fallback home", async () => {
    mockClient.GET.mockResolvedValue({});
    const res = (await load(loadEvent())) as Record<string, unknown>;
    expect(res.error).toBeUndefined();
    expect((res.home as { has_ordered: boolean }).has_ordered).toBe(false);
  });

  it("home fetch throwing is caught and reported", async () => {
    mockClient.GET.mockRejectedValue(new Error("network down"));
    const res = (await load(loadEvent())) as Record<string, unknown>;
    expect(res.error).toBe("network down");
  });

  it("home fetch throwing a non-Error stringifies it", async () => {
    mockClient.GET.mockRejectedValue("plain string");
    const res = (await load(loadEvent())) as Record<string, unknown>;
    expect(res.error).toBe("plain string");
  });

  it("filtered-menu empty response and thrown non-Error are handled", async () => {
    mockClient.GET.mockImplementation((path: string) =>
      path === "/api/employee/home"
        ? Promise.resolve({
            data: { target_day: "d", has_ordered: false, favorite_chips: [], day_menu: [] },
          })
        : Promise.reject("menu str"),
    );
    const res = (await load(loadEvent({ search: "?q=x" }))) as Record<string, unknown>;
    expect(res.error).toBe("menu str");
    expect(res.filteredMenu).toBeUndefined();
  });

  it("filtered-menu empty (no data/no error) leaves items undefined", async () => {
    mockClient.GET.mockImplementation((path: string) =>
      path === "/api/employee/home"
        ? Promise.resolve({
            data: { target_day: "d", has_ordered: false, favorite_chips: [], day_menu: [] },
          })
        : Promise.resolve({}),
    );
    const res = (await load(loadEvent({ search: "?q=x" }))) as Record<string, unknown>;
    expect(res.filteredMenu).toBeUndefined();
    expect(res.error).toBeUndefined();
  });

  it("menu with data uses items default and collectTags ignores null tags", async () => {
    mockClient.GET.mockImplementation((path: string) =>
      path === "/api/employee/home"
        ? Promise.resolve({
            data: {
              target_day: "d",
              has_ordered: false,
              favorite_chips: [],
              day_menu: [{ tags: null }, {}],
            },
          })
        : Promise.resolve({ data: {} }),
    );
    const res = (await load(loadEvent({ search: "?q=x" }))) as Record<string, unknown>;
    expect(res.filteredMenu).toEqual([]);
    expect(res.tagPool).toEqual([]);
  });
});

describe("placeOrder action", () => {
  it("redirects to login when unauthenticated", async () => {
    await expect(actions.placeOrder!(actionEvent(form([]), null))).rejects.toMatchObject({
      status: 303,
      location: "/login",
    });
  });

  it("fails on empty cart", async () => {
    const res = await actions.placeOrder!(actionEvent(form([["plant", "p"]])));
    expect(res).toMatchObject({ status: 400, data: { error: "cart is empty" } });
  });

  it("fails when notes exceed 500 chars", async () => {
    const res = await actions.placeOrder!(
      actionEvent(
        form([
          ["item_id", "i1"],
          ["qty", "1"],
          ["notes", "a".repeat(501)],
        ]),
      ),
    );
    expect(res).toMatchObject({ status: 400, data: { error: "備註不可超過 500 字" } });
  });

  it("fails when all qty resolve to zero", async () => {
    const res = await actions.placeOrder!(
      actionEvent(
        form([
          ["item_id", "i1"],
          ["qty", "0"],
        ]),
      ),
    );
    expect(res).toMatchObject({ status: 400, data: { error: "no items selected" } });
  });

  it("maps cutoff error to friendly message", async () => {
    mockClient.POST.mockResolvedValue({
      error: { status: 409, detail: "order: cutoff time has passed" },
    });
    const res = await actions.placeOrder!(
      actionEvent(
        form([
          ["item_id", "i1"],
          ["qty", "2"],
          ["plant", "p"],
          ["supply_date", "d"],
        ]),
      ),
    );
    expect(res).toMatchObject({ status: 409, data: { error: "已超過截單時間，此日已無法預訂。" } });
  });

  it("uses backend detail then default for other errors", async () => {
    mockClient.POST.mockResolvedValue({ error: { detail: "custom" } });
    const res = await actions.placeOrder!(
      actionEvent(
        form([
          ["item_id", "i1"],
          ["qty", "2"],
        ]),
      ),
    );
    expect(res).toMatchObject({ status: 409, data: { error: "custom" } });

    mockClient.POST.mockResolvedValue({ error: {} });
    const res2 = await actions.placeOrder!(
      actionEvent(
        form([
          ["item_id", "i1"],
          ["qty", "2"],
        ]),
      ),
    );
    expect(res2).toMatchObject({ status: 409, data: { error: "送出預訂失敗，請稍後再試。" } });
  });

  it("fails when response lacks order id", async () => {
    mockClient.POST.mockResolvedValue({ data: { order: {} } });
    const res = await actions.placeOrder!(
      actionEvent(
        form([
          ["item_id", "i1"],
          ["qty", "2"],
        ]),
      ),
    );
    expect(res).toMatchObject({ status: 500, data: { error: "no order id in response" } });
  });

  it("redirects to the created order, filtering qty<=0 lines", async () => {
    mockClient.POST.mockResolvedValue({ data: { order: { id: "o9" } } });
    await expect(
      actions.placeOrder!(
        actionEvent(
          form([
            ["item_id", "i1"],
            ["qty", "0"],
            ["item_id", "i2"],
            ["qty", "3"],
          ]),
        ),
      ),
    ).rejects.toMatchObject({ status: 303, location: "/orders/o9" });
    expect(mockClient.POST).toHaveBeenCalledWith(
      "/api/employee/orders",
      expect.objectContaining({
        body: expect.objectContaining({ items: [{ menu_item_id: "i2", qty: 3 }] }),
      }),
    );
  });
});

describe("reorderPast action", () => {
  it("redirects to login when unauthenticated", async () => {
    await expect(actions.reorderPast!(actionEvent(form([]), null))).rejects.toMatchObject({
      status: 303,
      location: "/login",
    });
  });

  it("fails when source or date missing", async () => {
    const res = await actions.reorderPast!(actionEvent(form([["source_order_id", "s1"]])));
    expect(res).toMatchObject({ status: 400 });
  });

  it("on error builds toast from unavailable names", async () => {
    mockClient.POST.mockResolvedValue({
      error: { detail: "nope", unavailable_items: [{ name: "A" }, { name: "B" }] },
    });
    const res = await actions.reorderPast!(
      actionEvent(
        form([
          ["source_order_id", "s1"],
          ["supply_date", "d"],
        ]),
      ),
    );
    expect(res).toMatchObject({
      status: 409,
      data: { reorderToast: "今日皆無供應：A、B", error: "nope" },
    });
  });

  it("on error with no items uses detail fallback for toast", async () => {
    mockClient.POST.mockResolvedValue({ error: { detail: "fail detail" } });
    const res = await actions.reorderPast!(
      actionEvent(
        form([
          ["source_order_id", "s1"],
          ["supply_date", "d"],
        ]),
      ),
    );
    expect(res).toMatchObject({
      status: 409,
      data: { reorderToast: "fail detail", error: "fail detail" },
    });
  });

  it("on error with neither items nor detail uses generic strings", async () => {
    mockClient.POST.mockResolvedValue({ error: {} });
    const res = await actions.reorderPast!(
      actionEvent(
        form([
          ["source_order_id", "s1"],
          ["supply_date", "d"],
        ]),
      ),
    );
    expect(res).toMatchObject({
      status: 409,
      data: { reorderToast: "今日皆無供應", error: "reorder failed" },
    });
  });

  it("fails when no new_order_id", async () => {
    mockClient.POST.mockResolvedValue({ data: {} });
    const res = await actions.reorderPast!(
      actionEvent(
        form([
          ["source_order_id", "s1"],
          ["supply_date", "d"],
        ]),
      ),
    );
    expect(res).toMatchObject({ status: 500 });
  });

  it("redirects with partial query when some items unavailable", async () => {
    mockClient.POST.mockResolvedValue({
      data: { new_order_id: "n1", unavailable_items: [{ name: "X" }] },
    });
    await expect(
      actions.reorderPast!(
        actionEvent(
          form([
            ["source_order_id", "s1"],
            ["supply_date", "d"],
          ]),
        ),
      ),
    ).rejects.toMatchObject({
      status: 303,
      location: expect.stringContaining("/orders/n1?reorder=partial"),
    });
  });

  it("redirects plainly when all items available", async () => {
    mockClient.POST.mockResolvedValue({ data: { new_order_id: "n2" } });
    await expect(
      actions.reorderPast!(
        actionEvent(
          form([
            ["source_order_id", "s1"],
            ["supply_date", "d"],
          ]),
        ),
      ),
    ).rejects.toMatchObject({ status: 303, location: "/orders/n2" });
  });
});

describe("addFavorite / removeFavorite actions", () => {
  it("addFavorite rejects when unauthenticated", async () => {
    const res = await actions.addFavorite!(actionEvent(form([]), null));
    expect(res).toMatchObject({ status: 401 });
  });
  it("addFavorite requires menu_item_id", async () => {
    const res = await actions.addFavorite!(actionEvent(form([])));
    expect(res).toMatchObject({ status: 400 });
  });
  it("addFavorite surfaces error", async () => {
    mockClient.POST.mockResolvedValue({ error: { detail: "bad" } });
    const res = await actions.addFavorite!(actionEvent(form([["menu_item_id", "m1"]])));
    expect(res).toMatchObject({ status: 400, data: { error: "bad" } });
  });
  it("addFavorite succeeds", async () => {
    mockClient.POST.mockResolvedValue({ data: {} });
    const res = await actions.addFavorite!(actionEvent(form([["menu_item_id", "m1"]])));
    expect(res).toEqual({ ok: true });
  });

  it("removeFavorite rejects when unauthenticated", async () => {
    const res = await actions.removeFavorite!(actionEvent(form([]), null));
    expect(res).toMatchObject({ status: 401 });
  });
  it("removeFavorite requires menu_item_id", async () => {
    const res = await actions.removeFavorite!(actionEvent(form([])));
    expect(res).toMatchObject({ status: 400 });
  });
  it("removeFavorite surfaces error", async () => {
    mockClient.DELETE.mockResolvedValue({ error: { detail: "bad" } });
    const res = await actions.removeFavorite!(actionEvent(form([["menu_item_id", "m1"]])));
    expect(res).toMatchObject({ status: 400, data: { error: "bad" } });
  });
  it("removeFavorite succeeds", async () => {
    mockClient.DELETE.mockResolvedValue({ data: {} });
    const res = await actions.removeFavorite!(actionEvent(form([["menu_item_id", "m1"]])));
    expect(res).toEqual({ ok: true });
  });
});
