import type { RequestHandler } from "./$types";

import { error, json, redirect } from "@sveltejs/kit";

import { AUTH_ROLES, parseAuthRole, type AuthRole } from "../../../lib/server/auth/contracts";
import { authRuntime } from "../../../lib/server/auth";

const DEFAULT_ROLE_ROUTE: Readonly<Record<AuthRole, string>> = {
  employee: "/employee",
  vendor: "/vendor",
  admin: "/admin"
};

export const GET: RequestHandler = async (event) => {
  if (!authRuntime.canIssueMockSessions()) {
    throw error(404, "mock auth endpoint is unavailable for the configured auth provider");
  }

  const nextPath = normalizeNextPath(event.url.searchParams.get("next"));
  const shouldLogout = event.url.searchParams.get("logout");
  if (shouldLogout === "1" || shouldLogout === "true") {
    authRuntime.clearSession(event);
    throw redirect(303, nextPath ?? "/");
  }

  const role = parseAuthRole(event.url.searchParams.get("role"));
  if (!role) {
    return json({
      mode: "mock",
      roles: AUTH_ROLES,
      usage: "/auth/mock?role=employee&next=/employee"
    });
  }

  const auth = await authRuntime.issueMockSession(event, role);
  const landingPath = nextPath ?? DEFAULT_ROLE_ROUTE[auth.actor?.role ?? role];
  throw redirect(303, landingPath);
};

function normalizeNextPath(raw: string | null): string | null {
  if (!raw) {
    return null;
  }

  if (!raw.startsWith("/") || raw.startsWith("//")) {
    return null;
  }

  return raw;
}
