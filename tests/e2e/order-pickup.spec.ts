import { test, expect } from "@playwright/test";
import { loginEmployee } from "./auth";

test("pickup page redirects to order detail when not ready", async ({ page }) => {
  await loginEmployee(page);

  // The default cutoff is 17:00 UTC the day BEFORE supply_date, so "today" is
  // already past cutoff. Switch to "明天" (tomorrow) before submitting.
  await page.getByRole("button", { name: /明天/ }).click();

  // Locate first non-sold-out MealCard and click +
  const cards = page.locator("article");
  await expect(cards.first()).toBeVisible({ timeout: 10_000 });
  await cards.first().locator('button[aria-label="增加"]').click();

  // Submit the cart bar
  const submitBtn = page.getByRole("button", { name: /送出預訂/ });
  await expect(submitBtn).toBeVisible();
  await submitBtn.click();

  // After successful place, app redirects to /orders/[id]
  await page.waitForURL(/\/orders\/.+/, { timeout: 10_000 });
  const m = page.url().match(/\/orders\/([0-9a-f-]+)/);
  expect(m).not.toBeNull();
  const orderID = m![1];

  // Order is in 'placed' status → /pickup should redirect back to /orders/[id]
  await page.goto(`/orders/${orderID}/pickup`);
  await page.waitForURL(`**/orders/${orderID}`, { timeout: 5_000 });
  await expect(page.getByText("訂單詳情")).toBeVisible();
});
