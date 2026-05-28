import { createAuthLandingHandler } from "@tbite/web-auth/routes";
import { env } from "$env/dynamic/private";

export const GET = createAuthLandingHandler({
  portal: "merchant",
  cookieName: "tbite_sid_merchant",
  apiBaseUrl: env.API_BASE_URL ?? "http://localhost:8080",
  cookieDomain: env.COOKIE_DOMAIN || undefined,
  cookieSecure: env.NODE_ENV === "production",
});
