import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";

vi.mock("$lib/server/env", () => ({ API_BASE_URL: "http://api.test" }));

import { load } from "./+page.server";

const VENDOR = { id: "u1", role: "vendor_operator" };

function loadEvent(search = "", user?: unknown) {
  return { locals: { user, apiToken: "t" }, url: new URL("http://x/login" + search) } as never;
}

const fetchMock = vi.fn();

beforeEach(() => {
  fetchMock.mockReset();
  vi.stubGlobal("fetch", fetchMock);
});
afterEach(() => {
  vi.unstubAllGlobals();
});

describe("login load", () => {
  it("redirects an already-authed user to return_to", async () => {
    await expect(load(loadEvent("?return_to=%2Fdashboard", VENDOR))).rejects.toMatchObject({
      status: 303,
      location: "/dashboard",
    });
  });

  it("redirects an already-authed user to / when no return_to", async () => {
    await expect(load(loadEvent("", VENDOR))).rejects.toMatchObject({ status: 303, location: "/" });
  });

  it("returns providers and return_to for an anonymous visitor", async () => {
    fetchMock.mockResolvedValue({
      ok: true,
      json: async () => ({ items: [{ slug: "google", display_name: "Google" }] }),
    });
    const res = (await load(loadEvent("?return_to=%2Forders"))) as {
      returnTo: string;
      providers: unknown[];
    };
    expect(res.returnTo).toBe("/orders");
    expect(res.providers).toEqual([{ slug: "google", display_name: "Google" }]);
    expect(fetchMock).toHaveBeenCalledWith("http://api.test/auth/providers");
  });

  it("returns empty providers when the providers fetch fails", async () => {
    fetchMock.mockResolvedValue({ ok: false });
    const res = (await load(loadEvent())) as { returnTo: string; providers: unknown[] };
    expect(res.returnTo).toBe("/");
    expect(res.providers).toEqual([]);
  });

  it("returns empty providers when the response has no items", async () => {
    fetchMock.mockResolvedValue({ ok: true, json: async () => ({}) });
    const res = (await load(loadEvent())) as { providers: unknown[] };
    expect(res.providers).toEqual([]);
  });
});
