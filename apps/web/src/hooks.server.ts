import type { Handle } from "@sveltejs/kit";

import { error, redirect } from "@sveltejs/kit";

import { authRuntime } from "./lib/server/auth";
import { hasPermission, validateActorScope } from "./lib/server/auth/contracts";
import { resolveScopeRequirements } from "./lib/server/auth/guards";

export const handle: Handle = async ({ event, resolve }) => {
  const auth = await authRuntime.authenticate(event);

  event.locals.auth = auth;
  event.locals.actor = auth.actor;

  if (auth.actor) {
    const scopeIssue = validateActorScope(auth.actor);
    if (scopeIssue) {
      authRuntime.clearSession(event);
      throw error(403, `actor scope is invalid: ${scopeIssue.message}`);
    }
  }

  const guard = authRuntime.resolveRoleGuard(event.url.pathname);
  if (guard) {
    if (!auth.actor) {
      // Soft-redirect page navigation back to the sign-in landing; keep 401 for non-navigation callers.
      if (isPageNavigation(event.request)) {
        const next = encodeURIComponent(event.url.pathname + event.url.search);
        throw redirect(303, `/?flash=auth-required&next=${next}`);
      }
      throw error(401, `authentication is required for ${event.url.pathname}`);
    }

    if (!guard.allowedRoles.includes(auth.actor.role)) {
      // Soft-redirect cross-role navigation back to the actor's own portal with a flash message.
      if (isPageNavigation(event.request)) {
        const attempted = encodeURIComponent(event.url.pathname);
        throw redirect(303, `/${auth.actor.role}?flash=cross-role&attempted=${attempted}`);
      }
      throw error(
        403,
        `role ${auth.actor.role} cannot access ${event.url.pathname}; allowed roles: ${guard.allowedRoles.join(", ")}`
      );
    }

    for (const requiredPermission of guard.requiredPermissions) {
      if (!hasPermission(auth.actor, requiredPermission)) {
        throw error(
          403,
          `permission ${requiredPermission} is required for ${event.url.pathname}`
        );
      }
    }

    const scopeResolution = resolveScopeRequirements(event.url.pathname);
    if (scopeResolution.hasMalformedEncoding) {
      throw error(400, `malformed scoped path encoding in ${event.url.pathname}`);
    }

    for (const requirement of scopeResolution.requirements) {
      if (hasPermission(auth.actor, "scope:all")) {
        continue;
      }

      if (
        requirement.kind === "vendorId" &&
        !auth.actor.scope.vendorIds.includes(requirement.value)
      ) {
        throw error(
          403,
          `actor is not scoped for vendor ${requirement.value} at ${event.url.pathname}`
        );
      }

      if (
        requirement.kind === "plantId" &&
        !auth.actor.scope.plantIds.includes(requirement.value)
      ) {
        throw error(
          403,
          `actor is not scoped for plant ${requirement.value} at ${event.url.pathname}`
        );
      }
    }
  }

  const response = await resolve(event);
  const method = event.request.method.toUpperCase();
  if (method !== "GET" && method !== "HEAD") {
    return response;
  }

  const pathname = event.url.pathname;
  const dynamicCacheControl = process.env.FRONTEND_CACHE_CONTROL_DYNAMIC ?? "no-store";
  const assetCacheControl =
    process.env.FRONTEND_CACHE_CONTROL_ASSET ?? "public, max-age=300";
  const immutableAssetCacheControl =
    process.env.FRONTEND_CACHE_CONTROL_ASSET_IMMUTABLE ??
    "public, max-age=31536000, immutable";

  if (pathname.startsWith("/_app/immutable/")) {
    if (!response.headers.has("cache-control")) {
      response.headers.set("cache-control", immutableAssetCacheControl);
    }
    return response;
  }

  if (pathname.startsWith("/_app/")) {
    if (!response.headers.has("cache-control")) {
      response.headers.set("cache-control", assetCacheControl);
    }
    return response;
  }

  response.headers.set("cache-control", dynamicCacheControl);
  return response;
};

function isPageNavigation(request: Request): boolean {
  const accept = request.headers.get("accept") ?? "";
  // HTML navigation requests include text/html; fetch/API callers typically set application/json.
  return accept.includes("text/html");
}
