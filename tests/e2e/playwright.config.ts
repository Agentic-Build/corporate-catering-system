import { defineConfig } from "@playwright/test";

export default defineConfig({
  testDir: ".",
  timeout: 30_000,
  // Serial: all employee specs share the same seeded Authentik identity and
  // local session state.
  workers: 1,
  use: {
    baseURL: process.env.E2E_BASE_URL ?? "http://localhost:5173",
    headless: true,
    trace: "retain-on-failure",
  },
  reporter: [["list"]],
});
