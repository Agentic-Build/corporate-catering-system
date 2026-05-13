import { redirect, fail, error } from "@sveltejs/kit";
import type { Actions, PageServerLoad } from "./$types";
import { createApiClient } from "@tbite/api-client";
import { API_BASE_URL } from "$lib/server/env";

export const load: PageServerLoad = async ({ locals, params, url }) => {
  if (!locals.user) {
    throw redirect(303, "/login?return_to=" + encodeURIComponent(url.pathname));
  }
  const client = createApiClient(API_BASE_URL, locals.apiToken);
  const r = await client.GET("/api/employee/orders/{id}", {
    params: { path: { id: params.id } },
  });
  if (r.error || !r.data) throw error(404, "order not found");
  return { user: locals.user, order: (r.data as any).order };
};

export const actions: Actions = {
  cancel: async ({ locals, params }) => {
    if (!locals.user) return fail(401, { error: "unauthenticated" });
    const client = createApiClient(API_BASE_URL, locals.apiToken);
    const r = await client.POST("/api/employee/orders/{id}/cancel", {
      params: { path: { id: params.id } },
    });
    if (r.error) return fail(400, { error: JSON.stringify(r.error) });
    throw redirect(303, `/orders/${params.id}`);
  },
};
