import { redirect, fail } from "@sveltejs/kit";
import { problemMessage } from "@tbite/web-shared";
import type { Actions, PageServerLoad } from "./$types";
import type { components, operations } from "@tbite/api-client";
import { apiFor } from "$lib/server/api";
import { formStr } from "@tbite/web-shared";

type AnomalyDTO = components["schemas"]["AnomalyDTO"];
type AnomalyQuery = NonNullable<operations["listAnomalies"]["parameters"]["query"]>;
type TriageBody = operations["triageAnomaly"]["requestBody"]["content"]["application/json"];

export const load: PageServerLoad = async ({ locals, url }) => {
  if (!locals.user) throw redirect(303, "/login?return_to=" + encodeURIComponent(url.pathname));
  if (locals.user.role !== "welfare_admin") throw redirect(303, "/login");

  const status = (url.searchParams.get("status") ?? "open") as NonNullable<AnomalyQuery["status"]>;
  const severity = (url.searchParams.get("severity") ?? "") as NonNullable<
    AnomalyQuery["severity"]
  >;

  const client = apiFor(locals.apiToken);
  let anomalies: AnomalyDTO[] = [];
  try {
    const query: AnomalyQuery = {};
    if (status) query.status = status;
    if (severity) query.severity = severity;
    const r = await client.GET("/api/admin/anomalies", { params: { query } });
    if (r.data) anomalies = r.data.items ?? [];
  } catch {}

  return { user: locals.user, anomalies, status, severity };
};

export const actions: Actions = {
  triage: async ({ request, locals }) => {
    const fd = await request.formData();
    const id = formStr(fd, "id");
    const notes = formStr(fd, "notes");
    const action = formStr(fd, "action");
    if (!id) return fail(400, { error: "id required" });
    const body: TriageBody = { notes };
    if (action === "warn" || action === "suspend") body.action = action;
    const client = apiFor(locals.apiToken);
    const r = await client.POST("/api/admin/anomalies/{id}/triage", {
      params: { path: { id } },
      body,
    });
    if (r.error) return fail(500, { error: problemMessage(r.error) });
    return { ok: true };
  },
  close: async ({ request, locals }) => {
    const fd = await request.formData();
    const id = formStr(fd, "id");
    const notes = formStr(fd, "notes");
    if (!id) return fail(400, { error: "id required" });
    const client = apiFor(locals.apiToken);
    const r = await client.POST("/api/admin/anomalies/{id}/close", {
      params: { path: { id } },
      body: { notes },
    });
    if (r.error) return fail(500, { error: problemMessage(r.error) });
    return { ok: true };
  },
};
