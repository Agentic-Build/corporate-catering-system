import { redirect, fail, error } from "@sveltejs/kit";
import type { Actions, PageServerLoad } from "./$types";
import { apiFor } from "$lib/server/api";

export const load: PageServerLoad = async ({ locals, params, url }) => {
  if (!locals.user) throw redirect(303, "/login?return_to=" + encodeURIComponent(url.pathname));
  const client = apiFor(locals.apiToken);
  const r = await client.GET("/api/merchant/menu-items", { params: { query: { include_archived: true } } });
  const items = (r.data as any)?.items ?? [];
  const item = items.find((i: any) => i.id === params.id);
  if (!item) throw error(404, "not found");
  return { user: locals.user, item };
};

export const actions: Actions = {
  update: async ({ request, params, locals }) => {
    const fd = await request.formData();
    const body = {
      name: String(fd.get("name") ?? ""),
      description: String(fd.get("description") ?? ""),
      price_minor: parseInt(String(fd.get("price") ?? "0"), 10),
      tags: String(fd.get("tags") ?? "").split(",").map(s => s.trim()).filter(Boolean),
      badges: String(fd.get("badges") ?? "").split(",").map(s => s.trim()).filter(Boolean),
    };
    const client = apiFor(locals.apiToken);
    const r = await client.PATCH("/api/merchant/menu-items/{id}", { params: { path: { id: params.id } }, body: body as any });
    if (r.error) return fail(500, { error: JSON.stringify(r.error) });
    throw redirect(303, "/menus");
  },
  publish: async ({ params, locals }) => {
    const client = apiFor(locals.apiToken);
    const r = await client.POST("/api/merchant/menu-items/{id}/publish", { params: { path: { id: params.id } } });
    if (r.error) return fail(500, { error: JSON.stringify(r.error) });
    throw redirect(303, "/menus");
  },
  archive: async ({ params, locals }) => {
    const client = apiFor(locals.apiToken);
    const r = await client.POST("/api/merchant/menu-items/{id}/archive", { params: { path: { id: params.id } } });
    if (r.error) return fail(500, { error: JSON.stringify(r.error) });
    throw redirect(303, "/menus");
  },
};
