import { defineConfig } from "@playwright/test";

export default defineConfig({
  testDir: ".",
  timeout: 30_000,
  // Serial: fake OIDC creates a single shared user via a non-upsert bootstrap,
  // so parallel workers race the first-login INSERT. Tests are tiny — workers=1
  // costs ~9s total and is reliable.
  workers: 1,
  use: {
    baseURL: process.env.E2E_BASE_URL ?? "http://localhost:5173",
    headless: true,
    trace: "retain-on-failure",
  },
  reporter: [["list"]],
});
