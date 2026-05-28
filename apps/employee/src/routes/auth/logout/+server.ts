import { createAuthLogoutHandler } from "@tbite/web-auth/routes";
import { env } from "$env/dynamic/private";

export const POST = createAuthLogoutHandler({
  portal: "employee",
  cookieName: "tbite_sid_employee",
  apiBaseUrl: env.API_BASE_URL ?? "http://localhost:8080",
  cookieDomain: env.COOKIE_DOMAIN || undefined,
  cookieSecure: env.NODE_ENV === "production",
});
