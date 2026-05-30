import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";

const { mockClient } = vi.hoisted(() => ({
  mockClient: { GET: vi.fn(), POST: vi.fn(), PUT: vi.fn(), PATCH: vi.fn() },
}));
vi.mock("$lib/server/api", () => ({ apiFor: () => mockClient }));

import { load as plantsLoad, actions as plantsActions } from "./plants/+page.server";
import {
  load as settlementsLoad,
  actions as settlementsActions,
} from "./vendor-settlements/+page.server";
import { load as vendorsLoad, actions as vendorsActions } from "./vendors/+page.server";
import {
  load as vendorLoad,
  actions as vendorActions,
} from "./vendors/[id]/+page.server";
import {
  load as docsLoad,
  actions as docsActions,
} from "./vendors/[id]/documents/+page.server";

const ADMIN = { id: "u1", role: "welfare_admin" };

function form(entries: Record<string, string | string[]>): FormData {
  const fd = new FormData();
  for (const [k, v] of Object.entries(entries)) {
    if (Array.isArray(v)) for (const x of v) fd.append(k, x);
    else fd.append(k, v);
  }
  return fd;
}
function event(
  fd: FormData,
  opts: { params?: Record<string, string>; user?: unknown; url?: string } = {},
) {
  return {
    request: { formData: async () => fd },
    params: opts.params ?? { id: "v1" },
    locals: { user: "user" in opts ? opts.user : ADMIN, apiToken: "t" },
    url: new URL("http://x" + (opts.url ?? "/p")),
  } as never;
}
function loadEvent(
  opts: { user?: unknown; query?: string; params?: Record<string, string> } = {},
) {
  return {
    locals: { user: "user" in opts ? opts.user : ADMIN, apiToken: "t" },
    url: new URL("http://x/p" + (opts.query ?? "")),
    params: opts.params ?? { id: "v1" },
    depends: vi.fn(),
  } as never;
}

beforeEach(() => {
  for (const fn of Object.values(mockClient)) fn.mockReset();
});

describe("plants", () => {
  it("load redirects, lists, swallows errors", async () => {
    await expect(plantsLoad(loadEvent({ user: undefined }))).rejects.toMatchObject({ status: 303 });
    await expect(plantsLoad(loadEvent({ user: { role: "x" } }))).rejects.toMatchObject({
      status: 303,
      location: "/login",
    });
    mockClient.GET.mockResolvedValue({ data: { items: [{ code: "P1" }] } });
    expect(((await plantsLoad(loadEvent())) as { plants: unknown[] }).plants).toEqual([
      { code: "P1" },
    ]);
    mockClient.GET.mockRejectedValue(new Error("x"));
    expect(((await plantsLoad(loadEvent())) as { plants: unknown[] }).plants).toEqual([]);
    mockClient.GET.mockResolvedValue({ data: null });
    expect(((await plantsLoad(loadEvent())) as { plants: unknown[] }).plants).toEqual([]);
  });
  it("create validates, posts (with parsed sort), surfaces errors", async () => {
    expect(await plantsActions.create!(event(form({ code: "P1" })))).toMatchObject({ status: 400 });
    mockClient.POST.mockResolvedValue({ data: {} });
    expect(
      await plantsActions.create!(
        event(form({ code: " P1 ", label: " Main ", address: " St ", sort_order: "5" })),
      ),
    ).toEqual({ ok: true });
    expect(mockClient.POST).toHaveBeenCalledWith("/api/admin/plants", {
      body: { code: "P1", label: "Main", address: "St", sort_order: 5 },
    });
    // unparseable sort_order falls back to 0
    await plantsActions.create!(event(form({ code: "P2", label: "L", sort_order: "abc" })));
    expect(mockClient.POST).toHaveBeenLastCalledWith(
      "/api/admin/plants",
      expect.objectContaining({ body: expect.objectContaining({ sort_order: 0 }) }),
    );
    mockClient.POST.mockResolvedValue({ error: { detail: "x" } });
    expect(
      await plantsActions.create!(event(form({ code: "P1", label: "L" }))),
    ).toMatchObject({ status: 500 });
  });
  it("update validates, puts (active flag), surfaces errors", async () => {
    expect(await plantsActions.update!(event(form({ code: "P1" })))).toMatchObject({ status: 400 });
    mockClient.PUT.mockResolvedValue({ data: {} });
    expect(
      await plantsActions.update!(
        event(form({ code: "P1", label: "L", address: "A", active: "true", sort_order: "3" })),
      ),
    ).toEqual({ ok: true });
    expect(mockClient.PUT).toHaveBeenCalledWith("/api/admin/plants/{code}", {
      params: { path: { code: "P1" } },
      body: { label: "L", address: "A", active: true, sort_order: 3 },
    });
    await plantsActions.update!(event(form({ code: "P1", label: "L", active: "no" })));
    expect(mockClient.PUT).toHaveBeenLastCalledWith(
      "/api/admin/plants/{code}",
      expect.objectContaining({ body: expect.objectContaining({ active: false }) }),
    );
    mockClient.PUT.mockResolvedValue({ error: { detail: "x" } });
    expect(
      await plantsActions.update!(event(form({ code: "P1", label: "L" }))),
    ).toMatchObject({ status: 500 });
  });
});

describe("vendor-settlements", () => {
  afterEach(() => vi.useRealTimers());
  it("load redirects, defaults period to current month, lists, swallows errors", async () => {
    await expect(settlementsLoad(loadEvent({ user: undefined }))).rejects.toMatchObject({
      status: 303,
    });
    await expect(settlementsLoad(loadEvent({ user: { role: "x" } }))).rejects.toMatchObject({
      status: 303,
      location: "/login",
    });
    vi.useFakeTimers();
    vi.setSystemTime(new Date(2026, 2, 10)); // March 2026
    mockClient.GET.mockResolvedValue({ data: { items: [{ id: "s1" }] } });
    const res = (await settlementsLoad(loadEvent())) as Record<string, unknown>;
    expect(res.period).toBe("2026-03");
    expect(res.settlements).toEqual([{ id: "s1" }]);
    expect(mockClient.GET).toHaveBeenCalledWith("/api/admin/vendor-settlements", {
      params: { query: { period: "2026-03" } },
    });
    vi.useRealTimers();
    mockClient.GET.mockRejectedValue(new Error("x"));
    expect(
      ((await settlementsLoad(loadEvent({ query: "?period=2026-01" }))) as {
        settlements: unknown[];
      }).settlements,
    ).toEqual([]);
    mockClient.GET.mockResolvedValue({ data: null });
    expect(
      ((await settlementsLoad(loadEvent({ query: "?period=2026-01" }))) as {
        settlements: unknown[];
      }).settlements,
    ).toEqual([]);
  });
  it("close validates period format and month range, posts, redirects", async () => {
    expect(await settlementsActions.close!(event(form({ period: "bad" })))).toMatchObject({
      status: 400,
    });
    expect(await settlementsActions.close!(event(form({ period: "2026-13" })))).toMatchObject({
      status: 400,
    });
    expect(await settlementsActions.close!(event(form({ period: "2026-00" })))).toMatchObject({
      status: 400,
    });
    mockClient.POST.mockResolvedValue({ data: {} });
    await expect(
      settlementsActions.close!(event(form({ period: "2026-02" }))),
    ).rejects.toMatchObject({ status: 303, location: "/vendor-settlements?period=2026-02" });
    expect(mockClient.POST).toHaveBeenCalledWith("/api/admin/vendor-settlements/close", {
      body: { period_start: "2026-02-01", period_end: "2026-02-28" },
    });
    mockClient.POST.mockResolvedValue({ error: { detail: "x" } });
    expect(await settlementsActions.close!(event(form({ period: "2026-02" })))).toMatchObject({
      status: 500,
    });
  });
  it("voidSettlement validates id, voids, redirects (with/without period)", async () => {
    expect(await settlementsActions.voidSettlement!(event(form({})))).toMatchObject({
      status: 400,
    });
    mockClient.POST.mockResolvedValue({ data: {} });
    await expect(
      settlementsActions.voidSettlement!(
        event(form({ id: "s1" }), { url: "/p?period=2026-04" }),
      ),
    ).rejects.toMatchObject({ status: 303, location: "/vendor-settlements?period=2026-04" });
    await expect(
      settlementsActions.voidSettlement!(event(form({ id: "s1" }), { url: "/p" })),
    ).rejects.toMatchObject({ status: 303, location: "/vendor-settlements" });
    mockClient.POST.mockResolvedValue({ error: { detail: "x" } });
    expect(await settlementsActions.voidSettlement!(event(form({ id: "s1" })))).toMatchObject({
      status: 500,
    });
  });
});

describe("vendors list", () => {
  it("load redirects non-admin, lists with/without status, swallows errors", async () => {
    await expect(vendorsLoad(loadEvent({ user: undefined }))).rejects.toMatchObject({
      status: 303,
      location: "/login",
    });
    await expect(vendorsLoad(loadEvent({ user: { role: "x" } }))).rejects.toMatchObject({
      status: 303,
      location: "/login",
    });
    mockClient.GET.mockResolvedValue({ data: { items: [{ id: "v1" }] } });
    const res = (await vendorsLoad(loadEvent({ query: "?status=pending" }))) as Record<
      string,
      unknown
    >;
    expect(res.vendors).toEqual([{ id: "v1" }]);
    expect(mockClient.GET).toHaveBeenCalledWith("/api/admin/vendors", {
      params: { query: { status: "pending" } },
    });
    await vendorsLoad(loadEvent());
    expect(mockClient.GET).toHaveBeenLastCalledWith("/api/admin/vendors", {
      params: { query: {} },
    });
    mockClient.GET.mockRejectedValue(new Error("x"));
    expect(((await vendorsLoad(loadEvent())) as { vendors: unknown[] }).vendors).toEqual([]);
    mockClient.GET.mockResolvedValue({ data: null });
    expect(((await vendorsLoad(loadEvent())) as { vendors: unknown[] }).vendors).toEqual([]);
  });
  it("create validates, posts, redirects, surfaces errors", async () => {
    expect(
      await vendorsActions.create!(event(form({ display_name: "D" }))),
    ).toMatchObject({ status: 400 });
    mockClient.POST.mockResolvedValue({ data: {} });
    await expect(
      vendorsActions.create!(
        event(
          form({
            display_name: " D ",
            legal_name: " L ",
            contact_email: " A@B.COM ",
          }),
        ),
      ),
    ).rejects.toMatchObject({ status: 303, location: "/vendors" });
    expect(mockClient.POST).toHaveBeenCalledWith("/api/admin/vendors", {
      body: { display_name: "D", legal_name: "L", contact_email: "a@b.com" },
    });
    mockClient.POST.mockResolvedValue({ error: { detail: "x" } });
    expect(
      await vendorsActions.create!(
        event(form({ display_name: "D", legal_name: "L", contact_email: "a@b.com" })),
      ),
    ).toMatchObject({ status: 500 });
  });
});

describe("vendor detail", () => {
  it("load redirects non-admin", async () => {
    await expect(vendorLoad(loadEvent({ user: undefined }))).rejects.toMatchObject({
      status: 303,
      location: "/login",
    });
    await expect(vendorLoad(loadEvent({ user: { role: "x" } }))).rejects.toMatchObject({
      status: 303,
      location: "/login",
    });
  });
  it("throws 404 when vendor not found", async () => {
    mockClient.GET.mockResolvedValue({ data: { items: [{ id: "other" }] } });
    await expect(vendorLoad(loadEvent())).rejects.toMatchObject({ status: 404 });
  });
  it("returns vendor, operators, plants", async () => {
    mockClient.GET.mockImplementation((path: string) => {
      if (path === "/api/admin/vendors")
        return Promise.resolve({ data: { items: [{ id: "v1" }] } });
      if (path === "/api/admin/vendors/{id}/operators")
        return Promise.resolve({ data: { items: [{ id: "op1" }] } });
      return Promise.resolve({ data: { items: [{ code: "P1" }] } });
    });
    const res = (await vendorLoad(loadEvent())) as Record<string, unknown>;
    expect(res.vendor).toEqual({ id: "v1" });
    expect(res.operators).toEqual([{ id: "op1" }]);
    expect(res.knownPlants).toEqual([{ code: "P1" }]);
  });
  it("defaults operators/plants to [] when those calls reject", async () => {
    mockClient.GET.mockImplementation((path: string) => {
      if (path === "/api/admin/vendors")
        return Promise.resolve({ data: { items: [{ id: "v1" }] } });
      return Promise.reject(new Error("down"));
    });
    const res = (await vendorLoad(loadEvent())) as Record<string, unknown>;
    expect(res.operators).toEqual([]);
    expect(res.knownPlants).toEqual([]);
  });
  it("defaults vendors list to [] when that call rejects -> 404", async () => {
    mockClient.GET.mockRejectedValue(new Error("down"));
    await expect(vendorLoad(loadEvent())).rejects.toMatchObject({ status: 404 });
  });

  it("approve posts plants and redirects, surfaces errors", async () => {
    mockClient.POST.mockResolvedValue({ data: {} });
    await expect(
      vendorActions.approve!(event(form({ plants: ["A", "B"] }))),
    ).rejects.toMatchObject({ status: 303, location: "/vendors/v1" });
    expect(mockClient.POST).toHaveBeenCalledWith("/api/admin/vendors/{id}/approve", {
      params: { path: { id: "v1" } },
      body: { plants: ["A", "B"] },
    });
    mockClient.POST.mockResolvedValue({ error: { detail: "x" } });
    expect(await vendorActions.approve!(event(form({})))).toMatchObject({ status: 500 });
  });
  it("update validates email, patches, surfaces errors", async () => {
    expect(await vendorActions.update!(event(form({})))).toMatchObject({ status: 400 });
    mockClient.PATCH.mockResolvedValue({ data: {} });
    await expect(
      vendorActions.update!(event(form({ contact_email: " a@b.com ", plants: ["A"] }))),
    ).rejects.toMatchObject({ status: 303, location: "/vendors/v1" });
    expect(mockClient.PATCH).toHaveBeenCalledWith("/api/admin/vendors/{id}", {
      params: { path: { id: "v1" } },
      body: { contact_email: "a@b.com", plants: ["A"] },
    });
    mockClient.PATCH.mockResolvedValue({ error: { detail: "x" } });
    expect(
      await vendorActions.update!(event(form({ contact_email: "a@b.com" }))),
    ).toMatchObject({ status: 500 });
  });
  it("setPlantWindow validates plant, puts, surfaces errors", async () => {
    expect(await vendorActions.setPlantWindow!(event(form({})))).toMatchObject({ status: 400 });
    mockClient.PUT.mockResolvedValue({ data: {} });
    await expect(
      vendorActions.setPlantWindow!(
        event(form({ plant: "A", service_window: " 11:00-13:00 " })),
      ),
    ).rejects.toMatchObject({ status: 303, location: "/vendors/v1" });
    expect(mockClient.PUT).toHaveBeenCalledWith("/api/admin/vendors/{id}/plants/{plant}/window", {
      params: { path: { id: "v1", plant: "A" } },
      body: { service_window: "11:00-13:00" },
    });
    mockClient.PUT.mockResolvedValue({ error: { detail: "x" } });
    expect(
      await vendorActions.setPlantWindow!(event(form({ plant: "A" }))),
    ).toMatchObject({ status: 500 });
  });
  it("suspend redirects and surfaces errors", async () => {
    mockClient.POST.mockResolvedValue({ data: {} });
    await expect(vendorActions.suspend!(event(form({})))).rejects.toMatchObject({
      status: 303,
      location: "/vendors/v1",
    });
    mockClient.POST.mockResolvedValue({ error: { detail: "x" } });
    expect(await vendorActions.suspend!(event(form({})))).toMatchObject({ status: 500 });
  });
  it("reinstate redirects and surfaces errors", async () => {
    mockClient.POST.mockResolvedValue({ data: {} });
    await expect(vendorActions.reinstate!(event(form({})))).rejects.toMatchObject({
      status: 303,
      location: "/vendors/v1",
    });
    mockClient.POST.mockResolvedValue({ error: { detail: "x" } });
    expect(await vendorActions.reinstate!(event(form({})))).toMatchObject({ status: 500 });
  });
  it("createOperator validates, returns setupUrl, surfaces errors", async () => {
    expect(await vendorActions.createOperator!(event(form({ email: "a@b.com" })))).toMatchObject({
      status: 400,
    });
    mockClient.POST.mockResolvedValue({ data: { operator: { setup_url: "http://setup" } } });
    const res = await vendorActions.createOperator!(
      event(form({ email: " A@B.COM ", display_name: " Bob " })),
    );
    expect(res).toEqual({ setupUrl: "http://setup" });
    expect(mockClient.POST).toHaveBeenCalledWith("/api/admin/vendors/{id}/operators", {
      params: { path: { id: "v1" } },
      body: { email: "a@b.com", display_name: "Bob" },
    });
    mockClient.POST.mockResolvedValue({ error: { detail: "x" } });
    expect(
      await vendorActions.createOperator!(
        event(form({ email: "a@b.com", display_name: "Bob" })),
      ),
    ).toMatchObject({ status: 500 });
  });
  it("suspendOperator posts and surfaces errors", async () => {
    mockClient.POST.mockResolvedValue({ data: {} });
    await expect(
      vendorActions.suspendOperator!(event(form({ operator_id: "op1" }))),
    ).rejects.toMatchObject({ status: 303, location: "/vendors/v1" });
    expect(mockClient.POST).toHaveBeenCalledWith(
      "/api/admin/vendors/{id}/operators/{operator_id}/suspend",
      { params: { path: { id: "v1", operator_id: "op1" } } },
    );
    mockClient.POST.mockResolvedValue({ error: { detail: "x" } });
    expect(
      await vendorActions.suspendOperator!(event(form({ operator_id: "op1" }))),
    ).toMatchObject({ status: 500 });
  });
  it("reinstateOperator posts and surfaces errors", async () => {
    mockClient.POST.mockResolvedValue({ data: {} });
    await expect(
      vendorActions.reinstateOperator!(event(form({ operator_id: "op1" }))),
    ).rejects.toMatchObject({ status: 303, location: "/vendors/v1" });
    expect(mockClient.POST).toHaveBeenCalledWith(
      "/api/admin/vendors/{id}/operators/{operator_id}/reinstate",
      { params: { path: { id: "v1", operator_id: "op1" } } },
    );
    mockClient.POST.mockResolvedValue({ error: { detail: "x" } });
    expect(
      await vendorActions.reinstateOperator!(event(form({ operator_id: "op1" }))),
    ).toMatchObject({ status: 500 });
  });
});

describe("vendor documents", () => {
  it("load redirects anonymous and non-admin", async () => {
    await expect(docsLoad(loadEvent({ user: undefined }))).rejects.toMatchObject({ status: 303 });
    await expect(docsLoad(loadEvent({ user: { role: "x" } }))).rejects.toMatchObject({
      status: 303,
      location: "/login",
    });
  });
  it("throws 404 when vendor not found (also covers empty data)", async () => {
    mockClient.GET.mockResolvedValue({ data: { items: [{ id: "other" }] } });
    await expect(docsLoad(loadEvent())).rejects.toMatchObject({ status: 404 });
    mockClient.GET.mockResolvedValue({ data: null });
    await expect(docsLoad(loadEvent())).rejects.toMatchObject({ status: 404 });
  });
  it("returns vendor and documents, swallows doc errors", async () => {
    mockClient.GET.mockImplementation((path: string) => {
      if (path === "/api/admin/vendors")
        return Promise.resolve({ data: { items: [{ id: "v1" }] } });
      return Promise.resolve({ data: { items: [{ id: "doc1" }] } });
    });
    const res = (await docsLoad(loadEvent())) as Record<string, unknown>;
    expect(res.vendor).toEqual({ id: "v1" });
    expect(res.documents).toEqual([{ id: "doc1" }]);

    mockClient.GET.mockImplementation((path: string) => {
      if (path === "/api/admin/vendors")
        return Promise.resolve({ data: { items: [{ id: "v1" }] } });
      return Promise.reject(new Error("down"));
    });
    expect(((await docsLoad(loadEvent())) as { documents: unknown[] }).documents).toEqual([]);

    mockClient.GET.mockImplementation((path: string) => {
      if (path === "/api/admin/vendors")
        return Promise.resolve({ data: { items: [{ id: "v1" }] } });
      return Promise.resolve({ data: null });
    });
    expect(((await docsLoad(loadEvent())) as { documents: unknown[] }).documents).toEqual([]);
  });
  it("upload validates, posts (with expires_at), redirects", async () => {
    expect(await docsActions.upload!(event(form({ filename: "f" })))).toMatchObject({
      status: 400,
    });
    mockClient.POST.mockResolvedValue({ data: {} });
    await expect(
      docsActions.upload!(
        event(
          form({
            filename: " f.pdf ",
            kind: "insurance",
            expires_at: " 2026-12-31 ",
            content_base64: "abc",
          }),
        ),
      ),
    ).rejects.toMatchObject({ status: 303, location: "/vendors/v1/documents" });
    expect(mockClient.POST).toHaveBeenCalledWith("/api/admin/vendors/{vendor_id}/documents", {
      params: { path: { vendor_id: "v1" } },
      body: {
        filename: "f.pdf",
        kind: "insurance",
        content_base64: "abc",
        expires_at: "2026-12-31",
      },
    });
  });
  it("upload omits expires_at when blank and surfaces errors", async () => {
    mockClient.POST.mockResolvedValue({ data: {} });
    await expect(
      docsActions.upload!(
        event(form({ filename: "f", kind: "insurance", content_base64: "abc" })),
      ),
    ).rejects.toMatchObject({ status: 303 });
    expect(mockClient.POST).toHaveBeenLastCalledWith(
      "/api/admin/vendors/{vendor_id}/documents",
      expect.objectContaining({
        body: { filename: "f", kind: "insurance", content_base64: "abc" },
      }),
    );
    mockClient.POST.mockResolvedValue({ error: { detail: "x" } });
    expect(
      await docsActions.upload!(
        event(form({ filename: "f", kind: "insurance", content_base64: "abc" })),
      ),
    ).toMatchObject({ status: 500 });
  });
  it("review validates input, posts approved/rejected, surfaces errors", async () => {
    expect(await docsActions.review!(event(form({ id: "d1", status: "bogus" })))).toMatchObject({
      status: 400,
    });
    expect(await docsActions.review!(event(form({ status: "approved" })))).toMatchObject({
      status: 400,
    });
    mockClient.POST.mockResolvedValue({ data: {} });
    expect(
      await docsActions.review!(event(form({ id: "d1", status: "approved", notes: "ok" }))),
    ).toEqual({ ok: true });
    expect(mockClient.POST).toHaveBeenCalledWith("/api/admin/documents/{id}/review", {
      params: { path: { id: "d1" } },
      body: { status: "approved", notes: "ok" },
    });
    expect(
      await docsActions.review!(event(form({ id: "d1", status: "rejected" }))),
    ).toEqual({ ok: true });
    mockClient.POST.mockResolvedValue({ error: { detail: "x" } });
    expect(
      await docsActions.review!(event(form({ id: "d1", status: "approved" }))),
    ).toMatchObject({ status: 500 });
  });
});
