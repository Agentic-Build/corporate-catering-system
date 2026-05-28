import { redirect, fail, type Actions } from "@sveltejs/kit";
import type { PageServerLoad } from "./$types";
import type { components } from "@tbite/api-client";
import { apiFor } from "$lib/server/api";

type MerchantItemDTO = components["schemas"]["MerchantItemDTO"];

export const load: PageServerLoad = async ({ locals, url }) => {
  if (!locals.user) throw redirect(303, "/login?return_to=" + encodeURIComponent(url.pathname));
  const includeArchived = url.searchParams.get("archived") === "1";
  const client = apiFor(locals.apiToken);
  let items: MerchantItemDTO[] = [];
  try {
    const r = await client.GET("/api/merchant/menu-items", {
      params: { query: { include_archived: includeArchived } },
    });
    if (r.data) items = r.data.items ?? [];
  } catch {}
  return { user: locals.user, items, includeArchived };
};

export const actions: Actions = {
  copy: async ({ request, locals }) => {
    if (!locals.user) return fail(401, { error: "unauthenticated" });
    const id = String((await request.formData()).get("id") ?? "");
    if (!id) return fail(400, { error: "缺少品項 id" });
    const client = apiFor(locals.apiToken);
    const r = await client.POST("/api/merchant/menu-items/{id}/copy", {
      params: { path: { id } },
    });
    if (r.error) return fail(400, { error: "複製菜單失敗，請稍後再試。" });
    const newId = r.data?.item.id;
    throw redirect(303, newId ? `/menus/${newId}` : "/menus");
  },

  // Soft-delete (archive); hidden from the default list.
  delete: async ({ request, locals }) => {
    if (!locals.user) return fail(401, { error: "unauthenticated" });
    const id = String((await request.formData()).get("id") ?? "");
    if (!id) return fail(400, { error: "缺少品項 id" });
    const client = apiFor(locals.apiToken);
    const r = await client.POST("/api/merchant/menu-items/{id}/archive", {
      params: { path: { id } },
    });
    if (r.error) return fail(400, { error: "刪除菜單失敗，請稍後再試。" });
    throw redirect(303, "/menus");
  },
};
