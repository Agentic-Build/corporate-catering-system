import { describe, it, expect, vi, beforeEach } from "vitest";

const { mockClient } = vi.hoisted(() => ({ mockClient: { GET: vi.fn(), POST: vi.fn() } }));
vi.mock("$lib/server/env", () => ({ API_BASE_URL: "http://x" }));
vi.mock("@tbite/api-client", () => ({ createApiClient: () => mockClient }));

import { load, actions } from "./+page.server";

const USER = { id: "u1" };
function loadEvent(user: unknown = USER) {
  return { locals: { user, apiToken: "t" }, url: new URL("http://h/scan") } as never;
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

describe("scan load", () => {
  it("redirects to login when unauthenticated", async () => {
    await expect(load(loadEvent(null))).rejects.toMatchObject({
      status: 303,
      location: "/login?return_to=%2Fscan",
    });
  });
  it("returns the user when authenticated", async () => {
    expect(await load(loadEvent())).toEqual({ user: USER });
  });
});

describe("scan action", () => {
  it("401 when unauthenticated", async () => {
    expect(await actions.scan!(actionEvent(form([]), null))).toMatchObject({ status: 401 });
  });
  it("400 when order id blank", async () => {
    expect(await actions.scan!(actionEvent(form([["orderId", "  "]])))).toMatchObject({
      status: 400,
      data: { error: "未取得訂單編號" },
    });
  });
  it("succeeds when pickup has no error", async () => {
    mockClient.POST.mockResolvedValue({ data: {} });
    expect(await actions.scan!(actionEvent(form([["orderId", "o1"]])))).toEqual({
      ok: true,
      pickedUpId: "o1",
    });
  });
  it("maps each pickup error status to its message", async () => {
    const cases: Array<[number, string]> = [
      [403, "這不是您本人的訂單，無法核銷。"],
      [404, "找不到這筆訂單。"],
      [409, "尚無法領取：請確認商家已掃描出餐（備餐完成），且此單尚未被領取。"],
      [500, "核銷失敗，請稍後再試。"],
    ];
    for (const [status, msg] of cases) {
      mockClient.POST.mockResolvedValue({ error: {}, response: { status } });
      expect(await actions.scan!(actionEvent(form([["orderId", "o1"]])))).toMatchObject({
        status: 400,
        data: { error: msg, orderId: "o1" },
      });
    }
  });
  it("uses 0 when response has no status", async () => {
    mockClient.POST.mockResolvedValue({ error: {}, response: undefined });
    expect(await actions.scan!(actionEvent(form([["orderId", "o1"]])))).toMatchObject({
      data: { error: "核銷失敗，請稍後再試。" },
    });
  });
});

describe("manual action", () => {
  it("401 when unauthenticated", async () => {
    expect(await actions.manual!(actionEvent(form([]), null))).toMatchObject({ status: 401 });
  });
  it("400 when code blank", async () => {
    expect(await actions.manual!(actionEvent(form([["code", " "]])))).toMatchObject({
      status: 400,
      data: { manual: true },
    });
  });
  it("404 when no matching ready order", async () => {
    mockClient.GET.mockResolvedValue({ data: { items: [{ id: "abcd1234ef", order_number: 5, status: "placed" }] } });
    expect(await actions.manual!(actionEvent(form([["code", "12345678"]])))).toMatchObject({
      status: 404,
      data: { manual: true },
    });
  });
  it("400 when multiple matches", async () => {
    mockClient.GET.mockResolvedValue({
      data: {
        items: [
          { id: "11111111aa", order_number: 7, status: "ready" },
          { id: "11111111bb", order_number: 7, status: "ready" },
        ],
      },
    });
    expect(await actions.manual!(actionEvent(form([["code", "11111111"]])))).toMatchObject({
      status: 400,
      data: { manual: true },
    });
  });
  it("matches by order_number and picks up", async () => {
    mockClient.GET.mockResolvedValue({
      data: { items: [{ id: "ID-FULL-1", order_number: 42, status: "ready" }] },
    });
    mockClient.POST.mockResolvedValue({ data: {} });
    expect(await actions.manual!(actionEvent(form([["code", "42"]])))).toEqual({
      ok: true,
      pickedUpId: "ID-FULL-1",
    });
  });
  it("matches by full id (lowercased)", async () => {
    mockClient.GET.mockResolvedValue({
      data: { items: [{ id: "AbCdEfGhIj", order_number: 1, status: "ready" }] },
    });
    mockClient.POST.mockResolvedValue({ data: {} });
    expect(await actions.manual!(actionEvent(form([["code", "abcdefghij"]])))).toEqual({
      ok: true,
      pickedUpId: "AbCdEfGhIj",
    });
  });
  it("matches by 8-char prefix and surfaces pickup error", async () => {
    mockClient.GET.mockResolvedValue({
      data: { items: [{ id: "PREFIX12tail", order_number: 9, status: "ready" }] },
    });
    mockClient.POST.mockResolvedValue({ error: {}, response: { status: 409 } });
    expect(await actions.manual!(actionEvent(form([["code", "prefix12"]])))).toMatchObject({
      status: 400,
      data: { manual: true, error: expect.stringContaining("尚無法領取") },
    });
  });
  it("defaults items to empty when list response lacks data", async () => {
    mockClient.GET.mockResolvedValue({});
    expect(await actions.manual!(actionEvent(form([["code", "zzzz"]])))).toMatchObject({
      status: 404,
    });
  });
});
