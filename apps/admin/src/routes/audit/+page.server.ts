import { redirect } from "@sveltejs/kit";
import type { PageServerLoad } from "./$types";
import type { components, operations } from "@tbite/api-client";
import { apiFor } from "$lib/server/api";

type AuditRowDTO = components["schemas"]["AuditRowDTO"];
type AuditQuery = NonNullable<operations["listAuditEvents"]["parameters"]["query"]>;

export const load: PageServerLoad = async ({ locals, url }) => {
  if (!locals.user) throw redirect(303, "/login?return_to=" + encodeURIComponent(url.pathname));
  if (locals.user.role !== "welfare_admin") throw redirect(303, "/login");

  const target_kind = url.searchParams.get("target_kind") ?? "";
  const target_id = url.searchParams.get("target_id") ?? "";
  const since = url.searchParams.get("since") ?? "";
  const limit = Number(url.searchParams.get("limit") ?? "100");

  const client = apiFor(locals.apiToken);
  let events: AuditRowDTO[] = [];
  try {
    const query: AuditQuery = { limit };
    if (target_kind) query.target_kind = target_kind;
    if (target_id) query.target_id = target_id;
    if (since) query.since = since;
    const r = await client.GET("/api/admin/audit", { params: { query } });
    if (r.data) events = r.data.items ?? [];
  } catch {}

  return { user: locals.user, events, target_kind, target_id, since, limit };
};
