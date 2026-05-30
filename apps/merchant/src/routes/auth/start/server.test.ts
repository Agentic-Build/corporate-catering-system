import { describe, it, expect, afterEach, vi } from "vitest";

const createAuthStartHandler = vi.fn((cfg: unknown) => ({ handler: cfg }));

vi.mock("@tbite/web-auth/routes", () => ({
  createAuthStartHandler: (cfg: unknown) => createAuthStartHandler(cfg),
}));

const KEYS = ["API_BASE_URL", "NODE_ENV", "COOKIE_DOMAIN"];

afterEach(() => {
  for (const k of KEYS) delete process.env[k];
  createAuthStartHandler.mockClear();
  vi.resetModules();
});

describe("auth/start GET wiring", () => {
  it("wires defaults", async () => {
    delete process.env.API_BASE_URL;
    delete process.env.NODE_ENV;
    delete process.env.COOKIE_DOMAIN;
    vi.resetModules();

    const mod = await import("./+server");
    expect(mod.GET).toBeDefined();
    expect(createAuthStartHandler).toHaveBeenCalledWith({
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
    expect(createAuthStartHandler).toHaveBeenCalledWith({
      portal: "merchant",
      cookieName: "tbite_sid_merchant",
      apiBaseUrl: "http://api.prod.test",
      cookieDomain: ".tbite.dev",
      cookieSecure: true,
    });
  });
});
