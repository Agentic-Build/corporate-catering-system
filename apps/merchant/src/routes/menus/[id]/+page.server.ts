import { redirect, fail, error } from "@sveltejs/kit";
import { problemMessage, formStr } from "@tbite/web-shared";
import type { Actions, PageServerLoad } from "./$types";
import type { components, operations } from "@tbite/api-client";
import { apiFor } from "$lib/server/api";

type MerchantItemDTO = components["schemas"]["MerchantItemDTO"];
type UpdateItemBody =
  operations["updateMerchantMenuItem"]["requestBody"]["content"]["application/json"];

export const load: PageServerLoad = async ({ locals, params, url }) => {
  if (!locals.user) throw redirect(303, "/login?return_to=" + encodeURIComponent(url.pathname));
  const client = apiFor(locals.apiToken);
  const r = await client.GET("/api/merchant/menu-items", {
    params: { query: { include_archived: true } },
  });
  const items: MerchantItemDTO[] = r.data?.items ?? [];
  const item = items.find((i) => i.id === params.id);
  if (!item) throw error(404, "not found");
  return { user: locals.user, item };
};

export const actions: Actions = {
  update: async ({ request, params, locals }) => {
    const fd = await request.formData();
    let images: string[] = [];
    try {
      const parsed = JSON.parse(formStr(fd, "images", "[]"));
      if (Array.isArray(parsed)) images = parsed.filter((s) => typeof s === "string");
    } catch {
      images = [];
    }
    const body: UpdateItemBody = {
      name: formStr(fd, "name"),
      description: formStr(fd, "description"),
      price_minor: Number.parseInt(formStr(fd, "price", "0"), 10),
      tags: formStr(fd, "tags")
        .split(/\s+/)
        .map((s) => s.trim())
        .filter(Boolean),
      images,
    };
    const client = apiFor(locals.apiToken);
    const r = await client.PATCH("/api/merchant/menu-items/{id}", {
      params: { path: { id: params.id } },
      body,
    });
    if (r.error) return fail(500, { error: problemMessage(r.error) });
    throw redirect(303, "/menus");
  },
};
