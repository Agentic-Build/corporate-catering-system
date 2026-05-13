import { redirect, fail, error } from "@sveltejs/kit";
import type { Actions, PageServerLoad } from "./$types";
import { apiFor } from "$lib/server/api";

export const load: PageServerLoad = async ({ locals, params, url }) => {
  if (!locals.user) throw redirect(303, "/login?return_to=" + encodeURIComponent(url.pathname));
  if (locals.user.role !== "welfare_admin") throw redirect(303, "/login");

  const client = apiFor(locals.apiToken);
  const r = await client.GET("/api/admin/payroll/batches/{id}", {
    params: { path: { id: params.id } },
  });
  if (r.error || !r.data) throw error(404, "batch not found");
  const body = r.data as any;
  return {
    user: locals.user,
    batch: body.batch,
    entries: body.entries ?? [],
  };
};

export const actions: Actions = {
  lock: async ({ params, locals }) => {
    const client = apiFor(locals.apiToken);
    const r = await client.POST("/api/admin/payroll/batches/{id}/lock", {
      params: { path: { id: params.id } },
    });
    if (r.error) return fail(500, { error: JSON.stringify(r.error) });
    throw redirect(303, `/payroll/${params.id}`);
  },
};
