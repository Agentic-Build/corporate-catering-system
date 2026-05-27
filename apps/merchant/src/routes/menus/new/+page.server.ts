import { redirect, fail } from "@sveltejs/kit";
import type { Actions, PageServerLoad } from "./$types";
import { apiFor } from "$lib/server/api";

export const load: PageServerLoad = async ({ locals, url }) => {
  if (!locals.user) throw redirect(303, "/login?return_to=" + encodeURIComponent(url.pathname));
  return { user: locals.user };
};

export const actions: Actions = {
  default: async ({ request, locals }) => {
    const fd = await request.formData();
    const name = String(fd.get("name") ?? "").trim();
    const description = String(fd.get("description") ?? "").trim();
    const priceStr = String(fd.get("price") ?? "0").trim();
    const tagsStr = String(fd.get("tags") ?? "").trim();
    if (!name) return fail(400, { error: "name 必填" });
    const priceMinor = parseInt(priceStr, 10);
    if (!Number.isFinite(priceMinor) || priceMinor < 0) return fail(400, { error: "price 非數字" });

    let images: string[] = [];
    try {
      const parsed = JSON.parse(String(fd.get("images") ?? "[]"));
      if (Array.isArray(parsed)) images = parsed.filter((s) => typeof s === "string");
    } catch {
      images = [];
    }

    const client = apiFor(locals.apiToken);
    const r = await client.POST("/api/merchant/menu-items", {
      body: {
        name,
        description,
        price_minor: priceMinor,
        tags: tagsStr
          ? tagsStr
              .split(/\s+/)
              .map((s) => s.trim())
              .filter(Boolean)
          : [],
        images,
      } as any,
    });
    if (r.error) return fail(500, { error: JSON.stringify(r.error) });
    throw redirect(303, "/menus");
  },
};
