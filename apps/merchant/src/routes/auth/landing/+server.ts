import { redirect, error } from "@sveltejs/kit";
import { env } from "$env/dynamic/private";
import { setSessionCookie } from "@tbite/web-auth/server";

export async function GET(event) {
  const token = event.url.searchParams.get("token");
  const returnTo = event.url.searchParams.get("return_to") ?? "/";
  if (!token) throw error(400, "missing token");
  setSessionCookie(event, token, {
    apiBaseUrl: "",
    cookieSecure: env.NODE_ENV === "production",
    cookieDomain: env.COOKIE_DOMAIN || undefined,
    cookieName: "tbite_sid_merchant",
  });
  throw redirect(303, returnTo);
}
