import type { Actions } from "./$types";

import { fail, redirect } from "@sveltejs/kit";

import { zhTW } from "$lib/i18n/zh-tw";
import { probeApiAccess } from "$lib/platform/api";
import { authRuntime } from "$lib/server/auth";
import { parseAuthRole } from "$lib/server/auth/contracts";

export const actions: Actions = {
  session: async (event) => {
    if (!authRuntime.isDevMode()) {
      return fail(404, {
        errorMessage: zhTW.home.actions.errorFallback
      });
    }

    const formData = await event.request.formData();
    const intent = normalizeFormValue(formData.get("intent"));
    const nextPath = normalizeNextPath(formData.get("next"));

    if (intent === "logout") {
      authRuntime.clearSession(event);
      throw redirect(303, nextPath ?? "/");
    }

    if (intent !== "login") {
      return fail(400, {
        errorMessage: zhTW.home.actions.errorFallback
      });
    }

    const role = parseAuthRole(normalizeFormValue(formData.get("role")));
    if (!role) {
      return fail(400, {
        errorMessage: zhTW.home.actions.errorFallback
      });
    }

    await authRuntime.issueDevSession(event, role);
    throw redirect(303, nextPath ?? `/${role}`);
  },
  probeApi: async (event) => {
    if (!event.locals.actor) {
      return fail(401, {
        errorMessage: zhTW.api.failure.statusText[401]
      });
    }

    const probeState = await probeApiAccess(event.locals.actor);
    if (probeState.status === "success") {
      return {
        successMessage: probeState.data.message
      };
    }

    if (probeState.status === "error") {
      return fail(502, {
        errorMessage: probeState.error
      });
    }

    return fail(500, {
      errorMessage: zhTW.home.actions.errorFallback
    });
  }
};

function normalizeFormValue(value: FormDataEntryValue | null): string | null {
  if (typeof value !== "string") {
    return null;
  }

  const normalized = value.trim();
  return normalized.length > 0 ? normalized : null;
}

function normalizeNextPath(raw: FormDataEntryValue | null): string | null {
  if (typeof raw !== "string") {
    return null;
  }

  if (!raw.startsWith("/") || raw.startsWith("//")) {
    return null;
  }

  return raw;
}
