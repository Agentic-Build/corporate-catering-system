import { redirect } from "@sveltejs/kit";
import { env } from "$env/dynamic/private";
import { clearSessionCookie, getToken } from "@tbite/web-auth/server";

export async function POST(event) {
  const token = getToken(event, "tbite_sid_admin");
  if (token) {
    const apiBaseUrl = env.API_BASE_URL ?? "http://localhost:8080";
    await fetch(`${apiBaseUrl}/auth/logout`, {
      method: "POST",
      headers: { Authorization: `Bearer ${token}` },
    });
  }
  clearSessionCookie(event, {
    apiBaseUrl: "",
    cookieSecure: env.NODE_ENV === "production",
    cookieDomain: env.COOKIE_DOMAIN || undefined,
    cookieName: "tbite_sid_admin",
  });
  throw redirect(303, "/login");
}
