import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";

vi.mock("$lib/server/env", () => ({ API_BASE_URL: "http://api" }));

import { load } from "./+page.server";

function loadEvent(opts: { user?: unknown; query?: string } = {}) {
  return {
    locals: { user: "user" in opts ? opts.user : undefined },
    url: new URL("http://x/login" + (opts.query ?? "")),
  } as never;
}

beforeEach(() => {
  vi.restoreAllMocks();
});
afterEach(() => {
  vi.restoreAllMocks();
});

describe("login load", () => {
  it("redirects logged-in users to return_to or root", async () => {
    await expect(
      load(loadEvent({ user: { id: "u1" }, query: "?return_to=/dash" })),
    ).rejects.toMatchObject({ status: 303, location: "/dash" });
    await expect(load(loadEvent({ user: { id: "u1" } }))).rejects.toMatchObject({
      status: 303,
      location: "/",
    });
  });

  it("returns providers from API and default returnTo", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({
        ok: true,
        json: async () => ({ items: [{ slug: "g", display_name: "Google" }] }),
      }),
    );
    const res = (await load(loadEvent())) as Record<string, unknown>;
    expect(res.returnTo).toBe("/");
    expect(res.providers).toEqual([{ slug: "g", display_name: "Google" }]);
    expect(fetch).toHaveBeenCalledWith("http://api/auth/providers");
  });

  it("returns custom returnTo and empty providers when API not ok", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue({ ok: false }));
    const res = (await load(loadEvent({ query: "?return_to=/x" }))) as Record<string, unknown>;
    expect(res.returnTo).toBe("/x");
    expect(res.providers).toEqual([]);
  });

  it("returns empty providers when body has no items", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({ ok: true, json: async () => ({}) }),
    );
    const res = (await load(loadEvent())) as Record<string, unknown>;
    expect(res.providers).toEqual([]);
  });
});
