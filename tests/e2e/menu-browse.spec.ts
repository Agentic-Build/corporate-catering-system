import { test, expect } from "@playwright/test";
import { loginEmployee } from "./auth";

test("employee sees today's seeded menu items", async ({ page }) => {
  await loginEmployee(page);
  await expect(
    page.getByText(/已選 \d 份/).or(page.getByText("挑選你今天想預訂的餐點")),
  ).toBeVisible();

  // At least one seeded item should be visible. The seed maps all 10 vendors to
  // every plant (tn-a..tn-d), so the tn-a E2E user sees every vendor's menu.
  await expect(page.getByText("炙燒雞腿便當")).toBeVisible();
  await expect(page.getByText("半熟蛋滷肉飯")).toBeVisible();
});
