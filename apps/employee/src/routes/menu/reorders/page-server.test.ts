import { describe, it, expect, vi, beforeEach } from "vitest";

const { mockClient } = vi.hoisted(() => ({ mockClient: { GET: vi.fn(), POST: vi.fn() } }));
vi.mock("$lib/server/env", () => ({ API_BASE_URL: "http://x" }));
vi.mock("@tbite/api-client", () => ({ createApiClient: () => mockClient }));

import { load, actions } from "./+page.server";

const USER = { id: "u1" };
function loadEvent(user: unknown = USER) {
  return { locals: { user, apiToken: "t" }, url: new URL("http://h/menu/reorders") } as never;
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
});

describe("reorders load", () => {
  it("redirects to login when unauthenticated", async () => {
    await expect(load(loadEvent(null))).rejects.toMatchObject({
      status: 303,
      location: "/login?return_to=%2Fmenu%2Freorders",
    });
  });
  it("returns chips, cursor and target day", async () => {
    mockClient.GET.mockImplementation((path: string) =>
      Promise.resolve(
        path === "/api/employee/reorders"
          ? { data: { chips: [{ id: "c" }], next_cursor: 5 } }
          : { data: { target_day: "2026-09-09" } },
      ),
    );
    const res = await load(loadEvent());
    expect(res).toMatchObject({
      chips: [{ id: "c" }],
      nextCursor: 5,
      targetDay: "2026-09-09",
      error: undefined,
    });
  });
  it("defaults chips and falls back to taipei day; surfaces error", async () => {
    mockClient.GET.mockImplementation((path: string) =>
      Promise.resolve(
        path === "/api/employee/reorders" ? { error: { detail: "boom" } } : { data: {} },
      ),
    );
    const res = (await load(loadEvent())) as {
      chips: unknown[];
      error?: string;
      targetDay: string;
    };
    expect(res.chips).toEqual([]);
    expect(res.error).toBe("boom");
    expect(typeof res.targetDay).toBe("string");
  });
});

describe("loadMore action", () => {
  it("401 when unauthenticated", async () => {
    expect(await actions.loadMore!(actionEvent(form([]), null))).toMatchObject({ status: 401 });
  });
  it("error surfaces fail", async () => {
    mockClient.GET.mockResolvedValue({ error: { detail: "e" } });
    expect(await actions.loadMore!(actionEvent(form([["cursor", "3"]])))).toMatchObject({
      status: 400,
      data: { error: "e" },
    });
  });
  it("returns next page; defaults cursor when absent", async () => {
    mockClient.GET.mockResolvedValue({ data: { chips: [{ id: "x" }], next_cursor: 9 } });
    const res = await actions.loadMore!(actionEvent(form([])));
    expect(res).toEqual({ chips: [{ id: "x" }], nextCursor: 9 });
    expect(mockClient.GET).toHaveBeenCalledWith(
      "/api/employee/reorders",
      expect.objectContaining({ params: { query: { cursor: 0, limit: 20 } } }),
    );
  });
  it("defaults chips to empty when data lacks them", async () => {
    mockClient.GET.mockResolvedValue({ data: {} });
    expect(
      ((await actions.loadMore!(actionEvent(form([["cursor", "1"]])))) as { chips: unknown[] })
        .chips,
    ).toEqual([]);
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
    expect(await actions.reorderPast!(actionEvent(form([["source_order_id", "s"]])))).toMatchObject(
      {
        status: 400,
      },
    );
  });
  it("error with unavailable names builds toast", async () => {
    mockClient.POST.mockResolvedValue({
      error: { detail: "nope", unavailable_items: [{ name: "A" }] },
    });
    expect(
      await actions.reorderPast!(
        actionEvent(
          form([
            ["source_order_id", "s"],
            ["supply_date", "d"],
          ]),
        ),
      ),
    ).toMatchObject({ status: 409, data: { reorderToast: "今日皆無供應：A", error: "nope" } });
  });
  it("error without items uses detail then generic", async () => {
    mockClient.POST.mockResolvedValue({ error: { detail: "fd" } });
    expect(
      await actions.reorderPast!(
        actionEvent(
          form([
            ["source_order_id", "s"],
            ["supply_date", "d"],
          ]),
        ),
      ),
    ).toMatchObject({ data: { reorderToast: "fd", error: "fd" } });
    mockClient.POST.mockResolvedValue({ error: {} });
    expect(
      await actions.reorderPast!(
        actionEvent(
          form([
            ["source_order_id", "s"],
            ["supply_date", "d"],
          ]),
        ),
      ),
    ).toMatchObject({ data: { reorderToast: "今日皆無供應", error: "reorder failed" } });
  });
  it("fails when no new_order_id", async () => {
    mockClient.POST.mockResolvedValue({ data: {} });
    expect(
      await actions.reorderPast!(
        actionEvent(
          form([
            ["source_order_id", "s"],
            ["supply_date", "d"],
          ]),
        ),
      ),
    ).toMatchObject({ status: 500 });
  });
  it("redirects with partial query when some unavailable", async () => {
    mockClient.POST.mockResolvedValue({
      data: { new_order_id: "n1", unavailable_items: [{ name: "X" }] },
    });
    await expect(
      actions.reorderPast!(
        actionEvent(
          form([
            ["source_order_id", "s"],
            ["supply_date", "d"],
          ]),
        ),
      ),
    ).rejects.toMatchObject({
      status: 303,
      location: expect.stringContaining("/orders/n1?reorder=partial"),
    });
  });
  it("redirects plainly when all available", async () => {
    mockClient.POST.mockResolvedValue({ data: { new_order_id: "n2" } });
    await expect(
      actions.reorderPast!(
        actionEvent(
          form([
            ["source_order_id", "s"],
            ["supply_date", "d"],
          ]),
        ),
      ),
    ).rejects.toMatchObject({ status: 303, location: "/orders/n2" });
  });
});
