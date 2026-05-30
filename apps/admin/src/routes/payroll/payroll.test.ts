import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";

const { mockClient } = vi.hoisted(() => ({
  mockClient: { GET: vi.fn(), POST: vi.fn(), PUT: vi.fn() },
}));
vi.mock("$lib/server/api", () => ({ apiFor: () => mockClient }));

import { load as listLoad } from "./+page.server";
import { load as detailLoad, actions as detailActions } from "./[id]/+page.server";
import { load as disputesLoad, actions as disputesActions } from "./[id]/disputes/+page.server";
import { load as newLoad, actions as newActions } from "./new/+page.server";

const ADMIN = { id: "u1", role: "welfare_admin" };

function form(entries: Record<string, string>): FormData {
  const fd = new FormData();
  for (const [k, v] of Object.entries(entries)) fd.append(k, v);
  return fd;
}
function event(fd: FormData, params: Record<string, string> = { id: "b1" }, user: unknown = ADMIN) {
  return {
    request: { formData: async () => fd },
    params,
    locals: { user, apiToken: "t" },
  } as never;
}
function loadEvent(opts: { user?: unknown; query?: string; params?: Record<string, string> } = {}) {
  return {
    locals: { user: "user" in opts ? opts.user : ADMIN, apiToken: "t" },
    url: new URL("http://x/p" + (opts.query ?? "")),
    params: opts.params ?? { id: "b1" },
    depends: vi.fn(),
  } as never;
}

beforeEach(() => {
  for (const fn of Object.values(mockClient)) fn.mockReset();
});

describe("payroll list load", () => {
  it("redirects anonymous and non-admin", async () => {
    await expect(listLoad(loadEvent({ user: undefined }))).rejects.toMatchObject({ status: 303 });
    await expect(listLoad(loadEvent({ user: { role: "x" } }))).rejects.toMatchObject({
      status: 303,
      location: "/login",
    });
  });
  it("lists with and without status filter, swallows errors", async () => {
    mockClient.GET.mockResolvedValue({ data: { items: [{ id: "b1" }] } });
    const res = (await listLoad(loadEvent({ query: "?status=draft" }))) as Record<string, unknown>;
    expect(res.batches).toEqual([{ id: "b1" }]);
    expect(mockClient.GET).toHaveBeenCalledWith("/api/admin/payroll/batches", {
      params: { query: { status: "draft" } },
    });
    await listLoad(loadEvent());
    expect(mockClient.GET).toHaveBeenLastCalledWith("/api/admin/payroll/batches", {
      params: { query: {} },
    });
    mockClient.GET.mockRejectedValue(new Error("x"));
    expect(((await listLoad(loadEvent())) as { batches: unknown[] }).batches).toEqual([]);
    mockClient.GET.mockResolvedValue({ data: null });
    expect(((await listLoad(loadEvent())) as { batches: unknown[] }).batches).toEqual([]);
  });
});

describe("payroll detail load", () => {
  it("redirects anonymous and non-admin", async () => {
    await expect(detailLoad(loadEvent({ user: undefined }))).rejects.toMatchObject({ status: 303 });
    await expect(detailLoad(loadEvent({ user: { role: "x" } }))).rejects.toMatchObject({
      status: 303,
      location: "/login",
    });
  });
  it("throws 404 when batch missing (error or no data)", async () => {
    mockClient.GET.mockResolvedValueOnce({ error: { detail: "x" } });
    await expect(detailLoad(loadEvent())).rejects.toMatchObject({ status: 404 });
    mockClient.GET.mockResolvedValueOnce({ data: null });
    await expect(detailLoad(loadEvent())).rejects.toMatchObject({ status: 404 });
  });
  it("returns batch with entries and exceptions", async () => {
    mockClient.GET.mockImplementation((path: string) => {
      if (path === "/api/admin/payroll/batches/{id}")
        return Promise.resolve({ data: { batch: { id: "b1" }, entries: [{ id: "e1" }] } });
      return Promise.resolve({ data: { items: [{ id: "x1" }] } });
    });
    const res = (await detailLoad(loadEvent())) as Record<string, unknown>;
    expect(res.batch).toEqual({ id: "b1" });
    expect(res.entries).toEqual([{ id: "e1" }]);
    expect(res.exceptions).toEqual([{ id: "x1" }]);
  });
  it("defaults entries and exceptions when absent", async () => {
    mockClient.GET.mockImplementation((path: string) => {
      if (path === "/api/admin/payroll/batches/{id}")
        return Promise.resolve({ data: { batch: { id: "b1" } } });
      return Promise.resolve({ data: null });
    });
    const res = (await detailLoad(loadEvent())) as Record<string, unknown>;
    expect(res.entries).toEqual([]);
    expect(res.exceptions).toEqual([]);
  });
});

describe("payroll detail actions", () => {
  it("lock redirects on success and fails on error", async () => {
    mockClient.POST.mockResolvedValue({ data: {} });
    await expect(detailActions.lock!(event(form({})))).rejects.toMatchObject({
      status: 303,
      location: "/payroll/b1",
    });
    mockClient.POST.mockResolvedValue({ error: { detail: "x" } });
    expect(await detailActions.lock!(event(form({})))).toMatchObject({ status: 500 });
  });
  it("flagException validates entry_id", async () => {
    expect(await detailActions.flagException!(event(form({})))).toMatchObject({
      status: 400,
      data: { exError: "請選擇要標記的月結明細" },
    });
  });
  it("flagException posts and redirects", async () => {
    mockClient.POST.mockResolvedValue({ data: {} });
    await expect(
      detailActions.flagException!(event(form({ entry_id: "e1", detail: " bad " }))),
    ).rejects.toMatchObject({ status: 303, location: "/payroll/b1" });
    expect(mockClient.POST).toHaveBeenCalledWith("/api/admin/payroll/batches/{id}/exceptions", {
      params: { path: { id: "b1" } },
      body: { entry_id: "e1", detail: "bad" },
    });
  });
  it("flagException returns server detail or fallback on error", async () => {
    mockClient.POST.mockResolvedValue({ error: { detail: "server says no" } });
    expect(await detailActions.flagException!(event(form({ entry_id: "e1" })))).toMatchObject({
      status: 400,
      data: { exError: "server says no" },
    });
    mockClient.POST.mockResolvedValue({ error: {} });
    expect(await detailActions.flagException!(event(form({ entry_id: "e1" })))).toMatchObject({
      status: 400,
      data: { exError: "標記例外失敗，請稍後再試。" },
    });
  });
  it("resolveException validates input", async () => {
    expect(await detailActions.resolveException!(event(form({})))).toMatchObject({ status: 400 });
    expect(
      await detailActions.resolveException!(event(form({ exception_id: "x1", status: "bogus" }))),
    ).toMatchObject({ status: 400, data: { exError: "例外解決參數不正確" } });
  });
  it("resolveException posts resolved/excluded and redirects", async () => {
    mockClient.POST.mockResolvedValue({ data: {} });
    await expect(
      detailActions.resolveException!(
        event(form({ exception_id: "x1", status: "resolved", resolution: " ok " })),
      ),
    ).rejects.toMatchObject({ status: 303, location: "/payroll/b1" });
    expect(mockClient.POST).toHaveBeenCalledWith("/api/admin/payroll/exceptions/{id}/resolve", {
      params: { path: { id: "x1" } },
      body: { status: "resolved", resolution: "ok" },
    });
    await expect(
      detailActions.resolveException!(event(form({ exception_id: "x1", status: "excluded" }))),
    ).rejects.toMatchObject({ status: 303 });
  });
  it("resolveException returns server detail or fallback on error", async () => {
    mockClient.POST.mockResolvedValue({ error: { detail: "boom" } });
    expect(
      await detailActions.resolveException!(
        event(form({ exception_id: "x1", status: "resolved" })),
      ),
    ).toMatchObject({ status: 400, data: { exError: "boom" } });
    mockClient.POST.mockResolvedValue({ error: {} });
    expect(
      await detailActions.resolveException!(
        event(form({ exception_id: "x1", status: "resolved" })),
      ),
    ).toMatchObject({ status: 400, data: { exError: "解決例外失敗，請稍後再試。" } });
  });
});

describe("payroll [id] disputes", () => {
  it("load redirects, returns batchId, applies status, swallows errors", async () => {
    await expect(disputesLoad(loadEvent({ user: undefined }))).rejects.toMatchObject({
      status: 303,
    });
    await expect(disputesLoad(loadEvent({ user: { role: "x" } }))).rejects.toMatchObject({
      status: 303,
      location: "/login",
    });
    mockClient.GET.mockResolvedValue({ data: { items: [{ id: "d1" }] } });
    const res = (await disputesLoad(loadEvent({ query: "?status=open" }))) as Record<
      string,
      unknown
    >;
    expect(res.batchId).toBe("b1");
    expect(res.disputes).toEqual([{ id: "d1" }]);
    expect(mockClient.GET).toHaveBeenCalledWith("/api/admin/payroll/disputes", {
      params: { query: { status: "open" } },
    });
    await disputesLoad(loadEvent());
    expect(mockClient.GET).toHaveBeenLastCalledWith("/api/admin/payroll/disputes", {
      params: { query: {} },
    });
    mockClient.GET.mockRejectedValue(new Error("x"));
    expect(((await disputesLoad(loadEvent())) as { disputes: unknown[] }).disputes).toEqual([]);
    mockClient.GET.mockResolvedValue({ data: null });
    expect(((await disputesLoad(loadEvent())) as { disputes: unknown[] }).disputes).toEqual([]);
  });
  it("resolveRefund validates and posts", async () => {
    expect(await disputesActions.resolveRefund!(event(form({})))).toMatchObject({ status: 400 });
    expect(
      await disputesActions.resolveRefund!(event(form({ dispute_id: "d1", refund_minor: "-1" }))),
    ).toMatchObject({ status: 400 });
    mockClient.POST.mockResolvedValue({ data: {} });
    expect(
      await disputesActions.resolveRefund!(
        event(form({ dispute_id: "d1", resolution: "ok", refund_minor: "50" })),
      ),
    ).toEqual({ ok: true });
    mockClient.POST.mockResolvedValue({ error: { detail: "x" } });
    expect(await disputesActions.resolveRefund!(event(form({ dispute_id: "d1" })))).toMatchObject({
      status: 500,
    });
  });
  it("resolveReject validates and posts", async () => {
    expect(await disputesActions.resolveReject!(event(form({})))).toMatchObject({ status: 400 });
    expect(await disputesActions.resolveReject!(event(form({ dispute_id: "d1" })))).toMatchObject({
      status: 400,
    });
    mockClient.POST.mockResolvedValue({ data: {} });
    expect(
      await disputesActions.resolveReject!(event(form({ dispute_id: "d1", resolution: "no" }))),
    ).toEqual({ ok: true });
    mockClient.POST.mockResolvedValue({ error: { detail: "x" } });
    expect(
      await disputesActions.resolveReject!(event(form({ dispute_id: "d1", resolution: "no" }))),
    ).toMatchObject({ status: 500 });
  });
});

describe("payroll new", () => {
  afterEach(() => vi.useRealTimers());
  it("load redirects and returns default month bounds", async () => {
    await expect(newLoad(loadEvent({ user: undefined }))).rejects.toMatchObject({ status: 303 });
    await expect(newLoad(loadEvent({ user: { role: "x" } }))).rejects.toMatchObject({
      status: 303,
      location: "/login",
    });
    vi.useFakeTimers();
    vi.setSystemTime(new Date(2026, 1, 15)); // Feb 2026
    const res = (await newLoad(loadEvent())) as Record<string, unknown>;
    expect(res.defaultStart).toBe("2026-02-01");
    expect(res.defaultEnd).toBe("2026-02-28");
  });
  it("default action validates both period fields", async () => {
    expect(await newActions.default!(event(form({ period_start: "2026-01-01" })))).toMatchObject({
      status: 400,
    });
  });
  it("default action posts and redirects to new batch", async () => {
    mockClient.POST.mockResolvedValue({ data: { batch: { id: "b9" } } });
    await expect(
      newActions.default!(event(form({ period_start: "2026-01-01", period_end: "2026-01-31" }))),
    ).rejects.toMatchObject({ status: 303, location: "/payroll/b9" });
    expect(mockClient.POST).toHaveBeenCalledWith("/api/admin/payroll/batches", {
      body: { period_start: "2026-01-01", period_end: "2026-01-31" },
    });
  });
  it("default action surfaces api error and missing id", async () => {
    mockClient.POST.mockResolvedValue({ error: { detail: "x" } });
    expect(
      await newActions.default!(
        event(form({ period_start: "2026-01-01", period_end: "2026-01-31" })),
      ),
    ).toMatchObject({ status: 500 });
    mockClient.POST.mockResolvedValue({ data: { batch: { id: "" } } });
    expect(
      await newActions.default!(
        event(form({ period_start: "2026-01-01", period_end: "2026-01-31" })),
      ),
    ).toMatchObject({ status: 500, data: { error: "no batch id in response" } });
  });
});
