import { redirect, error } from "@sveltejs/kit";
import type { PageServerLoad } from "./$types";
import { apiFor } from "$lib/server/api";

export const load: PageServerLoad = async ({ locals, params, url }) => {
  if (!locals.user) throw redirect(303, "/login?return_to=" + encodeURIComponent(url.pathname));
  if (locals.user.role !== "vendor_operator") throw redirect(303, "/login");

  const client = apiFor(locals.apiToken);
  // The handler returns { "settlement": {...}, "orders": [...] }.
  let settlement: any = null;
  let orders: any[] = [];
  try {
    const r = await client.GET("/api/merchant/settlements/{id}" as any, {
      params: { path: { id: params.id } } as any,
    });
    if ((r as any).error) throw error(404, "找不到對帳單");
    const body = (r as any).data ?? {};
    settlement = body.settlement ?? null;
    orders = body.orders ?? [];
  } catch (e: any) {
    if (e?.status) throw e;
    throw error(404, "找不到對帳單");
  }

  return { user: locals.user, settlement, orders };
};
