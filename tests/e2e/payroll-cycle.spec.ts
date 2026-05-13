import { test, expect } from "@playwright/test";

const ADMIN_URL = process.env.E2E_ADMIN_BASE_URL;
test.skip(!ADMIN_URL, "E2E_ADMIN_BASE_URL not set, skipping admin payroll e2e");

test("admin payroll page redirects unauthenticated to login", async ({ page }) => {
  if (!ADMIN_URL) return;
  await page.goto(`${ADMIN_URL}/payroll`);
  await page.waitForURL(/\/login/, { timeout: 5_000 });
  expect(page.url()).toMatch(/login/);
});
