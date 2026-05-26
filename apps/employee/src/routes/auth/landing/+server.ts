import { redirect, error } from "@sveltejs/kit";
import { env } from "$env/dynamic/private";
import { setSessionCookie } from "@tbite/web-auth/server";

const API_BASE_URL = env.API_BASE_URL ?? "http://localhost:8080";

export async function GET(event) {
  const returnTo = event.url.searchParams.get("return_to") ?? "/";
  const code = event.url.searchParams.get("code");
  // Legacy fallback: older API builds redirect with the token in the URL.
  let token = event.url.searchParams.get("token") ?? undefined;

  if (code) {
    const r = await event.fetch(`${API_BASE_URL}/api/auth/session`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ code }),
    });
    if (!r.ok) throw error(401, "login exchange failed");
    token = ((await r.json()) as { token?: string }).token;
  }

  if (!token) throw error(400, "missing login code");
  setSessionCookie(event, token, {
    apiBaseUrl: "",
    cookieSecure: env.NODE_ENV === "production",
    cookieDomain: env.COOKIE_DOMAIN || undefined,
    cookieName: "tbite_sid_employee",
  });
  throw redirect(303, returnTo);
}
