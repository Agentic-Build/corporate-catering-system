import { test, expect } from "@playwright/test";

const ADMIN_URL = process.env.E2E_ADMIN_BASE_URL;
test.skip(!ADMIN_URL, "E2E_ADMIN_BASE_URL not set");

test("admin governance routes redirect to login when unauthenticated", async ({ page }) => {
  if (!ADMIN_URL) return;
  for (const path of ["/anomalies", "/dlq", "/audit"]) {
    await page.goto(`${ADMIN_URL}${path}`);
    await page.waitForURL(/\/login/, { timeout: 5_000 });
    expect(page.url()).toMatch(/login/);
  }
});
