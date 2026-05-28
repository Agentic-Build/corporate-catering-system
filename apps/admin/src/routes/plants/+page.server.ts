import { redirect, fail } from "@sveltejs/kit";
import { problemMessage } from "@tbite/web-shared";
import type { Actions, PageServerLoad } from "./$types";
import type { components } from "@tbite/api-client";
import { apiFor } from "$lib/server/api";

type PlantDTO = components["schemas"]["PlantDTO"];

export const load: PageServerLoad = async ({ locals, url }) => {
  if (!locals.user) throw redirect(303, "/login?return_to=" + encodeURIComponent(url.pathname));
  if (locals.user.role !== "welfare_admin") throw redirect(303, "/login");

  const client = apiFor(locals.apiToken);
  let plants: PlantDTO[] = [];
  try {
    const r = await client.GET("/api/admin/plants");
    if (r.data) plants = r.data.items ?? [];
  } catch {}

  return { user: locals.user, plants };
};

export const actions: Actions = {
  create: async ({ request, locals }) => {
    const fd = await request.formData();
    const code = String(fd.get("code") ?? "").trim();
    const label = String(fd.get("label") ?? "").trim();
    const address = String(fd.get("address") ?? "").trim();
    const sortOrder = parseInt(String(fd.get("sort_order") ?? "0"), 10) || 0;
    if (!code || !label) return fail(400, { error: "code and label are required" });

    const client = apiFor(locals.apiToken);
    const r = await client.POST("/api/admin/plants", {
      body: { code, label, address, sort_order: sortOrder },
    });
    if (r.error) return fail(500, { error: problemMessage(r.error) });
    return { ok: true };
  },

  update: async ({ request, locals }) => {
    const fd = await request.formData();
    const code = String(fd.get("code") ?? "");
    const label = String(fd.get("label") ?? "").trim();
    const address = String(fd.get("address") ?? "").trim();
    const active = fd.get("active") === "true";
    const sortOrder = parseInt(String(fd.get("sort_order") ?? "0"), 10) || 0;
    if (!code || !label) return fail(400, { error: "code and label are required" });

    const client = apiFor(locals.apiToken);
    const r = await client.PUT("/api/admin/plants/{code}", {
      params: { path: { code } },
      body: { label, address, active, sort_order: sortOrder },
    });
    if (r.error) return fail(500, { error: problemMessage(r.error) });
    return { ok: true };
  },
};
