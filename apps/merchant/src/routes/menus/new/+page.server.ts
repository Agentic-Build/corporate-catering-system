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
    const badgesStr = String(fd.get("badges") ?? "").trim();
    if (!name) return fail(400, { error: "name 必填" });
    const priceMinor = parseInt(priceStr, 10);
    if (!Number.isFinite(priceMinor) || priceMinor < 0) return fail(400, { error: "price 非數字" });

    const client = apiFor(locals.apiToken);
    const r = await client.POST("/api/merchant/menu-items", {
      body: {
        name,
        description,
        price_minor: priceMinor,
        tags: tagsStr
          ? tagsStr
              .split(",")
              .map((s) => s.trim())
              .filter(Boolean)
          : [],
        badges: badgesStr
          ? badgesStr
              .split(",")
              .map((s) => s.trim())
              .filter(Boolean)
          : [],
      } as any,
    });
    if (r.error) return fail(500, { error: JSON.stringify(r.error) });
    throw redirect(303, "/menus");
  },
};
