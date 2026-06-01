import { describe, it, expect, vi, beforeEach } from "vitest";

const { mockClient } = vi.hoisted(() => ({ mockClient: { GET: vi.fn() } }));
vi.mock("$lib/server/api", () => ({ apiFor: () => mockClient }));

import { load } from "./+page.server";

const VENDOR = { id: "u1", role: "vendor_operator" };

function loadEvent(id = "s1", user: unknown = VENDOR) {
  return {
    locals: { user, apiToken: "t" },
    params: { id },
    url: new URL("http://x/reconciliation/" + id),
  } as never;
}

beforeEach(() => {
  mockClient.GET.mockReset();
});

describe("reconciliation/[id] load", () => {
  it("redirects unauthenticated", async () => {
    await expect(
      load({
        locals: {},
        params: { id: "s1" },
        url: new URL("http://x/reconciliation/s1"),
      } as never),
    ).rejects.toMatchObject({ status: 303 });
  });

  it("redirects non-vendor", async () => {
    await expect(load(loadEvent("s1", { role: "employee" }))).rejects.toMatchObject({
      status: 303,
      location: "/login",
    });
  });

  it("returns settlement + orders", async () => {
    mockClient.GET.mockResolvedValue({
      data: { settlement: { id: "s1" }, orders: [{ id: "o1" }] },
    });
    const res = (await load(loadEvent())) as { settlement: unknown; orders: unknown[] };
    expect(res.settlement).toEqual({ id: "s1" });
    expect(res.orders).toHaveLength(1);
  });

  it("defaults settlement null / orders [] when data fields missing", async () => {
    mockClient.GET.mockResolvedValue({ data: {} });
    const res = (await load(loadEvent())) as { settlement: unknown; orders: unknown[] };
    expect(res.settlement).toBeNull();
    expect(res.orders).toEqual([]);
  });

  it("404s when API returns an error", async () => {
    mockClient.GET.mockResolvedValue({ error: { status: 404 } });
    await expect(load(loadEvent())).rejects.toMatchObject({ status: 404 });
  });

  it("404s when API returns no data", async () => {
    mockClient.GET.mockResolvedValue({ data: null });
    await expect(load(loadEvent())).rejects.toMatchObject({ status: 404 });
  });

  it("404s when GET throws a non-http error", async () => {
    mockClient.GET.mockRejectedValue(new Error("boom"));
    await expect(load(loadEvent())).rejects.toMatchObject({ status: 404 });
  });
});
