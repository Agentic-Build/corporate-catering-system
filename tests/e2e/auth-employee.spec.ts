import { test, expect } from "@playwright/test";

test("employee can login via fake google provider", async ({ page }) => {
  await page.goto("/login");
  await expect(page.getByText("員工登入")).toBeVisible();
  await page.getByText("使用 Google 繼續").click();
  // FakeProvider chain:
  //   /auth/start (SvelteKit) -> POST /auth/google/start (Go API)
  //   -> 303 to /test/oidc/google/authorize (Go API, fake consent)
  //   -> 302 to /auth/google/callback?state=...&code=fake&app=employee
  //   -> 303 to http://localhost:5173/auth/landing?token=...&return_to=/
  //   -> 303 to /
  await page.waitForURL(/\/$/, { timeout: 15_000 });
  // P2 replaced the simple landing with the menu UI. The display name now
  // appears in the greeting heading "哈囉，{name} 👋".
  await expect(page.getByText("哈囉，E2E 員工")).toBeVisible();
});
