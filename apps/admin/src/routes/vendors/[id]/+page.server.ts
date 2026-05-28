import { redirect, fail, error } from "@sveltejs/kit";
import type { Actions, PageServerLoad } from "./$types";
import type { components } from "@tbite/api-client";
import { apiFor } from "$lib/server/api";

type VendorDTO = components["schemas"]["VendorDTO"];
type OperatorDTO = components["schemas"]["OperatorDTO"];
type PlantDTO = components["schemas"]["PlantDTO"];

export const load: PageServerLoad = async ({ locals, params, url }) => {
  if (!locals.user || locals.user.role !== "welfare_admin") throw redirect(303, "/login");
  const client = apiFor(locals.apiToken);
  const [vendorsRes, operatorsRes, plantsRes] = await Promise.allSettled([
    client.GET("/api/admin/vendors", { params: { query: {} } }),
    client.GET("/api/admin/vendors/{id}/operators", { params: { path: { id: params.id } } }),
    client.GET("/api/admin/plants"),
  ]);
  const all: VendorDTO[] =
    vendorsRes.status === "fulfilled" ? (vendorsRes.value.data?.items ?? []) : [];
  const vendor = all.find((v) => v.id === params.id);
  if (!vendor) throw error(404, "vendor not found");
  const operators: OperatorDTO[] =
    operatorsRes.status === "fulfilled" ? (operatorsRes.value.data?.items ?? []) : [];
  const knownPlants: PlantDTO[] =
    plantsRes.status === "fulfilled" ? (plantsRes.value.data?.items ?? []) : [];
  return { user: locals.user, vendor, operators, knownPlants };
};

export const actions: Actions = {
  approve: async ({ request, params, locals }) => {
    const fd = await request.formData();
    const plants = fd.getAll("plants").map(String);
    const client = apiFor(locals.apiToken);
    const r = await client.POST("/api/admin/vendors/{id}/approve", {
      params: { path: { id: params.id } },
      body: { plants },
    });
    if (r.error) return fail(500, { error: JSON.stringify(r.error) });
    throw redirect(303, `/vendors/${params.id}`);
  },
  update: async ({ request, params, locals }) => {
    const fd = await request.formData();
    const contactEmail = String(fd.get("contact_email") ?? "").trim();
    const plants = fd.getAll("plants").map(String);
    if (!contactEmail) return fail(400, { error: "請填寫聯絡 email" });
    const client = apiFor(locals.apiToken);
    const r = await client.PATCH("/api/admin/vendors/{id}", {
      params: { path: { id: params.id } },
      body: { contact_email: contactEmail, plants },
    });
    if (r.error) return fail(500, { error: JSON.stringify(r.error) });
    throw redirect(303, `/vendors/${params.id}`);
  },
  setPlantWindow: async ({ request, params, locals }) => {
    const fd = await request.formData();
    const plant = String(fd.get("plant") ?? "");
    const serviceWindow = String(fd.get("service_window") ?? "").trim();
    if (!plant) return fail(400, { error: "缺少廠區" });
    const client = apiFor(locals.apiToken);
    const r = await client.PUT("/api/admin/vendors/{id}/plants/{plant}/window", {
      params: { path: { id: params.id, plant } },
      body: { service_window: serviceWindow },
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
      body: { email, display_name: displayName },
    });
    if (r.error) return fail(500, { error: JSON.stringify(r.error) });
    return { setupUrl: r.data?.operator.setup_url };
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
