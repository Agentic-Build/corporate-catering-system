import { test, expect } from "@playwright/test";
import { loginEmployee } from "./auth";

test("employee can login via Authentik", async ({ page }) => {
  await loginEmployee(page);
  await expect(page.getByText("哈囉，E2E Employee")).toBeVisible();
});
