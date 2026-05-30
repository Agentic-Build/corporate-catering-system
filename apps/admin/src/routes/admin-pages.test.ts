import { describe, it, expect, vi, beforeEach } from "vitest";

const { mockClient } = vi.hoisted(() => ({
  mockClient: { GET: vi.fn(), POST: vi.fn(), PUT: vi.fn(), PATCH: vi.fn() },
}));
vi.mock("$lib/server/api", () => ({ apiFor: () => mockClient }));

import { load as anomaliesLoad, actions as anomaliesActions } from "./anomalies/+page.server";
import { load as auditLoad } from "./audit/+page.server";
import { load as complaintsLoad, actions as complaintsActions } from "./complaints/+page.server";
import { load as disputesLoad, actions as disputesActions } from "./disputes/+page.server";
import { load as dlqLoad, actions as dlqActions } from "./dlq/+page.server";

const ADMIN = { id: "u1", role: "welfare_admin" };

function form(entries: Record<string, string>): FormData {
  const fd = new FormData();
  for (const [k, v] of Object.entries(entries)) fd.append(k, v);
  return fd;
}
function event(fd: FormData, user: unknown = ADMIN) {
  return { request: { formData: async () => fd }, locals: { user, apiToken: "t" } } as never;
}
function loadEvent(opts: { user?: unknown; path?: string; query?: string } = {}) {
  return {
    locals: { user: "user" in opts ? opts.user : ADMIN, apiToken: "t" },
    url: new URL("http://x" + (opts.path ?? "/p") + (opts.query ?? "")),
    depends: vi.fn(),
  } as never;
}

beforeEach(() => {
  for (const fn of Object.values(mockClient)) fn.mockReset();
});

describe("anomalies load", () => {
  it("redirects anonymous and non-admin", async () => {
    await expect(anomaliesLoad(loadEvent({ user: undefined }))).rejects.toMatchObject({
      status: 303,
    });
    await expect(
      anomaliesLoad(loadEvent({ user: { role: "x" } })),
    ).rejects.toMatchObject({ status: 303, location: "/login" });
  });
  it("uses default status=open with no filters and lists items", async () => {
    mockClient.GET.mockResolvedValue({ data: { items: [{ id: "a1" }] } });
    const res = (await anomaliesLoad(loadEvent())) as Record<string, unknown>;
    expect(res.status).toBe("open");
    expect(res.severity).toBe("");
    expect(res.anomalies).toEqual([{ id: "a1" }]);
    expect(mockClient.GET).toHaveBeenCalledWith("/api/admin/anomalies", {
      params: { query: { status: "open" } },
    });
  });
  it("applies status and severity filters", async () => {
    mockClient.GET.mockResolvedValue({ data: { items: [] } });
    await anomaliesLoad(loadEvent({ query: "?status=closed&severity=high" }));
    expect(mockClient.GET).toHaveBeenCalledWith("/api/admin/anomalies", {
      params: { query: { status: "closed", severity: "high" } },
    });
  });
  it("treats empty status string as no filter", async () => {
    mockClient.GET.mockResolvedValue({ data: { items: [] } });
    await anomaliesLoad(loadEvent({ query: "?status=" }));
    expect(mockClient.GET).toHaveBeenCalledWith("/api/admin/anomalies", {
      params: { query: {} },
    });
  });
  it("swallows GET errors and missing data", async () => {
    mockClient.GET.mockRejectedValue(new Error("down"));
    const res = (await anomaliesLoad(loadEvent())) as { anomalies: unknown[] };
    expect(res.anomalies).toEqual([]);
    mockClient.GET.mockResolvedValue({ data: null });
    const res2 = (await anomaliesLoad(loadEvent())) as { anomalies: unknown[] };
    expect(res2.anomalies).toEqual([]);
  });
});

describe("anomalies actions", () => {
  it("triage fails without id", async () => {
    expect(await anomaliesActions.triage!(event(form({})))).toMatchObject({ status: 400 });
  });
  it("triage with warn action", async () => {
    mockClient.POST.mockResolvedValue({ data: {} });
    const res = await anomaliesActions.triage!(
      event(form({ id: "a1", notes: "n", action: "warn" })),
    );
    expect(res).toEqual({ ok: true });
    expect(mockClient.POST).toHaveBeenCalledWith("/api/admin/anomalies/{id}/triage", {
      params: { path: { id: "a1" } },
      body: { notes: "n", action: "warn" },
    });
  });
  it("triage with suspend action", async () => {
    mockClient.POST.mockResolvedValue({ data: {} });
    await anomaliesActions.triage!(event(form({ id: "a1", action: "suspend" })));
    expect(mockClient.POST).toHaveBeenCalledWith(
      "/api/admin/anomalies/{id}/triage",
      expect.objectContaining({ body: { notes: "", action: "suspend" } }),
    );
  });
  it("triage ignores unknown action and surfaces errors", async () => {
    mockClient.POST.mockResolvedValue({ data: {} });
    await anomaliesActions.triage!(event(form({ id: "a1", action: "other" })));
    expect(mockClient.POST).toHaveBeenCalledWith(
      "/api/admin/anomalies/{id}/triage",
      expect.objectContaining({ body: { notes: "" } }),
    );
    mockClient.POST.mockResolvedValue({ error: { detail: "x" } });
    expect(await anomaliesActions.triage!(event(form({ id: "a1" })))).toMatchObject({
      status: 500,
    });
  });
  it("close fails without id, succeeds, and surfaces errors", async () => {
    expect(await anomaliesActions.close!(event(form({})))).toMatchObject({ status: 400 });
    mockClient.POST.mockResolvedValue({ data: {} });
    expect(await anomaliesActions.close!(event(form({ id: "a1", notes: "n" })))).toEqual({
      ok: true,
    });
    mockClient.POST.mockResolvedValue({ error: { detail: "x" } });
    expect(await anomaliesActions.close!(event(form({ id: "a1" })))).toMatchObject({
      status: 500,
    });
  });
});

describe("audit load", () => {
  it("redirects anonymous and non-admin", async () => {
    await expect(auditLoad(loadEvent({ user: undefined }))).rejects.toMatchObject({ status: 303 });
    await expect(auditLoad(loadEvent({ user: { role: "x" } }))).rejects.toMatchObject({
      status: 303,
      location: "/login",
    });
  });
  it("defaults limit to 100 with no filters", async () => {
    mockClient.GET.mockResolvedValue({ data: { items: [{ id: "e1" }] } });
    const res = (await auditLoad(loadEvent())) as Record<string, unknown>;
    expect(res.limit).toBe(100);
    expect(res.events).toEqual([{ id: "e1" }]);
    expect(mockClient.GET).toHaveBeenCalledWith("/api/admin/audit", {
      params: { query: { limit: 100 } },
    });
  });
  it("applies target_kind/target_id/since/limit filters", async () => {
    mockClient.GET.mockResolvedValue({ data: { items: [] } });
    await auditLoad(
      loadEvent({ query: "?target_kind=vendor&target_id=v1&since=2026-01-01&limit=5" }),
    );
    expect(mockClient.GET).toHaveBeenCalledWith("/api/admin/audit", {
      params: { query: { limit: 5, target_kind: "vendor", target_id: "v1", since: "2026-01-01" } },
    });
  });
  it("swallows errors and missing data", async () => {
    mockClient.GET.mockRejectedValue(new Error("x"));
    expect(((await auditLoad(loadEvent())) as { events: unknown[] }).events).toEqual([]);
    mockClient.GET.mockResolvedValue({ data: null });
    expect(((await auditLoad(loadEvent())) as { events: unknown[] }).events).toEqual([]);
  });
});

describe("complaints", () => {
  it("load redirects, lists, swallows errors", async () => {
    await expect(complaintsLoad(loadEvent({ user: undefined }))).rejects.toMatchObject({
      status: 303,
    });
    await expect(complaintsLoad(loadEvent({ user: { role: "x" } }))).rejects.toMatchObject({
      status: 303,
      location: "/login",
    });
    mockClient.GET.mockResolvedValue({ data: { items: [{ id: "c1" }] } });
    expect(((await complaintsLoad(loadEvent())) as { complaints: unknown[] }).complaints).toEqual([
      { id: "c1" },
    ]);
    mockClient.GET.mockRejectedValue(new Error("x"));
    expect(((await complaintsLoad(loadEvent())) as { complaints: unknown[] }).complaints).toEqual(
      [],
    );
    mockClient.GET.mockResolvedValue({ data: null });
    expect(((await complaintsLoad(loadEvent())) as { complaints: unknown[] }).complaints).toEqual(
      [],
    );
  });
  it("resolve validates id and resolution length", async () => {
    expect(await complaintsActions.resolve!(event(form({})))).toMatchObject({
      status: 400,
      data: { error: "缺少客訴編號" },
    });
    expect(
      await complaintsActions.resolve!(event(form({ id: "c1", resolution: "ab" }))),
    ).toMatchObject({ status: 400, data: { error: "結案說明至少需 5 個字" } });
  });
  it("resolve posts with compensate flag and returns id", async () => {
    mockClient.POST.mockResolvedValue({ data: {} });
    const res = await complaintsActions.resolve!(
      event(form({ id: "c1", resolution: "all good now", compensate: "true" })),
    );
    expect(res).toEqual({ ok: true, resolved: "c1" });
    expect(mockClient.POST).toHaveBeenCalledWith("/api/admin/complaints/{id}/resolve", {
      params: { path: { id: "c1" } },
      body: { resolution: "all good now", compensate: true },
    });
  });
  it("resolve surfaces api errors and non-true compensate", async () => {
    mockClient.POST.mockResolvedValue({ error: { detail: "x" } });
    const res = await complaintsActions.resolve!(
      event(form({ id: "c1", resolution: "long enough", compensate: "no" })),
    );
    expect(res).toMatchObject({ status: 500 });
    expect(mockClient.POST).toHaveBeenCalledWith(
      "/api/admin/complaints/{id}/resolve",
      expect.objectContaining({ body: { resolution: "long enough", compensate: false } }),
    );
  });
});

describe("disputes", () => {
  it("load redirects, applies status filter, swallows errors", async () => {
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
  it("resolveRefund validates dispute_id and refund_minor", async () => {
    expect(await disputesActions.resolveRefund!(event(form({})))).toMatchObject({ status: 400 });
    expect(
      await disputesActions.resolveRefund!(event(form({ dispute_id: "d1", refund_minor: "-5" }))),
    ).toMatchObject({ status: 400, data: { error: "refund_minor must be >= 0" } });
    expect(
      await disputesActions.resolveRefund!(event(form({ dispute_id: "d1", refund_minor: "abc" }))),
    ).toMatchObject({ status: 400 });
  });
  it("resolveRefund posts and surfaces errors", async () => {
    mockClient.POST.mockResolvedValue({ data: {} });
    expect(
      await disputesActions.resolveRefund!(
        event(form({ dispute_id: "d1", resolution: " ok ", refund_minor: "120" })),
      ),
    ).toEqual({ ok: true });
    expect(mockClient.POST).toHaveBeenCalledWith("/api/admin/payroll/disputes/{id}/resolve", {
      params: { path: { id: "d1" } },
      body: { status: "resolved_refund", resolution: "ok", refund_minor: 120 },
    });
    mockClient.POST.mockResolvedValue({ error: { detail: "x" } });
    expect(
      await disputesActions.resolveRefund!(event(form({ dispute_id: "d1" }))),
    ).toMatchObject({ status: 500 });
  });
  it("resolveReject validates and posts", async () => {
    expect(await disputesActions.resolveReject!(event(form({})))).toMatchObject({ status: 400 });
    expect(
      await disputesActions.resolveReject!(event(form({ dispute_id: "d1", resolution: "" }))),
    ).toMatchObject({ status: 400, data: { error: "resolution required" } });
    mockClient.POST.mockResolvedValue({ data: {} });
    expect(
      await disputesActions.resolveReject!(event(form({ dispute_id: "d1", resolution: "no" }))),
    ).toEqual({ ok: true });
    expect(mockClient.POST).toHaveBeenCalledWith("/api/admin/payroll/disputes/{id}/resolve", {
      params: { path: { id: "d1" } },
      body: { status: "resolved_reject", resolution: "no", refund_minor: 0 },
    });
    mockClient.POST.mockResolvedValue({ error: { detail: "x" } });
    expect(
      await disputesActions.resolveReject!(event(form({ dispute_id: "d1", resolution: "no" }))),
    ).toMatchObject({ status: 500 });
  });
});

describe("dlq", () => {
  it("load redirects, applies stream filter, swallows errors", async () => {
    await expect(dlqLoad(loadEvent({ user: undefined }))).rejects.toMatchObject({ status: 303 });
    await expect(dlqLoad(loadEvent({ user: { role: "x" } }))).rejects.toMatchObject({
      status: 303,
      location: "/login",
    });
    mockClient.GET.mockResolvedValue({ data: { items: [{ id: "m1" }] } });
    const res = (await dlqLoad(loadEvent({ query: "?stream=orders" }))) as Record<string, unknown>;
    expect(res.messages).toEqual([{ id: "m1" }]);
    expect(mockClient.GET).toHaveBeenCalledWith("/api/admin/dlq", {
      params: { query: { limit: 200, stream: "orders" } },
    });
    await dlqLoad(loadEvent());
    expect(mockClient.GET).toHaveBeenLastCalledWith("/api/admin/dlq", {
      params: { query: { limit: 200 } },
    });
    mockClient.GET.mockRejectedValue(new Error("x"));
    expect(((await dlqLoad(loadEvent())) as { messages: unknown[] }).messages).toEqual([]);
    mockClient.GET.mockResolvedValue({ data: null });
    expect(((await dlqLoad(loadEvent())) as { messages: unknown[] }).messages).toEqual([]);
  });
  it("replay validates id, succeeds, surfaces errors", async () => {
    expect(await dlqActions.replay!(event(form({})))).toMatchObject({ status: 400 });
    mockClient.POST.mockResolvedValue({ data: {} });
    expect(await dlqActions.replay!(event(form({ id: "m1" })))).toEqual({ ok: true });
    expect(mockClient.POST).toHaveBeenCalledWith("/api/admin/dlq/{id}/replay", {
      params: { path: { id: "m1" } },
    });
    mockClient.POST.mockResolvedValue({ error: { detail: "x" } });
    expect(await dlqActions.replay!(event(form({ id: "m1" })))).toMatchObject({ status: 500 });
  });
  it("resolve validates id, succeeds, surfaces errors", async () => {
    expect(await dlqActions.resolve!(event(form({})))).toMatchObject({ status: 400 });
    mockClient.POST.mockResolvedValue({ data: {} });
    expect(await dlqActions.resolve!(event(form({ id: "m1", notes: "n" })))).toEqual({ ok: true });
    expect(mockClient.POST).toHaveBeenCalledWith("/api/admin/dlq/{id}/resolve", {
      params: { path: { id: "m1" } },
      body: { notes: "n" },
    });
    mockClient.POST.mockResolvedValue({ error: { detail: "x" } });
    expect(await dlqActions.resolve!(event(form({ id: "m1" })))).toMatchObject({ status: 500 });
  });
});
