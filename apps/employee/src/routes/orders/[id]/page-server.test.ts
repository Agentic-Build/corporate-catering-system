import { describe, it, expect, vi, beforeEach } from "vitest";

const { mockClient } = vi.hoisted(() => ({
  mockClient: { GET: vi.fn(), POST: vi.fn(), PUT: vi.fn() },
}));
vi.mock("$lib/server/env", () => ({ API_BASE_URL: "http://x" }));
vi.mock("@tbite/api-client", () => ({ createApiClient: () => mockClient }));

import { load, actions } from "./+page.server";

const USER = { id: "u1" };
function loadEvent(user: unknown = USER, id = "ord1") {
  return {
    locals: { user, apiToken: "t" },
    params: { id },
    url: new URL("http://h/orders/" + id),
  } as never;
}
function actionEvent(fd: FormData, user: unknown = USER, id = "ord1") {
  return {
    request: { formData: async () => fd },
    locals: { user, apiToken: "t" },
    params: { id },
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
  mockClient.PUT.mockReset();
});

describe("order detail load", () => {
  it("redirects to login when unauthenticated", async () => {
    await expect(load(loadEvent(null))).rejects.toMatchObject({ status: 303 });
  });
  it("throws 404 when order request errors", async () => {
    mockClient.GET.mockResolvedValue({ error: { detail: "x" } });
    await expect(load(loadEvent())).rejects.toMatchObject({ status: 404 });
  });
  it("throws 404 when order data absent", async () => {
    mockClient.GET.mockResolvedValue({ data: undefined });
    await expect(load(loadEvent())).rejects.toMatchObject({ status: 404 });
  });
  it("returns a placed order with vendor-filtered menu", async () => {
    mockClient.GET.mockImplementation((path: string) =>
      Promise.resolve(
        path === "/api/employee/orders/{id}"
          ? { data: { order: { status: "placed", plant: "p", supply_date: "d", vendor_id: "v1" } } }
          : { data: { items: [{ vendor_id: "v1" }, { vendor_id: "v2" }] } },
      ),
    );
    const res = (await load(loadEvent())) as {
      menu?: unknown[];
      complaint?: { id?: string };
      rating?: unknown;
    };
    expect(res.menu).toEqual([{ vendor_id: "v1" }]);
    expect(res.complaint).toBeUndefined();
    expect(res.rating).toBeUndefined();
  });
  it("placed order with no menu data leaves menu undefined", async () => {
    mockClient.GET.mockImplementation((path: string) =>
      Promise.resolve(
        path === "/api/employee/orders/{id}"
          ? { data: { order: { status: "placed", plant: "p", supply_date: "d", vendor_id: "v1" } } }
          : { data: undefined },
      ),
    );
    const res = (await load(loadEvent())) as {
      menu?: unknown[];
      complaint?: { id?: string };
      rating?: unknown;
    };
    expect(res.menu).toBeUndefined();
  });
  it("picked_up order fetches matching complaint and rating", async () => {
    mockClient.GET.mockImplementation((path: string) => {
      if (path === "/api/employee/orders/{id}")
        return Promise.resolve({ data: { order: { status: "picked_up" } } });
      if (path === "/api/employee/complaints")
        return Promise.resolve({ data: { items: [{ order_id: "ord1", id: "c1" }] } });
      return Promise.resolve({ data: { rating: { score: 4 } } });
    });
    const res = (await load(loadEvent())) as {
      menu?: unknown[];
      complaint?: { id?: string };
      rating?: unknown;
    };
    expect(res.complaint).toMatchObject({ id: "c1" });
    expect(res.rating).toEqual({ score: 4 });
  });
  it("picked_up order with no complaint/rating data leaves them undefined", async () => {
    mockClient.GET.mockImplementation((path: string) => {
      if (path === "/api/employee/orders/{id}")
        return Promise.resolve({ data: { order: { status: "picked_up" } } });
      if (path === "/api/employee/complaints") return Promise.resolve({ data: undefined });
      return Promise.resolve({ data: undefined });
    });
    const res = (await load(loadEvent())) as {
      menu?: unknown[];
      complaint?: { id?: string };
      rating?: unknown;
    };
    expect(res.complaint).toBeUndefined();
    expect(res.rating).toBeUndefined();
  });
  it("picked_up order with empty complaints list yields undefined complaint", async () => {
    mockClient.GET.mockImplementation((path: string) => {
      if (path === "/api/employee/orders/{id}")
        return Promise.resolve({ data: { order: { status: "picked_up" } } });
      if (path === "/api/employee/complaints") return Promise.resolve({ data: {} });
      return Promise.resolve({ data: { rating: null } });
    });
    const res = (await load(loadEvent())) as {
      menu?: unknown[];
      complaint?: { id?: string };
      rating?: unknown;
    };
    expect(res.complaint).toBeUndefined();
  });
});

describe("cancel action", () => {
  it("401 when unauthenticated", async () => {
    expect(
      await actions.cancel!({ locals: { user: null }, params: { id: "o" } } as never),
    ).toMatchObject({ status: 401 });
  });
  it("400 on error", async () => {
    mockClient.POST.mockResolvedValue({ error: { detail: "no" } });
    expect(
      await actions.cancel!({
        locals: { user: USER, apiToken: "t" },
        params: { id: "o" },
      } as never),
    ).toMatchObject({
      status: 400,
      data: { error: "no" },
    });
  });
  it("redirects on success", async () => {
    mockClient.POST.mockResolvedValue({ data: {} });
    await expect(
      actions.cancel!({ locals: { user: USER, apiToken: "t" }, params: { id: "o9" } } as never),
    ).rejects.toMatchObject({ status: 303, location: "/orders/o9" });
  });
});

describe("modify action", () => {
  it("401 when unauthenticated", async () => {
    expect(await actions.modify!(actionEvent(form([]), null))).toMatchObject({ status: 401 });
  });
  it("400 on invalid JSON", async () => {
    expect(await actions.modify!(actionEvent(form([["items", "{bad"]])))).toMatchObject({
      data: { modifyError: "資料格式錯誤，請重新操作。" },
    });
  });
  it("400 when no valid items remain (filters non-objects/zero qty/bad shape)", async () => {
    const items = JSON.stringify([
      null,
      { menu_item_id: 5, qty: 1 },
      { menu_item_id: "a", qty: 0 },
      { menu_item_id: "b", qty: 1.5 },
    ]);
    expect(await actions.modify!(actionEvent(form([["items", items]])))).toMatchObject({
      data: { modifyError: expect.stringContaining("至少需保留") },
    });
  });
  it("400 when items field absent (defaults to [])", async () => {
    expect(await actions.modify!(actionEvent(form([])))).toMatchObject({
      data: { modifyError: expect.stringContaining("至少需保留") },
    });
  });
  it("400 when notes too long", async () => {
    const items = JSON.stringify([{ menu_item_id: "a", qty: 1 }]);
    expect(
      await actions.modify!(
        actionEvent(
          form([
            ["items", items],
            ["notes", "x".repeat(501)],
          ]),
        ),
      ),
    ).toMatchObject({ data: { modifyError: "備註不可超過 500 字" } });
  });
  it("409 uses detail then default", async () => {
    const items = JSON.stringify([{ menu_item_id: "a", qty: 1 }]);
    mockClient.PUT.mockResolvedValue({ error: { detail: "d" }, response: { status: 409 } });
    expect(await actions.modify!(actionEvent(form([["items", items]])))).toMatchObject({
      data: { modifyError: "d" },
    });
    mockClient.PUT.mockResolvedValue({ error: {}, response: { status: 409 } });
    expect(await actions.modify!(actionEvent(form([["items", items]])))).toMatchObject({
      data: { modifyError: "餐點數量超過剩餘供應量，或已過截單時間。" },
    });
  });
  it("non-409 uses detail then default", async () => {
    const items = JSON.stringify([{ menu_item_id: "a", qty: 1 }]);
    mockClient.PUT.mockResolvedValue({ error: { detail: "d2" }, response: { status: 500 } });
    expect(await actions.modify!(actionEvent(form([["items", items]])))).toMatchObject({
      data: { modifyError: "d2" },
    });
    mockClient.PUT.mockResolvedValue({ error: {}, response: { status: 500 } });
    expect(await actions.modify!(actionEvent(form([["items", items]])))).toMatchObject({
      data: { modifyError: "修改訂單失敗，請稍後再試。" },
    });
  });
  it("redirects on success", async () => {
    const items = JSON.stringify([{ menu_item_id: "a", qty: 2 }]);
    mockClient.PUT.mockResolvedValue({ data: {} });
    await expect(
      actions.modify!(
        actionEvent(
          form([
            ["items", items],
            ["notes", "ok"],
          ]),
        ),
      ),
    ).rejects.toMatchObject({
      status: 303,
      location: "/orders/ord1",
    });
  });
});

describe("rate action", () => {
  it("401 when unauthenticated", async () => {
    expect(await actions.rate!(actionEvent(form([]), null))).toMatchObject({ status: 401 });
  });
  it("400 on invalid score", async () => {
    expect(await actions.rate!(actionEvent(form([["score", "0"]])))).toMatchObject({
      data: { ratingError: "請選擇 1 至 5 顆星的評分" },
    });
  });
  it("400 when comment too long", async () => {
    expect(
      await actions.rate!(
        actionEvent(
          form([
            ["score", "5"],
            ["comment", "a".repeat(501)],
          ]),
        ),
      ),
    ).toMatchObject({
      data: { ratingError: "留言不可超過 500 字" },
    });
  });
  it("409 then non-409 errors", async () => {
    mockClient.POST.mockResolvedValue({ error: {}, response: { status: 409 } });
    expect(await actions.rate!(actionEvent(form([["score", "5"]])))).toMatchObject({
      data: { ratingError: "此訂單已評分過了。" },
    });
    mockClient.POST.mockResolvedValue({ error: { detail: "d" }, response: { status: 500 } });
    expect(await actions.rate!(actionEvent(form([["score", "5"]])))).toMatchObject({
      data: { ratingError: "d" },
    });
    mockClient.POST.mockResolvedValue({ error: {}, response: { status: 500 } });
    expect(await actions.rate!(actionEvent(form([["score", "5"]])))).toMatchObject({
      data: { ratingError: "送出評分失敗，請稍後再試。" },
    });
  });
  it("succeeds", async () => {
    mockClient.POST.mockResolvedValue({ data: { rating: { score: 5 } } });
    expect(
      await actions.rate!(
        actionEvent(
          form([
            ["score", "5"],
            ["comment", "ok"],
          ]),
        ),
      ),
    ).toEqual({
      ratingOk: true,
      rating: { score: 5 },
    });
  });
});

describe("complain action", () => {
  it("401 when unauthenticated", async () => {
    expect(await actions.complain!(actionEvent(form([]), null))).toMatchObject({ status: 401 });
  });
  it("400 on invalid category", async () => {
    expect(await actions.complain!(actionEvent(form([["category", "nope"]])))).toMatchObject({
      data: { complaintError: "請選擇問題類型" },
    });
  });
  it("400 when description out of bounds", async () => {
    expect(
      await actions.complain!(
        actionEvent(
          form([
            ["category", "quality"],
            ["description", "abc"],
          ]),
        ),
      ),
    ).toMatchObject({ data: { complaintError: "問題描述需介於 5 至 1000 字" } });
  });
  it("409 then non-409 errors", async () => {
    const fd = () =>
      form([
        ["category", "quality"],
        ["description", "valid issue"],
      ]);
    mockClient.POST.mockResolvedValue({ error: {}, response: { status: 409 } });
    expect(await actions.complain!(actionEvent(fd()))).toMatchObject({
      data: { complaintError: "此訂單已有未結案的客訴。" },
    });
    mockClient.POST.mockResolvedValue({ error: { detail: "d" }, response: { status: 500 } });
    expect(await actions.complain!(actionEvent(fd()))).toMatchObject({
      data: { complaintError: "d" },
    });
    mockClient.POST.mockResolvedValue({ error: {}, response: { status: 500 } });
    expect(await actions.complain!(actionEvent(fd()))).toMatchObject({
      data: { complaintError: "送出客訴失敗，請稍後再試。" },
    });
  });
  it("succeeds", async () => {
    mockClient.POST.mockResolvedValue({ data: { complaint: { id: "c1" } } });
    expect(
      await actions.complain!(
        actionEvent(
          form([
            ["category", "quality"],
            ["description", "valid issue"],
          ]),
        ),
      ),
    ).toEqual({ complaintOk: true, complaint: { id: "c1" } });
  });
});
