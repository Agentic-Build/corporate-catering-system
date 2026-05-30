import { describe, it, expect, afterEach } from "vitest";

afterEach(() => {
  delete process.env.API_BASE_URL;
});

describe("lib/server/env API_BASE_URL", () => {
  it("falls back to the default when API_BASE_URL is unset", async () => {
    delete process.env.API_BASE_URL;
    const mod = await import("./env?env-default");
    expect(mod.API_BASE_URL).toBe("http://localhost:8080");
  });

  it("uses the explicit API_BASE_URL when set", async () => {
    process.env.API_BASE_URL = "http://explicit:9000";
    const mod = await import("./env?env-explicit");
    expect(mod.API_BASE_URL).toBe("http://explicit:9000");
  });
});
