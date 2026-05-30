import { describe, it, expect, vi, beforeEach } from "vitest";

const { mockClient } = vi.hoisted(() => ({ mockClient: { GET: vi.fn() } }));
vi.mock("$lib/server/api", () => ({ apiFor: () => mockClient }));

import { load } from "./+page.server";

const VENDOR = { id: "u1", role: "vendor_operator" };

function loadEvent(search = "", user: unknown = VENDOR) {
  return {
    locals: { user, apiToken: "t" },
    url: new URL("http://x/reconciliation" + search),
  } as never;
}

beforeEach(() => {
  mockClient.GET.mockReset();
});

describe("reconciliation load", () => {
  it("redirects unauthenticated", async () => {
    await expect(
      load({ locals: {}, url: new URL("http://x/reconciliation") } as never),
    ).rejects.toMatchObject({ status: 303 });
  });

  it("redirects non-vendor", async () => {
    await expect(load(loadEvent("", { role: "employee" }))).rejects.toMatchObject({
      status: 303,
      location: "/login",
    });
  });

  it("returns reconciliation + settlements for explicit period", async () => {
    mockClient.GET.mockImplementation((path: string) => {
      if (path === "/api/merchant/reconciliation")
        return Promise.resolve({ data: { reconciliation: { period: "2026-04" } } });
      if (path === "/api/merchant/settlements")
        return Promise.resolve({ data: { items: [{ id: "s1" }] } });
      return Promise.resolve({ data: {} });
    });
    const res = (await load(loadEvent("?period=2026-04"))) as {
      period: string;
      reconciliation: unknown;
      settlements: unknown[];
    };
    expect(res.period).toBe("2026-04");
    expect(res.reconciliation).toEqual({ period: "2026-04" });
    expect(res.settlements).toHaveLength(1);
  });

  it("defaults to current period when none supplied", async () => {
    mockClient.GET.mockResolvedValue({ data: {} });
    const res = (await load(loadEvent())) as { period: string };
    expect(res.period).toMatch(/^\d{4}-\d{2}$/);
  });

  it("defaults reconciliation null and settlements [] on throw / missing data", async () => {
    mockClient.GET.mockRejectedValue(new Error("boom"));
    let res = (await load(loadEvent("?period=2026-04"))) as {
      reconciliation: unknown;
      settlements: unknown[];
    };
    expect(res.reconciliation).toBeNull();
    expect(res.settlements).toEqual([]);

    mockClient.GET.mockResolvedValue({ data: {} });
    res = (await load(loadEvent("?period=2026-04"))) as {
      reconciliation: unknown;
      settlements: unknown[];
    };
    expect(res.reconciliation).toBeNull();
    expect(res.settlements).toEqual([]);
  });
});
