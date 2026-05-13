import type { Handle, RequestEvent } from "@sveltejs/kit";
import { createApiClient } from "@tbite/api-client";
import type { AuthOptions, Role, SessionUser } from "./types";

declare global {
  // eslint-disable-next-line @typescript-eslint/no-namespace
  namespace App {
    interface Locals {
      user: SessionUser | null;
      apiToken: string | undefined;
    }
  }
}

export const COOKIE_NAME = "tbite_sid";

export function getToken(event: RequestEvent): string | undefined {
  return event.cookies.get(COOKIE_NAME);
}

export function setSessionCookie(event: RequestEvent, token: string, opts: AuthOptions) {
  event.cookies.set(COOKIE_NAME, token, {
    path: "/",
    httpOnly: true,
    sameSite: "lax",
    secure: opts.cookieSecure ?? true,
    maxAge: 60 * 60 * 24 * 7,
    domain: opts.cookieDomain,
  });
}

export function clearSessionCookie(event: RequestEvent, opts: AuthOptions) {
  event.cookies.delete(COOKIE_NAME, { path: "/", domain: opts.cookieDomain });
}

export function createAuthHandle(opts: AuthOptions): Handle {
  return async ({ event, resolve }) => {
    const token = getToken(event);
    let user: SessionUser | null = null;
    if (token) {
      const client = createApiClient(opts.apiBaseUrl, token);
      const { data, error } = await client.GET("/me", {});
      if (data && !error) {
        user = data as unknown as SessionUser;
      } else if (error) {
        clearSessionCookie(event, opts);
      }
    }
    event.locals.user = user;
    event.locals.apiToken = token;
    return resolve(event);
  };
}

export function requireRole<L extends { user: SessionUser | null }>(
  locals: L,
  role: Role,
): SessionUser {
  if (!locals.user) throw new Error("unauthenticated");
  if (locals.user.role !== role) throw new Error("forbidden");
  return locals.user;
}

export type { AuthOptions, SessionUser, Role } from "./types";
