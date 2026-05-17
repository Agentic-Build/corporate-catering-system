import { test, expect } from "@playwright/test";
import { loginEmployee } from "./auth";

test("employee sees today's seeded menu items", async ({ page }) => {
  await loginEmployee(page);
  await expect(
    page.getByText(/已選 \d 份/).or(page.getByText("挑選你今天想預訂的餐點")),
  ).toBeVisible();

  // At least one seeded item should be visible (depends on plant: the seed maps
  // all 3 vendors to F12B-3F which is also the default plant for E2E user).
  await expect(page.getByText("椒麻雞腿便當")).toBeVisible();
  await expect(page.getByText("藜麥雞胸沙拉碗")).toBeVisible();
});
