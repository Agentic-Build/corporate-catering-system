import { describe, it, expect, vi, beforeEach } from "vitest";

vi.mock("$lib/server/env", () => ({ API_BASE_URL: "http://x" }));

import { load } from "./+page.server";

function loadEvent(user: unknown, search = "") {
  return { locals: { user }, url: new URL("http://h/login" + search) } as never;
}
beforeEach(() => {
  vi.restoreAllMocks();
});

describe("login load", () => {
  it("redirects authenticated user to return_to", async () => {
    await expect(load(loadEvent({ id: "u1" }, "?return_to=/orders"))).rejects.toMatchObject({
      status: 303,
      location: "/orders",
    });
  });
  it("redirects authenticated user to / by default", async () => {
    await expect(load(loadEvent({ id: "u1" }))).rejects.toMatchObject({
      status: 303,
      location: "/",
    });
  });
  it("returns providers list and returnTo for anonymous user", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue({
      ok: true,
      json: async () => ({ items: [{ slug: "authentik", display_name: "SSO" }] }),
    } as Response);
    const res = await load(loadEvent(null, "?return_to=/me"));
    expect(res).toEqual({
      returnTo: "/me",
      providers: [{ slug: "authentik", display_name: "SSO" }],
    });
  });
  it("defaults returnTo to / and providers to empty when body has no items", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue({
      ok: true,
      json: async () => ({}),
    } as Response);
    const res = await load(loadEvent(null));
    expect(res).toEqual({ returnTo: "/", providers: [] });
  });
  it("returns empty providers when fetch is not ok", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue({ ok: false } as Response);
    const res = (await load(loadEvent(null))) as { providers: unknown[] };
    expect(res.providers).toEqual([]);
  });
});
