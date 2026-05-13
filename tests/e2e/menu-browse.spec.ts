import { test, expect } from "@playwright/test";

test("employee sees today's seeded menu items", async ({ page }) => {
  await page.goto("/login");
  await expect(page.getByText("員工登入")).toBeVisible();
  await page.getByText("使用 Google 繼續").click();

  // After fake-OIDC redirect chain → landing → /
  await page.waitForURL(/\/$/, { timeout: 15_000 });
  await expect(page.getByText(/已選 \d 份/).or(page.getByText("挑選你今天想預訂的餐點"))).toBeVisible();

  // At least one seeded item should be visible (depends on plant: the seed maps
  // all 3 vendors to F12B-3F which is also the default plant for E2E user).
  await expect(page.getByText("椒麻雞腿便當")).toBeVisible();
  await expect(page.getByText("藜麥雞胸沙拉碗")).toBeVisible();
});
