import { redirect, fail, error } from "@sveltejs/kit";
import type { Actions, PageServerLoad } from "./$types";
import { apiFor } from "$lib/server/api";

const KNOWN_PLANTS = ["F12B-3F", "F12B-1F", "F15-2F", "F18-RF"];

export const load: PageServerLoad = async ({ locals, params, url }) => {
  if (!locals.user || locals.user.role !== "welfare_admin") throw redirect(303, "/login");
  const client = apiFor(locals.apiToken);
  const r = await client.GET("/api/admin/vendors", { params: { query: {} } });
  const all = (r.data as any)?.items ?? [];
  const vendor = all.find((v: any) => v.id === params.id);
  if (!vendor) throw error(404, "vendor not found");
  return { user: locals.user, vendor, knownPlants: KNOWN_PLANTS };
};

export const actions: Actions = {
  approve: async ({ request, params, locals }) => {
    const fd = await request.formData();
    const plants = fd.getAll("plants").map(String);
    const client = apiFor(locals.apiToken);
    const r = await client.POST("/api/admin/vendors/{id}/approve", {
      params: { path: { id: params.id } },
      body: { plants } as any,
    });
    if (r.error) return fail(500, { error: JSON.stringify(r.error) });
    throw redirect(303, `/vendors/${params.id}`);
  },
  suspend: async ({ params, locals }) => {
    const client = apiFor(locals.apiToken);
    const r = await client.POST("/api/admin/vendors/{id}/suspend", {
      params: { path: { id: params.id } },
    });
    if (r.error) return fail(500, { error: JSON.stringify(r.error) });
    throw redirect(303, `/vendors/${params.id}`);
  },
  reinstate: async ({ params, locals }) => {
    const client = apiFor(locals.apiToken);
    const r = await client.POST("/api/admin/vendors/{id}/reinstate", {
      params: { path: { id: params.id } },
    });
    if (r.error) return fail(500, { error: JSON.stringify(r.error) });
    throw redirect(303, `/vendors/${params.id}`);
  },
  invite: async ({ params, locals }) => {
    const client = apiFor(locals.apiToken);
    const r = await client.POST("/api/admin/vendors/{id}/invite", {
      params: { path: { id: params.id } },
    });
    if (r.error) return fail(500, { error: JSON.stringify(r.error) });
    return { invite: (r.data as any)?.code as string };
  },
};
