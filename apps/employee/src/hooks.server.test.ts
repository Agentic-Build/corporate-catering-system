import { describe, it, expect, afterEach, beforeEach, vi } from "vitest";

const createAuthHandle = vi.fn(() => "HANDLE");
vi.mock("@tbite/web-auth/server", () => ({ createAuthHandle }));

const KEYS = ["API_BASE_URL", "NODE_ENV", "COOKIE_DOMAIN"] as const;
const saved: Record<string, string | undefined> = {};
for (const k of KEYS) saved[k] = process.env[k];

beforeEach(() => {
  createAuthHandle.mockClear();
});

afterEach(() => {
  for (const k of KEYS) {
    if (saved[k] === undefined) delete process.env[k];
    else process.env[k] = saved[k];
  }
  vi.resetModules();
});

describe("hooks.server", () => {
  it("wires createAuthHandle with defaults (non-prod, no domain, default api url)", async () => {
    delete process.env.API_BASE_URL;
    process.env.NODE_ENV = "development";
    process.env.COOKIE_DOMAIN = "";
    vi.resetModules();

    const mod = await import("./hooks.server");
    expect(mod.handle).toBeTypeOf("function");
    expect(createAuthHandle).toHaveBeenCalledWith({
      apiBaseUrl: "http://localhost:8080",
      cookieSecure: false,
      cookieDomain: undefined,
      cookieName: "tbite_sid_employee",
    });
  });

  it("wires createAuthHandle for production with explicit api url and domain", async () => {
    process.env.API_BASE_URL = "http://api.prod.test";
    process.env.NODE_ENV = "production";
    process.env.COOKIE_DOMAIN = "tbite.example";
    vi.resetModules();

    await import("./hooks.server");
    expect(createAuthHandle).toHaveBeenCalledWith({
      apiBaseUrl: "http://api.prod.test",
      cookieSecure: true,
      cookieDomain: "tbite.example",
      cookieName: "tbite_sid_employee",
    });
  });
});
