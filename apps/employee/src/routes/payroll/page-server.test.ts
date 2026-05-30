import { describe, it, expect, vi, beforeEach } from "vitest";

const { mockClient } = vi.hoisted(() => ({ mockClient: { GET: vi.fn(), POST: vi.fn() } }));
vi.mock("$lib/server/env", () => ({ API_BASE_URL: "http://x" }));
vi.mock("@tbite/api-client", () => ({ createApiClient: () => mockClient }));

import { load, actions } from "./+page.server";

const USER = { id: "u1" };
function loadEvent(user: unknown = USER) {
  return { locals: { user, apiToken: "t" }, url: new URL("http://h/payroll") } as never;
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

describe("payroll load", () => {
  it("redirects to login when unauthenticated", async () => {
    await expect(load(loadEvent(null))).rejects.toMatchObject({
      status: 303,
      location: "/login?return_to=%2Fpayroll",
    });
  });
  it("aggregates entries and current lines", async () => {
    mockClient.GET.mockImplementation((path: string) =>
      Promise.resolve(
        path === "/api/employee/payroll"
          ? { data: { items: [{ id: "e1" }] } }
          : { data: { lines: [{ id: "l1" }], total_minor: 250 } },
      ),
    );
    const res = await load(loadEvent());
    expect(res).toMatchObject({
      entries: [{ id: "e1" }],
      currentLines: [{ id: "l1" }],
      currentTotalMinor: 250,
    });
  });
  it("defaults to empties when responses lack data", async () => {
    mockClient.GET.mockResolvedValue({});
    const res = await load(loadEvent());
    expect(res).toMatchObject({ entries: [], currentLines: [], currentTotalMinor: 0 });
  });
  it("defaults entries/lines arrays when fields absent", async () => {
    mockClient.GET.mockImplementation((path: string) =>
      Promise.resolve(
        path === "/api/employee/payroll" ? { data: {} } : { data: { total_minor: 0 } },
      ),
    );
    const res = (await load(loadEvent())) as { entries: unknown[]; currentLines: unknown[] };
    expect(res.entries).toEqual([]);
    expect(res.currentLines).toEqual([]);
  });
});

describe("rate action", () => {
  it("401 when unauthenticated", async () => {
    expect(await actions.rate!(actionEvent(form([]), null))).toMatchObject({ status: 401 });
  });
  it("400 when order id missing", async () => {
    expect(await actions.rate!(actionEvent(form([["score", "5"]])))).toMatchObject({
      status: 400,
      data: { ratingError: "缺少訂單資訊" },
    });
  });
  it("400 on out-of-range score", async () => {
    expect(
      await actions.rate!(
        actionEvent(
          form([
            ["order_id", "o1"],
            ["score", "9"],
          ]),
        ),
      ),
    ).toMatchObject({
      data: { ratingError: "請選擇 1 至 5 顆星的評分" },
    });
  });
  it("400 on non-integer score", async () => {
    expect(
      await actions.rate!(
        actionEvent(
          form([
            ["order_id", "o1"],
            ["score", "abc"],
          ]),
        ),
      ),
    ).toMatchObject({
      data: { ratingError: "請選擇 1 至 5 顆星的評分" },
    });
  });
  it("400 when comment too long", async () => {
    expect(
      await actions.rate!(
        actionEvent(
          form([
            ["order_id", "o1"],
            ["score", "5"],
            ["comment", "a".repeat(501)],
          ]),
        ),
      ),
    ).toMatchObject({ data: { ratingError: "留言不可超過 500 字" } });
  });
  it("409 maps to already-rated", async () => {
    mockClient.POST.mockResolvedValue({ error: {}, response: { status: 409 } });
    expect(
      await actions.rate!(
        actionEvent(
          form([
            ["order_id", "o1"],
            ["score", "5"],
          ]),
        ),
      ),
    ).toMatchObject({
      status: 409,
      data: { ratingError: "此訂單已評分過了。" },
    });
  });
  it("non-409 uses detail then default", async () => {
    mockClient.POST.mockResolvedValue({ error: { detail: "d" }, response: { status: 500 } });
    expect(
      await actions.rate!(
        actionEvent(
          form([
            ["order_id", "o1"],
            ["score", "5"],
          ]),
        ),
      ),
    ).toMatchObject({
      data: { ratingError: "d" },
    });
    mockClient.POST.mockResolvedValue({ error: {}, response: { status: 500 } });
    expect(
      await actions.rate!(
        actionEvent(
          form([
            ["order_id", "o1"],
            ["score", "5"],
          ]),
        ),
      ),
    ).toMatchObject({
      data: { ratingError: "送出評分失敗，請稍後再試。" },
    });
  });
  it("succeeds", async () => {
    mockClient.POST.mockResolvedValue({ data: { rating: { score: 5 } } });
    expect(
      await actions.rate!(
        actionEvent(
          form([
            ["order_id", "o1"],
            ["score", "5"],
            ["comment", "ok"],
          ]),
        ),
      ),
    ).toEqual({
      ratingOk: true,
      orderId: "o1",
      rating: { score: 5 },
    });
  });
});

describe("complain action", () => {
  it("401 when unauthenticated", async () => {
    expect(await actions.complain!(actionEvent(form([]), null))).toMatchObject({ status: 401 });
  });
  it("400 when order id missing", async () => {
    expect(await actions.complain!(actionEvent(form([["category", "quality"]])))).toMatchObject({
      data: { complaintError: "缺少訂單資訊" },
    });
  });
  it("400 on invalid category", async () => {
    expect(
      await actions.complain!(
        actionEvent(
          form([
            ["order_id", "o1"],
            ["category", "nope"],
          ]),
        ),
      ),
    ).toMatchObject({ data: { complaintError: "請選擇問題類型" } });
  });
  it("400 when description out of bounds", async () => {
    expect(
      await actions.complain!(
        actionEvent(
          form([
            ["order_id", "o1"],
            ["category", "quality"],
            ["description", "abc"],
          ]),
        ),
      ),
    ).toMatchObject({ data: { complaintError: "問題描述需介於 5 至 1000 字" } });
  });
  it("409 maps to existing complaint", async () => {
    mockClient.POST.mockResolvedValue({ error: {}, response: { status: 409 } });
    expect(
      await actions.complain!(
        actionEvent(
          form([
            ["order_id", "o1"],
            ["category", "quality"],
            ["description", "valid issue"],
          ]),
        ),
      ),
    ).toMatchObject({ status: 409, data: { complaintError: "此訂單已有未結案的客訴。" } });
  });
  it("non-409 uses detail then default", async () => {
    mockClient.POST.mockResolvedValue({ error: { detail: "d" }, response: { status: 500 } });
    expect(
      await actions.complain!(
        actionEvent(
          form([
            ["order_id", "o1"],
            ["category", "quality"],
            ["description", "valid issue"],
          ]),
        ),
      ),
    ).toMatchObject({ data: { complaintError: "d" } });
    mockClient.POST.mockResolvedValue({ error: {}, response: { status: 500 } });
    expect(
      await actions.complain!(
        actionEvent(
          form([
            ["order_id", "o1"],
            ["category", "quality"],
            ["description", "valid issue"],
          ]),
        ),
      ),
    ).toMatchObject({ data: { complaintError: "送出客訴失敗，請稍後再試。" } });
  });
  it("succeeds", async () => {
    mockClient.POST.mockResolvedValue({ data: { complaint: { id: "c1" } } });
    expect(
      await actions.complain!(
        actionEvent(
          form([
            ["order_id", "o1"],
            ["category", "quality"],
            ["description", "valid issue"],
          ]),
        ),
      ),
    ).toEqual({ complaintOk: true, orderId: "o1", complaint: { id: "c1" } });
  });
});
