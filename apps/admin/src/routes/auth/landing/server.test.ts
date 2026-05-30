import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";

const { createAuthLandingHandler } = vi.hoisted(() => ({
  createAuthLandingHandler: vi.fn(() => "GET_HANDLER"),
}));

vi.mock("@tbite/web-auth/routes", () => ({ createAuthLandingHandler }));

beforeEach(() => {
  vi.resetModules();
  createAuthLandingHandler.mockClear();
});

afterEach(() => {
  delete process.env.API_BASE_URL;
  delete process.env.NODE_ENV;
  delete process.env.COOKIE_DOMAIN;
});

describe("auth/landing +server", () => {
  it("wires defaults: non-secure, no domain, default api base url", async () => {
    delete process.env.API_BASE_URL;
    delete process.env.NODE_ENV;
    delete process.env.COOKIE_DOMAIN;
    const mod = await import("./+server");
    expect(createAuthLandingHandler).toHaveBeenCalledWith({
      portal: "admin",
      cookieName: "tbite_sid_admin",
      apiBaseUrl: "http://localhost:8080",
      cookieDomain: undefined,
      cookieSecure: false,
    });
    expect(mod.GET).toBe("GET_HANDLER");
  });

  it("wires explicit env: secure in production with cookie domain", async () => {
    process.env.API_BASE_URL = "http://api:7000";
    process.env.NODE_ENV = "production";
    process.env.COOKIE_DOMAIN = ".example.com";
    await import("./+server");
    expect(createAuthLandingHandler).toHaveBeenCalledWith({
      portal: "admin",
      cookieName: "tbite_sid_admin",
      apiBaseUrl: "http://api:7000",
      cookieDomain: ".example.com",
      cookieSecure: true,
    });
  });
});
