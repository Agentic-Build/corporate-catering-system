import type { Handle } from "@sveltejs/kit";

import { error } from "@sveltejs/kit";

import { authRuntime } from "./lib/server/auth";

export const handle: Handle = async ({ event, resolve }) => {
  const auth = await authRuntime.authenticate(event);

  event.locals.auth = auth;
  event.locals.actor = auth.actor;

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
  }

  return resolve(event);
};
