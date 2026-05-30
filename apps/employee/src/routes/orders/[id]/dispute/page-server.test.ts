import { describe, it, expect, vi, beforeEach } from "vitest";

const { mockClient } = vi.hoisted(() => ({ mockClient: { GET: vi.fn(), POST: vi.fn() } }));
vi.mock("$lib/server/env", () => ({ API_BASE_URL: "http://x" }));
vi.mock("@tbite/api-client", () => ({ createApiClient: () => mockClient }));

import { load, actions } from "./+page.server";

const USER = { id: "u1" };
function loadEvent(user: unknown = USER, id = "ord1") {
  return {
    locals: { user, apiToken: "t" },
    params: { id },
    url: new URL("http://h/orders/" + id + "/dispute"),
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
});

describe("dispute load", () => {
  it("redirects to login when unauthenticated", async () => {
    await expect(load(loadEvent(null))).rejects.toMatchObject({
      status: 303,
      location: "/login?return_to=%2Forders%2Ford1%2Fdispute",
    });
  });
  it("throws 404 when order request errors", async () => {
    mockClient.GET.mockResolvedValue({ error: { detail: "x" } });
    await expect(load(loadEvent())).rejects.toMatchObject({ status: 404 });
  });
  it("throws 404 when order data absent", async () => {
    mockClient.GET.mockResolvedValue({ data: undefined });
    await expect(load(loadEvent())).rejects.toMatchObject({ status: 404 });
  });
  it("marks disputable when status is in the allow-set", async () => {
    mockClient.GET.mockResolvedValue({ data: { order: { status: "no_show" } } });
    expect(((await load(loadEvent())) as { disputable: boolean }).disputable).toBe(true);
  });
  it("marks non-disputable otherwise", async () => {
    mockClient.GET.mockResolvedValue({ data: { order: { status: "placed" } } });
    expect(((await load(loadEvent())) as { disputable: boolean }).disputable).toBe(false);
  });
});

describe("dispute default action", () => {
  it("401 when unauthenticated", async () => {
    expect(await actions.default!(actionEvent(form([]), null))).toMatchObject({ status: 401 });
  });
  it("400 when reason blank", async () => {
    expect(await actions.default!(actionEvent(form([["reason", "  "]])))).toMatchObject({
      status: 400,
      data: { error: "請填寫申訴原因" },
    });
  });
  it("404 error maps to not-found message", async () => {
    mockClient.POST.mockResolvedValue({ error: { status: 404 } });
    expect(await actions.default!(actionEvent(form([["reason", "bad food"]])))).toMatchObject({
      status: 404,
      data: { error: "找不到訂單。" },
    });
  });
  it("other error uses generic message and default status", async () => {
    mockClient.POST.mockResolvedValue({ error: {} });
    expect(await actions.default!(actionEvent(form([["reason", "bad food"]])))).toMatchObject({
      status: 400,
      data: { error: "送出申訴失敗，請稍後再試。" },
    });
  });
  it("redirects to disputes on success", async () => {
    mockClient.POST.mockResolvedValue({ data: {} });
    await expect(
      actions.default!(actionEvent(form([["reason", "bad food"]]))),
    ).rejects.toMatchObject({
      status: 303,
      location: "/disputes",
    });
  });
});
