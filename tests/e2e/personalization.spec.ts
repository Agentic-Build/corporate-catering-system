import { test, expect } from "@playwright/test";
import { loginEmployee, pickTomorrow } from "./auth";

// P9 personalization: chip carousels + ⭐ favorites. Runs against the local
// dev stack (`make dev`) — same prerequisites as the
// other employee specs in this directory.

test("cold-start employee sees three empty chip rows with onboarding hints", async ({ page }) => {
  // Use a dedicated employee that no other spec orders/favorites for, so the
  // cold-start assumptions hold regardless of test order (the suite shares one
  // DB with no per-test reset; the default e2e-employee accrues orders from the
  // order specs).
  await loginEmployee(page, "emp-tnb@tbite.test");

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

  // Pick tomorrow so a non-cutoff menu is loaded with star buttons available.
  await pickTomorrow(page);

  // First menu card that is not yet favorited (favorited cards carry the
  // "取消最愛" label instead). Scoping by the star avoids picking a featured-row
  // card that lacks a favorite toggle.
  const firstCard = page.locator('article:has(button[aria-label="加入最愛"])').first();
  await expect(firstCard).toBeVisible({ timeout: 10_000 });
  const itemName = await firstCard.locator("h3, [class*='font-bold']").first().textContent();

  // Toggle the star — the home row updates optimistically, but the favorite is
  // POSTed asynchronously. Wait for that save to land before navigating to the
  // favorites page, whose SSR load reads the persisted state.
  const star = firstCard.getByRole("button", { name: "加入最愛" });
  const favSaved = page.waitForResponse((r) => /addFavorite/.test(r.url()));
  await star.click();
  await favSaved;

  // After invalidateAll the 我的最愛 carousel should contain the item.
  const favRow = page.getByRole("region", { name: "我的最愛" });
  await expect(favRow).toContainText(itemName?.trim() ?? "", { timeout: 5_000 });

  // 「查看全部」 page also lists it.
  await favRow.getByRole("link", { name: /查看全部/ }).click();
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
