import { test, expect } from "@playwright/test";
import { loginEmployee } from "./auth";

// P9 personalization: chip carousels + ⭐ favorites. Runs against the local
// dev stack (`make dev`) — same prerequisites as the
// other employee specs in this directory.

test("cold-start employee sees three empty chip rows with onboarding hints", async ({ page }) => {
  await loginEmployee(page);

  // The three chip-row headings are stable anchors; the empty placeholders
  // carry the spec-mandated copy.
  await expect(page.getByRole("region", { name: "再點一次" })).toBeVisible();
  await expect(page.getByRole("region", { name: "推薦你今天" })).toBeVisible();
  await expect(page.getByRole("region", { name: "我的最愛" })).toBeVisible();

  await expect(page.getByText("還沒有訂單紀錄 — 點完第一份午餐後就會出現")).toBeVisible();
  await expect(page.getByText("點 ⭐ 收藏喜歡的菜色")).toBeVisible();
});

test("clicking ⭐ on a meal card surfaces it in the 我的最愛 row", async ({ page }) => {
  await loginEmployee(page);

  // Pick "明天" so a non-cutoff menu is loaded with star buttons available.
  await page.getByRole("button", { name: /明天/ }).click();

  const firstCard = page.locator("article").first();
  await expect(firstCard).toBeVisible({ timeout: 10_000 });
  const itemName = await firstCard.locator("h3, [class*='font-bold']").first().textContent();

  // Toggle the star — uses optimistic UI then refetches.
  const star = firstCard.getByRole("button", { name: "加入最愛" });
  await star.click();

  // After invalidateAll the 我的最愛 carousel should contain the item.
  const favRow = page.getByRole("region", { name: "我的最愛" });
  await expect(favRow).toContainText(itemName?.trim() ?? "", { timeout: 5_000 });

  // 「看更多」 page also lists it.
  await favRow.getByRole("link", { name: /看更多/ }).click();
  await page.waitForURL(/\/menu\/favorites/);
  await expect(page.getByText(itemName?.trim() ?? "")).toBeVisible();
});

// Deferred scenarios. The dev-stack seed does not yet provide the
// preconditions these need; reactivate once seed scripts cover them.
//
// 3. Employee places + picks up today's order → home target_day flips to
//    tomorrow. Needs an API hook (or test-only endpoint) to transition the
//    order through ready→picked_up; current dev stack only exposes the
//    cutoff path.
//
// 4. Employee reorders a past order with 1 archived item → 201 + toast
//    "3 項中 2 項已加入購物車...". Needs seeded order_item referencing a
//    menu_item that was later archived.
//
// 5. Same-plant peer ordering boosts recommend ranking. Needs multiple
//    seeded employee identities + per-user order history in the same plant
//    to exercise the popularity × affinity score.
