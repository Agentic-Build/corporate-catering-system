import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";

const { createAuthHandle, sequence } = vi.hoisted(() => ({
  createAuthHandle: vi.fn(() => "HANDLE"),
  sequence: vi.fn((...handlers: unknown[]) => ({ tag: "sequence", handlers })),
}));

vi.mock("@tbite/web-auth/server", () => ({ createAuthHandle }));
vi.mock("@sveltejs/kit/hooks", () => ({ sequence }));

beforeEach(() => {
  createAuthHandle.mockClear();
  sequence.mockClear();
});

afterEach(() => {
  delete process.env.API_BASE_URL;
  delete process.env.NODE_ENV;
  delete process.env.COOKIE_DOMAIN;
});

describe("hooks.server handle", () => {
  it("uses default apiBaseUrl, non-secure cookie, no domain by default", async () => {
    delete process.env.API_BASE_URL;
    delete process.env.NODE_ENV;
    delete process.env.COOKIE_DOMAIN;
    const mod = await import("./hooks.server?default");
    expect(createAuthHandle).toHaveBeenCalledWith({
      apiBaseUrl: "http://localhost:8080",
      cookieSecure: false,
      cookieDomain: undefined,
      cookieName: "tbite_sid_admin",
    });
    expect(sequence).toHaveBeenCalledWith("HANDLE");
    expect(mod.handle).toEqual({ tag: "sequence", handlers: ["HANDLE"] });
  });

  it("uses explicit env values: secure in production with a cookie domain", async () => {
    process.env.API_BASE_URL = "http://api:7000";
    process.env.NODE_ENV = "production";
    process.env.COOKIE_DOMAIN = ".example.com";
    await import("./hooks.server?prod");
    expect(createAuthHandle).toHaveBeenCalledWith({
      apiBaseUrl: "http://api:7000",
      cookieSecure: true,
      cookieDomain: ".example.com",
      cookieName: "tbite_sid_admin",
    });
  });
});
