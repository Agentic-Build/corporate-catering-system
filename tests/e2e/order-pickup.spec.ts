import { test, expect } from "@playwright/test";
import { loginEmployee, pickTomorrow } from "./auth";

test("scan page manual fallback rejects an order that is not ready", async ({ page }) => {
  await loginEmployee(page);

  // The default cutoff is 17:00 UTC the day BEFORE supply_date, so "today" is
  // already past cutoff. Switch to tomorrow's day tab before submitting.
  await pickTomorrow(page);

  // Locate first non-sold-out MealCard and click +
  const cards = page.locator("article");
  await expect(cards.first()).toBeVisible({ timeout: 10_000 });
  await cards.first().locator('button[aria-label="增加"]').click();

  // Open the cart drawer (header cart button) then submit from inside it.
  await page.getByRole("button", { name: "購物車", exact: true }).click();
  const submitBtn = page.getByRole("button", { name: /送出預訂/ });
  await expect(submitBtn).toBeVisible();
  await submitBtn.click();

  // After successful place, app redirects to /orders/[id]
  await page.waitForURL(/\/orders\/.+/, { timeout: 10_000 });
  const m = page.url().match(/\/orders\/([0-9a-f-]+)/);
  expect(m).not.toBeNull();
  const orderID = m![1];

  // New flow: pickup is employee self-scan at /scan. The camera is unavailable
  // in headless E2E, so exercise the manual order-number fallback instead. The
  // order is still 'placed' (the vendor hasn't scanned it to 'ready'), so the
  // fallback must not find a matching ready order and must reject the pickup.
  await page.goto("/scan");
  await page.getByPlaceholder(/3f9a1c4b/).fill(orderID.slice(0, 8));
  await page.getByRole("button", { name: "核銷" }).click();

  await expect(page.getByText(/找不到符合的待領訂單/)).toBeVisible({ timeout: 5_000 });
});
