import { sequence } from "@sveltejs/kit/hooks";
import { createAuthHandle } from "@tbite/web-auth/server";
import { env } from "$env/dynamic/private";

const apiBaseUrl = env.API_BASE_URL ?? "http://localhost:8080";

export const handle = sequence(
  createAuthHandle({
    apiBaseUrl,
    cookieSecure: env.NODE_ENV === "production",
    cookieDomain: env.COOKIE_DOMAIN || undefined,
    cookieName: "tbite_sid_admin",
  }),
);
