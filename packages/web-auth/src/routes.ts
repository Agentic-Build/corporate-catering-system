import { redirect, error, type RequestHandler } from "@sveltejs/kit";
import { clearSessionCookie, getToken, setSessionCookie } from "./server";

export type Portal = "employee" | "merchant" | "admin";

export interface AuthRouteOptions {
  portal: Portal;
  /** Per-app session cookie name (e.g. "tbite_sid_employee"). */
  cookieName: string;
  /** API base URL, e.g. process.env.API_BASE_URL. */
  apiBaseUrl: string;
  /** Optional cookie domain (shared across subdomains). */
  cookieDomain?: string;
  /** Mark cookie Secure. Defaults to true. */
  cookieSecure?: boolean;
}

/** GET /auth/start?provider=<id>&return_to=<path> — kicks the OAuth flow. */
export function createAuthStartHandler(opts: AuthRouteOptions): RequestHandler {
  return async ({ url }) => {
    const provider = url.searchParams.get("provider");
    const returnTo = url.searchParams.get("return_to") ?? "/";
    if (!provider || !/^[a-z0-9][a-z0-9_.-]*$/.test(provider)) {
      throw error(400, "bad provider");
    }
    const resp = await fetch(`${opts.apiBaseUrl}/auth/${encodeURIComponent(provider)}/start`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ app: opts.portal, return_to: returnTo }),
    });
    if (!resp.ok) throw error(502, "auth start failed");
    const data = (await resp.json()) as { auth_url: string };
    throw redirect(303, data.auth_url);
  };
}

/** GET /auth/landing?code=<otc>&return_to=<path> — exchanges code → session cookie. */
export function createAuthLandingHandler(opts: AuthRouteOptions): RequestHandler {
  return async (event) => {
    const returnTo = event.url.searchParams.get("return_to") ?? "/";
    const code = event.url.searchParams.get("code");
    // Legacy fallback: older API builds redirect with the token in the URL.
    let token = event.url.searchParams.get("token") ?? undefined;

    if (code) {
      const r = await event.fetch(`${opts.apiBaseUrl}/auth/session`, {
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
      cookieSecure: opts.cookieSecure ?? true,
      cookieDomain: opts.cookieDomain,
      cookieName: opts.cookieName,
    });
    throw redirect(303, returnTo);
  };
}

/** POST /auth/logout — best-effort upstream revoke + clear cookie. */
export function createAuthLogoutHandler(opts: AuthRouteOptions): RequestHandler {
  return async (event) => {
    const token = getToken(event, opts.cookieName);
    if (token) {
      await fetch(`${opts.apiBaseUrl}/auth/logout`, {
        method: "POST",
        headers: { Authorization: `Bearer ${token}` },
      });
    }
    clearSessionCookie(event, {
      apiBaseUrl: "",
      cookieSecure: opts.cookieSecure ?? true,
      cookieDomain: opts.cookieDomain,
      cookieName: opts.cookieName,
    });
    throw redirect(303, "/login");
  };
}
