import { describe, it, expect, vi, beforeEach } from "vitest";

const { mockClient } = vi.hoisted(() => ({
  mockClient: { GET: vi.fn(), POST: vi.fn(), PUT: vi.fn(), PATCH: vi.fn(), DELETE: vi.fn() },
}));
vi.mock("$lib/server/api", () => ({ apiFor: () => mockClient }));

import { load as rootLoad, actions as rootActions } from "./+page.server";
import { load as layoutLoad } from "./+layout.server";

const ADMIN = { id: "u1", role: "welfare_admin" };

function form(entries: Record<string, string | string[]>): FormData {
  const fd = new FormData();
  for (const [k, v] of Object.entries(entries)) {
    if (Array.isArray(v)) for (const x of v) fd.append(k, x);
    else fd.append(k, v);
  }
  return fd;
}
function event(fd: FormData, user: unknown = ADMIN) {
  return { request: { formData: async () => fd }, locals: { user, apiToken: "t" } } as never;
}
function loadEvent(opts: { user?: unknown; path?: string } = {}) {
  return {
    locals: { user: "user" in opts ? opts.user : ADMIN, apiToken: "t" },
    url: new URL("http://x" + (opts.path ?? "/")),
    depends: vi.fn(),
  } as never;
}

beforeEach(() => {
  for (const fn of Object.values(mockClient)) fn.mockReset();
});

describe("+layout.server load", () => {
  it("returns the user from locals", () => {
    expect(layoutLoad({ locals: { user: ADMIN } } as never)).toEqual({ user: ADMIN });
  });
});

describe("root load guards", () => {
  it("redirects anonymous users to login with return_to", async () => {
    await expect(rootLoad(loadEvent({ user: undefined, path: "/dash" }))).rejects.toMatchObject({
      status: 303,
      location: "/login?return_to=%2Fdash",
    });
  });
  it("redirects non-admin users to login", async () => {
    await expect(
      rootLoad(loadEvent({ user: { id: "x", role: "vendor_operator" } })),
    ).rejects.toMatchObject({ status: 303, location: "/login" });
  });
});

describe("root load", () => {
  it("aggregates dashboard data, batch detail, counts and payroll totals", async () => {
    mockClient.GET.mockImplementation((path: string) => {
      if (path === "/api/admin/vendors")
        return Promise.resolve({
          data: {
            items: [{ status: "pending" }, { status: "approved" }, { status: "approved" }],
          },
        });
      if (path === "/api/admin/anomalies") {
        const now = new Date().toISOString();
        const old = new Date(Date.now() - 30 * 24 * 60 * 60 * 1000).toISOString();
        return Promise.resolve({
          data: {
            items: [
              { created_at: now, severity: "critical", status: "open" },
              { created_at: now, severity: "low", status: "open" },
              { created_at: now, severity: "high", status: "closed" },
              { created_at: old, severity: "high", status: "open" },
              { created_at: "not-a-date", severity: "high", status: "open" },
            ],
          },
        });
      }
      if (path === "/api/admin/payroll/batches")
        return Promise.resolve({
          data: {
            items: [
              { id: "b1", period_start: "2026-01-01" },
              { id: "b2", period_start: "2026-03-01" },
            ],
          },
        });
      if (path === "/api/admin/plants")
        return Promise.resolve({ data: { items: [{ code: "P1" }] } });
      if (path === "/api/admin/payroll/batches/{id}")
        return Promise.resolve({
          data: {
            batch: { id: "b2", period_start: "2026-03-01" },
            entries: [{ amount_minor: 100, refunded_minor: 10 }, { amount_minor: 200 }],
          },
        });
      return Promise.resolve({ data: { items: [] } });
    });

    const res = (await rootLoad(loadEvent())) as Record<string, unknown>;
    expect(res.knownPlants).toEqual([{ code: "P1" }]);
    expect(res.counts).toEqual({
      pending: 1,
      approved: 2,
      anomalies7d: 3,
      anomaliesSevere: 2,
    });
    // newest batch (b2) selected for detail
    expect(mockClient.GET).toHaveBeenCalledWith("/api/admin/payroll/batches/{id}", {
      params: { path: { id: "b2" } },
    });
    expect((res.payroll as { total: number }).total).toBe(300);
    expect((res.payroll as { refunded: number }).refunded).toBe(10);
    expect((res.payroll as { entries: unknown[] }).entries).toHaveLength(2);
    // open & non-closed anomalies only (closed dropped), old & invalid dates dropped
    expect((res.anomalies as unknown[]).length).toBe(2);
  });

  it("handles rejected list calls and missing batch detail data", async () => {
    mockClient.GET.mockImplementation((path: string) => {
      if (path === "/api/admin/vendors") return Promise.reject(new Error("boom"));
      if (path === "/api/admin/anomalies") return Promise.reject(new Error("boom"));
      if (path === "/api/admin/payroll/batches")
        return Promise.resolve({ data: { items: [{ id: "b1", period_start: "2026-02-01" }] } });
      if (path === "/api/admin/plants") return Promise.reject(new Error("boom"));
      if (path === "/api/admin/payroll/batches/{id}") return Promise.resolve({ data: null });
      return Promise.resolve({ data: { items: [] } });
    });
    const res = (await rootLoad(loadEvent())) as Record<string, unknown>;
    expect(res.knownPlants).toEqual([]);
    expect((res.payroll as { batch: unknown }).batch).toBeNull();
    expect((res.payroll as { entries: unknown[] }).entries).toEqual([]);
    expect(res.counts).toMatchObject({ pending: 0, approved: 0 });
  });

  it("falls back to latestBatch when batch detail GET throws", async () => {
    mockClient.GET.mockImplementation((path: string) => {
      if (path === "/api/admin/payroll/batches")
        return Promise.resolve({ data: { items: [{ id: "b9", period_start: "2026-04-01" }] } });
      if (path === "/api/admin/payroll/batches/{id}")
        return Promise.reject(new Error("detail down"));
      return Promise.resolve({ data: { items: [] } });
    });
    const res = (await rootLoad(loadEvent())) as Record<string, unknown>;
    expect((res.payroll as { batch: { id: string } }).batch.id).toBe("b9");
    expect((res.payroll as { entries: unknown[] }).entries).toEqual([]);
  });

  it("uses latestBatch when detail returns data without batch field", async () => {
    mockClient.GET.mockImplementation((path: string) => {
      if (path === "/api/admin/payroll/batches")
        return Promise.resolve({ data: { items: [{ id: "b3", period_start: "2026-05-01" }] } });
      if (path === "/api/admin/payroll/batches/{id}")
        return Promise.resolve({ data: { entries: undefined } });
      return Promise.resolve({ data: { items: [] } });
    });
    const res = (await rootLoad(loadEvent())) as Record<string, unknown>;
    expect((res.payroll as { batch: { id: string } }).batch.id).toBe("b3");
    expect((res.payroll as { entries: unknown[] }).entries).toEqual([]);
  });

  it("handles empty data fields (nullish coalescing to [])", async () => {
    mockClient.GET.mockResolvedValue({ data: {} });
    const res = (await rootLoad(loadEvent())) as Record<string, unknown>;
    expect((res.payroll as { batch: unknown }).batch).toBeNull();
    expect(res.counts).toMatchObject({ anomalies7d: 0 });
  });
});

describe("root actions.approveVendor", () => {
  it("fails without an id", async () => {
    const res = await rootActions.approveVendor!(event(form({})));
    expect(res).toMatchObject({ status: 400, data: { error: "vendor id required" } });
  });
  it("approves with selected plants", async () => {
    mockClient.POST.mockResolvedValue({ data: {} });
    const res = await rootActions.approveVendor!(event(form({ id: "v1", plants: ["A", "B"] })));
    expect(res).toEqual({ ok: true, approved: "v1" });
    expect(mockClient.POST).toHaveBeenCalledWith("/api/admin/vendors/{id}/approve", {
      params: { path: { id: "v1" } },
      body: { plants: ["A", "B"] },
    });
  });
  it("returns 500 on api error", async () => {
    mockClient.POST.mockResolvedValue({ error: { detail: "nope" } });
    const res = await rootActions.approveVendor!(event(form({ id: "v1" })));
    expect(res).toMatchObject({ status: 500 });
  });
});
