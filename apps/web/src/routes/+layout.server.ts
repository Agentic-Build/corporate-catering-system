import type { LayoutServerLoad } from "./$types";

import { buildAppShellData } from "$lib/platform/shell";

export const load: LayoutServerLoad = async ({ locals, url }) => {
  return buildAppShellData({
    actor: locals.actor,
    auth: locals.auth,
    pathname: url.pathname
  });
};
