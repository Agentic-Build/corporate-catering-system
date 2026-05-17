import type { Page } from "@playwright/test";

export async function loginEmployee(page: Page) {
  await page.goto("/login");
  await page.getByText("使用 Authentik 登入").click();
  await page.getByLabel(/username|email|帳號|電子/i).fill("e2e-employee@tbite.test");
  await page.getByRole("button", { name: /continue|next|log in|sign in|登入|繼續/i }).click();
  await page.getByLabel(/password|密碼/i).fill("tbite-dev-pass");
  await page.getByRole("button", { name: /continue|next|log in|sign in|登入|繼續/i }).click();
  await page.waitForURL(/\/$/, { timeout: 20_000 });
}
