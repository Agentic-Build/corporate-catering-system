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
  const operatorsResp = await client.GET("/api/admin/vendors/{id}/operators", {
    params: { path: { id: params.id } },
  });
  const operators = (operatorsResp.data as any)?.items ?? [];
  return { user: locals.user, vendor, operators, knownPlants: KNOWN_PLANTS };
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
  createOperator: async ({ request, params, locals }) => {
    const fd = await request.formData();
    const email = String(fd.get("email") ?? "")
      .trim()
      .toLowerCase();
    const displayName = String(fd.get("display_name") ?? "").trim();
    if (!email || !displayName)
      return fail(400, { error: "operator email and display name required" });
    const client = apiFor(locals.apiToken);
    const r = await client.POST("/api/admin/vendors/{id}/operators", {
      params: { path: { id: params.id } },
      body: { email, display_name: displayName } as any,
    });
    if (r.error) return fail(500, { error: JSON.stringify(r.error) });
    return { setupUrl: (r.data as any)?.operator?.setup_url as string };
  },
  suspendOperator: async ({ request, params, locals }) => {
    const fd = await request.formData();
    const operatorID = String(fd.get("operator_id") ?? "");
    const client = apiFor(locals.apiToken);
    const r = await client.POST("/api/admin/vendors/{id}/operators/{operator_id}/suspend", {
      params: { path: { id: params.id, operator_id: operatorID } },
    });
    if (r.error) return fail(500, { error: JSON.stringify(r.error) });
    throw redirect(303, `/vendors/${params.id}`);
  },
  reinstateOperator: async ({ request, params, locals }) => {
    const fd = await request.formData();
    const operatorID = String(fd.get("operator_id") ?? "");
    const client = apiFor(locals.apiToken);
    const r = await client.POST("/api/admin/vendors/{id}/operators/{operator_id}/reinstate", {
      params: { path: { id: params.id, operator_id: operatorID } },
    });
    if (r.error) return fail(500, { error: JSON.stringify(r.error) });
    throw redirect(303, `/vendors/${params.id}`);
  },
};
