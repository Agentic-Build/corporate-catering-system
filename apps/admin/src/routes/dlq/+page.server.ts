import { redirect, fail } from "@sveltejs/kit";
import type { Actions, PageServerLoad } from "./$types";
import { apiFor } from "$lib/server/api";

export const load: PageServerLoad = async ({ locals, url }) => {
  if (!locals.user) throw redirect(303, "/login?return_to=" + encodeURIComponent(url.pathname));
  if (locals.user.role !== "welfare_admin") throw redirect(303, "/login");

  const stream = url.searchParams.get("stream") ?? "";
  const client = apiFor(locals.apiToken);
  let messages: any[] = [];
  try {
    const query: Record<string, string | number> = { limit: 200 };
    if (stream) query.stream = stream;
    const r = await client.GET("/api/admin/dlq", { params: { query: query as any } });
    if (r.data) messages = (r.data as any).items ?? [];
  } catch {}
  return { user: locals.user, messages, stream };
};

export const actions: Actions = {
  replay: async ({ request, locals }) => {
    const fd = await request.formData();
    const id = String(fd.get("id") ?? "");
    if (!id) return fail(400, { error: "id required" });
    const client = apiFor(locals.apiToken);
    const r = await client.POST("/api/admin/dlq/{id}/replay", { params: { path: { id } } });
    if (r.error) return fail(500, { error: JSON.stringify(r.error) });
    return { ok: true };
  },
  resolve: async ({ request, locals }) => {
    const fd = await request.formData();
    const id = String(fd.get("id") ?? "");
    const notes = String(fd.get("notes") ?? "");
    if (!id) return fail(400, { error: "id required" });
    const client = apiFor(locals.apiToken);
    const r = await client.POST("/api/admin/dlq/{id}/resolve", {
      params: { path: { id } },
      body: { notes } as any,
    });
    if (r.error) return fail(500, { error: JSON.stringify(r.error) });
    return { ok: true };
  },
};
