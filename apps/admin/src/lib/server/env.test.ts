import { describe, it, expect, afterEach, vi } from "vitest";

afterEach(() => {
  delete process.env.API_BASE_URL;
});

describe("lib/server/env API_BASE_URL", () => {
  it("falls back to the default when API_BASE_URL is unset", async () => {
    delete process.env.API_BASE_URL;
    vi.resetModules();
    const mod = await import("./env");
    expect(mod.API_BASE_URL).toBe("http://localhost:8080");
  });

  it("uses the explicit API_BASE_URL when set", async () => {
    process.env.API_BASE_URL = "http://explicit:9000";
    vi.resetModules();
    const mod = await import("./env");
    expect(mod.API_BASE_URL).toBe("http://explicit:9000");
  });
});
