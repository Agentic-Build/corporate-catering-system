import { describe, it, expect, afterEach, vi } from "vitest";

const KEYS = ["API_BASE_URL"] as const;
const saved: Record<string, string | undefined> = {};
for (const k of KEYS) saved[k] = process.env[k];

afterEach(() => {
  for (const k of KEYS) {
    if (saved[k] === undefined) delete process.env[k];
    else process.env[k] = saved[k];
  }
  vi.resetModules();
});

describe("lib/server/env", () => {
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
