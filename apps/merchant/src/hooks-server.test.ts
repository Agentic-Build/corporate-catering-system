import { describe, it, expect, afterEach, vi } from "vitest";

const createAuthHandle = vi.fn((cfg: unknown) => ({ handle: cfg }));

vi.mock("@tbite/web-auth/server", () => ({
  createAuthHandle: (cfg: unknown) => createAuthHandle(cfg),
}));

const KEYS = ["API_BASE_URL", "NODE_ENV", "COOKIE_DOMAIN"];

afterEach(() => {
  for (const k of KEYS) delete process.env[k];
  createAuthHandle.mockClear();
  vi.resetModules();
});

describe("hooks.server handle wiring", () => {
  it("uses defaults: localhost apiBaseUrl, insecure cookie, no domain", async () => {
    delete process.env.API_BASE_URL;
    delete process.env.NODE_ENV;
    delete process.env.COOKIE_DOMAIN;
    vi.resetModules();

    const mod = await import("./hooks.server");
    expect(mod.handle).toBeTypeOf("function");
    expect(createAuthHandle).toHaveBeenCalledTimes(1);
    expect(createAuthHandle).toHaveBeenCalledWith({
      apiBaseUrl: "http://localhost:8080",
      cookieSecure: false,
      cookieDomain: undefined,
      cookieName: "tbite_sid_merchant",
    });
  });

  it("uses explicit env: custom apiBaseUrl, secure cookie in production, set domain", async () => {
    process.env.API_BASE_URL = "http://api.prod.test";
    process.env.NODE_ENV = "production";
    process.env.COOKIE_DOMAIN = ".tbite.dev";
    vi.resetModules();

    await import("./hooks.server");
    expect(createAuthHandle).toHaveBeenCalledWith({
      apiBaseUrl: "http://api.prod.test",
      cookieSecure: true,
      cookieDomain: ".tbite.dev",
      cookieName: "tbite_sid_merchant",
    });
  });
});
