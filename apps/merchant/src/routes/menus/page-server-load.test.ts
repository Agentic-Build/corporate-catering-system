import { describe, it, expect, vi, beforeEach } from "vitest";

const { mockClient } = vi.hoisted(() => ({
  mockClient: { GET: vi.fn(), POST: vi.fn() },
}));
vi.mock("$lib/server/api", () => ({ apiFor: () => mockClient }));

import { actions, load } from "./+page.server";

const VENDOR = { id: "u1", role: "vendor_operator" };

function loadEvent(search = "", user: unknown = VENDOR) {
  return { locals: { user, apiToken: "t" }, url: new URL("http://x/menus" + search) } as never;
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
});

describe("menus load", () => {
  it("redirects unauthenticated", async () => {
    await expect(
      load({ locals: {}, url: new URL("http://x/menus") } as never),
    ).rejects.toMatchObject({
      status: 303,
    });
  });

  it("returns items with includeArchived=false default", async () => {
    mockClient.GET.mockResolvedValue({ data: { items: [{ id: "m1" }] } });
    const res = (await load(loadEvent())) as { items: unknown[]; includeArchived: boolean };
    expect(res.items).toHaveLength(1);
    expect(res.includeArchived).toBe(false);
    expect(mockClient.GET).toHaveBeenCalledWith(
      "/api/merchant/menu-items",
      expect.objectContaining({ params: { query: { include_archived: false } } }),
    );
  });

  it("honors archived=1 and defaults items on throw / missing data", async () => {
    mockClient.GET.mockRejectedValueOnce(new Error("boom"));
    let res = (await load(loadEvent("?archived=1"))) as {
      items: unknown[];
      includeArchived: boolean;
    };
    expect(res.includeArchived).toBe(true);
    expect(res.items).toEqual([]);
    mockClient.GET.mockResolvedValueOnce({ data: {} });
    res = (await load(loadEvent("?archived=1"))) as { items: unknown[]; includeArchived: boolean };
    expect(res.items).toEqual([]);
  });
});

describe("menus.copy branches", () => {
  it("rejects unauthenticated", async () => {
    const res = await actions.copy!(actionEvent(form({ id: "m1" }), null));
    expect(res).toMatchObject({ status: 401 });
  });

  it("fails on missing id", async () => {
    const res = await actions.copy!(actionEvent(form({ id: "" })));
    expect(res).toMatchObject({ status: 400, data: { error: "缺少品項 id" } });
  });

  it("fails on API error", async () => {
    mockClient.POST.mockResolvedValue({ error: { detail: "x" } });
    const res = await actions.copy!(actionEvent(form({ id: "m1" })));
    expect(res).toMatchObject({ status: 400, data: { error: "複製菜單失敗，請稍後再試。" } });
  });

  it("redirects to /menus when copy returns no new id", async () => {
    mockClient.POST.mockResolvedValue({ data: { item: {} } });
    await expect(actions.copy!(actionEvent(form({ id: "m1" })))).rejects.toMatchObject({
      status: 303,
      location: "/menus",
    });
  });
});

describe("menus.delete branches", () => {
  it("rejects unauthenticated", async () => {
    const res = await actions.delete!(actionEvent(form({ id: "m1" }), null));
    expect(res).toMatchObject({ status: 401 });
  });

  it("fails on missing id", async () => {
    const res = await actions.delete!(actionEvent(form({ id: "" })));
    expect(res).toMatchObject({ status: 400, data: { error: "缺少品項 id" } });
  });

  it("fails on API error", async () => {
    mockClient.POST.mockResolvedValue({ error: { detail: "x" } });
    const res = await actions.delete!(actionEvent(form({ id: "m1" })));
    expect(res).toMatchObject({ status: 400, data: { error: "刪除菜單失敗，請稍後再試。" } });
  });
});
