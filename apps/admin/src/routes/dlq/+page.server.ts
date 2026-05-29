import { redirect, fail } from "@sveltejs/kit";
import { problemMessage, formStr } from "@tbite/web-shared";
import type { Actions, PageServerLoad } from "./$types";
import type { components, operations } from "@tbite/api-client";
import { apiFor } from "$lib/server/api";

type MessageDTO = components["schemas"]["MessageDTO"];
type DLQQuery = NonNullable<operations["listDLQ"]["parameters"]["query"]>;

export const load: PageServerLoad = async ({ locals, url }) => {
  if (!locals.user) throw redirect(303, "/login?return_to=" + encodeURIComponent(url.pathname));
  if (locals.user.role !== "welfare_admin") throw redirect(303, "/login");

  const stream = url.searchParams.get("stream") ?? "";
  const client = apiFor(locals.apiToken);
  let messages: MessageDTO[] = [];
  try {
    const query: DLQQuery = { limit: 200 };
    if (stream) query.stream = stream;
    const r = await client.GET("/api/admin/dlq", { params: { query } });
    if (r.data) messages = r.data.items ?? [];
  } catch {}
  return { user: locals.user, messages, stream };
};

export const actions: Actions = {
  replay: async ({ request, locals }) => {
    const fd = await request.formData();
    const id = formStr(fd, "id");
    if (!id) return fail(400, { error: "id required" });
    const client = apiFor(locals.apiToken);
    const r = await client.POST("/api/admin/dlq/{id}/replay", { params: { path: { id } } });
    if (r.error) return fail(500, { error: problemMessage(r.error) });
    return { ok: true };
  },
  resolve: async ({ request, locals }) => {
    const fd = await request.formData();
    const id = formStr(fd, "id");
    const notes = formStr(fd, "notes");
    if (!id) return fail(400, { error: "id required" });
    const client = apiFor(locals.apiToken);
    const r = await client.POST("/api/admin/dlq/{id}/resolve", {
      params: { path: { id } },
      body: { notes },
    });
    if (r.error) return fail(500, { error: problemMessage(r.error) });
    return { ok: true };
  },
};
