import type { Handle } from "@sveltejs/kit";

import { error } from "@sveltejs/kit";

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
      throw error(401, `authentication is required for ${event.url.pathname}`);
    }

    if (!guard.allowedRoles.includes(auth.actor.role)) {
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

    const scopeRequirements = resolveScopeRequirements(event.url.pathname);
    for (const requirement of scopeRequirements) {
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

  return resolve(event);
};
