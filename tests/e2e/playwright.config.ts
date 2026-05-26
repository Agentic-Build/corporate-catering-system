import { defineConfig } from "@playwright/test";

export default defineConfig({
  testDir: ".",
  timeout: 30_000,
  // Serial: all employee specs share the same seeded identity and session
  // state.
  workers: 1,
  use: {
    baseURL: process.env.E2E_BASE_URL ?? "http://app.tbite.local",
    headless: true,
    trace: "retain-on-failure",
  },
  reporter: [["list"]],
});
