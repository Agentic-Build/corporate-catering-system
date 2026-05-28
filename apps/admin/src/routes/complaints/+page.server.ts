import { redirect, fail } from "@sveltejs/kit";
import { problemMessage } from "@tbite/web-shared";
import type { components } from "@tbite/api-client";
import type { Actions, PageServerLoad } from "./$types";
import { apiFor } from "$lib/server/api";

type Complaint = components["schemas"]["ComplaintDTO"];

export const load: PageServerLoad = async ({ locals, url }) => {
  if (!locals.user) throw redirect(303, "/login?return_to=" + encodeURIComponent(url.pathname));
  if (locals.user.role !== "welfare_admin") throw redirect(303, "/login");

  const client = apiFor(locals.apiToken);
  let complaints: Complaint[] = [];
  try {
    const r = await client.GET("/api/admin/complaints");
    if (r.data) complaints = r.data.items ?? [];
  } catch {}

  return { user: locals.user, complaints };
};

export const actions: Actions = {
  resolve: async ({ request, locals }) => {
    const fd = await request.formData();
    const id = String(fd.get("id") ?? "");
    const resolution = String(fd.get("resolution") ?? "").trim();
    const compensate = fd.get("compensate") === "true";
    if (!id) return fail(400, { error: "缺少客訴編號" });
    if (resolution.length < 5) return fail(400, { error: "結案說明至少需 5 個字" });

    const client = apiFor(locals.apiToken);
    const r = await client.POST("/api/admin/complaints/{id}/resolve", {
      params: { path: { id } },
      body: { resolution, compensate },
    });
    if (r.error) return fail(500, { error: problemMessage(r.error) });
    return { ok: true, resolved: id };
  },
};
