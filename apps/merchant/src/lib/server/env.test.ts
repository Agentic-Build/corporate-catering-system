import { describe, it, expect, afterEach, vi } from "vitest";

const KEYS = ["API_BASE_URL"];

afterEach(() => {
  for (const k of KEYS) delete process.env[k];
  vi.resetModules();
});

describe("server/env API_BASE_URL", () => {
  it("falls back to localhost default when API_BASE_URL is unset", async () => {
    delete process.env.API_BASE_URL;
    vi.resetModules();
    const mod = await import("./env");
    expect(mod.API_BASE_URL).toBe("http://localhost:8080");
  });

  it("uses the explicit API_BASE_URL when set", async () => {
    process.env.API_BASE_URL = "http://api.example.test";
    vi.resetModules();
    const mod = await import("./env");
    expect(mod.API_BASE_URL).toBe("http://api.example.test");
  });
});
