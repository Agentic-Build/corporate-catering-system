import type { Page } from "@playwright/test";

export async function loginEmployee(page: Page, email = "e2e-employee@tbite.test") {
  await page.goto("/login");
  await page.getByText("使用 Authentik 登入").click();
  await page.getByLabel(/username|email|帳號|電子/i).fill(email);
  await page.getByRole("button", { name: /continue|next|log in|sign in|登入|繼續/i }).click();
  await page.getByLabel(/password|密碼/i).fill("tbite-dev-pass");
  await page.getByRole("button", { name: /continue|next|log in|sign in|登入|繼續/i }).click();
  await page.waitForURL(/\/$/, { timeout: 20_000 });
}

// Select tomorrow's day tab on the employee home WeekCalendar. The redesigned
// calendar renders each day as a "週{weekday} {day-of-month}" button (only the
// current day also carries a 今天 marker), so tomorrow is matched by its
// weekday + date. Used by specs that need a not-yet-past-cutoff menu.
export async function pickTomorrow(page: Page) {
  const t = new Date();
  t.setDate(t.getDate() + 1);
  const wd = ["日", "一", "二", "三", "四", "五", "六"][t.getDay()];
  await page.getByRole("button", { name: new RegExp(`^週${wd}\\s+${t.getDate()}$`) }).click();
}
