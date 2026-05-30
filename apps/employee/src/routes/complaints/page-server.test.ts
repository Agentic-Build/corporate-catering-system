import { describe, it, expect, vi, beforeEach } from "vitest";

const { mockClient } = vi.hoisted(() => ({
  mockClient: { GET: vi.fn(), POST: vi.fn() },
}));
vi.mock("$lib/server/env", () => ({ API_BASE_URL: "http://x" }));
vi.mock("@tbite/api-client", () => ({ createApiClient: () => mockClient }));

import { load, actions } from "./+page.server";

const USER = { id: "u1" };
function loadEvent(user: unknown = USER) {
  return { locals: { user, apiToken: "t" }, url: new URL("http://h/complaints") } as never;
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

describe("complaints load", () => {
  it("redirects to login when unauthenticated", async () => {
    await expect(load(loadEvent(null))).rejects.toMatchObject({
      status: 303,
      location: "/login?return_to=%2Fcomplaints",
    });
  });
  it("returns complaints on success", async () => {
    mockClient.GET.mockResolvedValue({ data: { items: [{ id: "c1" }] } });
    const res = await load(loadEvent());
    expect(res).toMatchObject({ complaints: [{ id: "c1" }], error: undefined });
  });
  it("defaults items to empty array", async () => {
    mockClient.GET.mockResolvedValue({ data: {} });
    const res = await load(loadEvent());
    expect(res.complaints).toEqual([]);
  });
  it("surfaces error detail then default", async () => {
    mockClient.GET.mockResolvedValue({ error: { detail: "boom" } });
    expect((await load(loadEvent())).error).toBe("boom");
    mockClient.GET.mockResolvedValue({ error: {} });
    expect((await load(loadEvent())).error).toBe("載入客訴失敗");
  });
});

describe("escalate action", () => {
  it("401 when unauthenticated", async () => {
    expect(await actions.escalate!(actionEvent(form([]), null))).toMatchObject({ status: 401 });
  });
  it("400 when id missing", async () => {
    expect(await actions.escalate!(actionEvent(form([])))).toMatchObject({ status: 400 });
  });
  it("409 maps to friendly message", async () => {
    mockClient.POST.mockResolvedValue({ error: {}, response: { status: 409 } });
    expect(await actions.escalate!(actionEvent(form([["id", "c1"]])))).toMatchObject({
      status: 409,
      data: { error: "尚未滿 24 小時或狀態不允許升級。" },
    });
  });
  it("non-409 uses detail then default", async () => {
    mockClient.POST.mockResolvedValue({ error: { detail: "x" }, response: { status: 500 } });
    expect(await actions.escalate!(actionEvent(form([["id", "c1"]])))).toMatchObject({
      status: 500,
      data: { error: "x" },
    });
    mockClient.POST.mockResolvedValue({ error: {}, response: { status: 500 } });
    expect(await actions.escalate!(actionEvent(form([["id", "c1"]])))).toMatchObject({
      data: { error: "升級失敗，請稍後再試。" },
    });
  });
  it("succeeds", async () => {
    mockClient.POST.mockResolvedValue({ data: {} });
    expect(await actions.escalate!(actionEvent(form([["id", "c1"]])))).toEqual({ ok: true });
  });
});

describe("resolve action", () => {
  it("401 when unauthenticated", async () => {
    expect(await actions.resolve!(actionEvent(form([]), null))).toMatchObject({ status: 401 });
  });
  it("400 when id missing", async () => {
    expect(await actions.resolve!(actionEvent(form([])))).toMatchObject({ status: 400 });
  });
  it("surfaces error detail then default", async () => {
    mockClient.POST.mockResolvedValue({ error: { detail: "e" }, response: { status: 400 } });
    expect(await actions.resolve!(actionEvent(form([["id", "c1"]])))).toMatchObject({
      status: 400,
      data: { error: "e" },
    });
    mockClient.POST.mockResolvedValue({ error: {}, response: { status: 400 } });
    expect(await actions.resolve!(actionEvent(form([["id", "c1"]])))).toMatchObject({
      data: { error: "結案失敗，請稍後再試。" },
    });
  });
  it("succeeds", async () => {
    mockClient.POST.mockResolvedValue({ data: {} });
    expect(await actions.resolve!(actionEvent(form([["id", "c1"]])))).toEqual({ ok: true });
  });
});
