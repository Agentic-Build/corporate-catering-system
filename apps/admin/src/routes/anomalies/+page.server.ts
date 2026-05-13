import { redirect, fail } from "@sveltejs/kit";
import type { Actions, PageServerLoad } from "./$types";
import { apiFor } from "$lib/server/api";

export const load: PageServerLoad = async ({ locals, url }) => {
  if (!locals.user) throw redirect(303, "/login?return_to=" + encodeURIComponent(url.pathname));
  if (locals.user.role !== "welfare_admin") throw redirect(303, "/login");

  const status = (url.searchParams.get("status") ?? "open") as "open" | "triaged" | "closed" | "";
  const severity = (url.searchParams.get("severity") ?? "") as
    | "low"
    | "medium"
    | "high"
    | "critical"
    | "";

  const client = apiFor(locals.apiToken);
  let anomalies: any[] = [];
  try {
    const query: Record<string, string> = {};
    if (status) query.status = status;
    if (severity) query.severity = severity;
    const r = await client.GET("/api/admin/anomalies", { params: { query: query as any } });
    if (r.data) anomalies = (r.data as any).items ?? [];
  } catch {}

  return { user: locals.user, anomalies, status, severity };
};

export const actions: Actions = {
  triage: async ({ request, locals }) => {
    const fd = await request.formData();
    const id = String(fd.get("id") ?? "");
    const notes = String(fd.get("notes") ?? "");
    if (!id) return fail(400, { error: "id required" });
    const client = apiFor(locals.apiToken);
    const r = await client.POST("/api/admin/anomalies/{id}/triage", {
      params: { path: { id } },
      body: { notes } as any,
    });
    if (r.error) return fail(500, { error: JSON.stringify(r.error) });
    return { ok: true };
  },
  close: async ({ request, locals }) => {
    const fd = await request.formData();
    const id = String(fd.get("id") ?? "");
    const notes = String(fd.get("notes") ?? "");
    if (!id) return fail(400, { error: "id required" });
    const client = apiFor(locals.apiToken);
    const r = await client.POST("/api/admin/anomalies/{id}/close", {
      params: { path: { id } },
      body: { notes } as any,
    });
    if (r.error) return fail(500, { error: JSON.stringify(r.error) });
    return { ok: true };
  },
};
