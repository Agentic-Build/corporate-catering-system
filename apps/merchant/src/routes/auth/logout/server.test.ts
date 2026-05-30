import { describe, it, expect, afterEach, vi } from "vitest";

const createAuthLogoutHandler = vi.fn((cfg: unknown) => ({ handler: cfg }));

vi.mock("@tbite/web-auth/routes", () => ({
  createAuthLogoutHandler: (cfg: unknown) => createAuthLogoutHandler(cfg),
}));

const KEYS = ["API_BASE_URL", "NODE_ENV", "COOKIE_DOMAIN"];

afterEach(() => {
  for (const k of KEYS) delete process.env[k];
  createAuthLogoutHandler.mockClear();
  vi.resetModules();
});

describe("auth/logout POST wiring", () => {
  it("wires defaults", async () => {
    delete process.env.API_BASE_URL;
    delete process.env.NODE_ENV;
    delete process.env.COOKIE_DOMAIN;
    vi.resetModules();

    const mod = await import("./+server");
    expect(mod.POST).toBeDefined();
    expect(createAuthLogoutHandler).toHaveBeenCalledWith({
      portal: "merchant",
      cookieName: "tbite_sid_merchant",
      apiBaseUrl: "http://localhost:8080",
      cookieDomain: undefined,
      cookieSecure: false,
    });
  });

  it("wires explicit env values", async () => {
    process.env.API_BASE_URL = "http://api.prod.test";
    process.env.NODE_ENV = "production";
    process.env.COOKIE_DOMAIN = ".tbite.dev";
    vi.resetModules();

    await import("./+server");
    expect(createAuthLogoutHandler).toHaveBeenCalledWith({
      portal: "merchant",
      cookieName: "tbite_sid_merchant",
      apiBaseUrl: "http://api.prod.test",
      cookieDomain: ".tbite.dev",
      cookieSecure: true,
    });
  });
});
