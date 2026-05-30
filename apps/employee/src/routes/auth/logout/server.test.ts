import { describe, it, expect, afterEach, beforeEach, vi } from "vitest";

const createAuthLogoutHandler = vi.fn(() => "LOGOUT");
vi.mock("@tbite/web-auth/routes", () => ({ createAuthLogoutHandler }));

const KEYS = ["API_BASE_URL", "NODE_ENV", "COOKIE_DOMAIN"] as const;
const saved: Record<string, string | undefined> = {};
for (const k of KEYS) saved[k] = process.env[k];

beforeEach(() => createAuthLogoutHandler.mockClear());
afterEach(() => {
  for (const k of KEYS) {
    if (saved[k] === undefined) delete process.env[k];
    else process.env[k] = saved[k];
  }
  vi.resetModules();
});

describe("auth/logout +server", () => {
  it("wires createAuthLogoutHandler with defaults", async () => {
    delete process.env.API_BASE_URL;
    process.env.NODE_ENV = "development";
    process.env.COOKIE_DOMAIN = "";
    vi.resetModules();

    const mod = await import("./+server");
    expect(mod.POST).toBe("LOGOUT");
    expect(createAuthLogoutHandler).toHaveBeenCalledWith({
      portal: "employee",
      cookieName: "tbite_sid_employee",
      apiBaseUrl: "http://localhost:8080",
      cookieDomain: undefined,
      cookieSecure: false,
    });
  });

  it("wires createAuthLogoutHandler for production with explicit env", async () => {
    process.env.API_BASE_URL = "http://api.prod.test";
    process.env.NODE_ENV = "production";
    process.env.COOKIE_DOMAIN = "tbite.example";
    vi.resetModules();

    await import("./+server");
    expect(createAuthLogoutHandler).toHaveBeenCalledWith({
      portal: "employee",
      cookieName: "tbite_sid_employee",
      apiBaseUrl: "http://api.prod.test",
      cookieDomain: "tbite.example",
      cookieSecure: true,
    });
  });
});
