import { redirect } from "@sveltejs/kit";
import type { PageServerLoad } from "./$types";
import { apiFor } from "$lib/server/api";

export const load: PageServerLoad = async ({ locals, url }) => {
  if (!locals.user) throw redirect(303, "/login?return_to=" + encodeURIComponent(url.pathname));
  const includeArchived = url.searchParams.get("archived") === "1";
  const client = apiFor(locals.apiToken);
  let items: any[] = [];
  try {
    const r = await client.GET("/api/merchant/menu-items", { params: { query: { include_archived: includeArchived } } });
    if (r.data) items = (r.data as any).items ?? [];
  } catch {}
  return { user: locals.user, items, includeArchived };
};
