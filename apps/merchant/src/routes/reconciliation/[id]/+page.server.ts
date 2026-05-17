import { redirect, error } from "@sveltejs/kit";
import type { components } from "@tbite/api-client";
import type { PageServerLoad } from "./$types";
import { apiFor } from "$lib/server/api";

type SettlementDTO = components["schemas"]["SettlementDTO"];
type OrderLineDTO = components["schemas"]["OrderLineDTO"];

export const load: PageServerLoad = async ({ locals, params, url }) => {
  if (!locals.user) throw redirect(303, "/login?return_to=" + encodeURIComponent(url.pathname));
  if (locals.user.role !== "vendor_operator") throw redirect(303, "/login");

  const client = apiFor(locals.apiToken);
  // The handler returns { "settlement": {...}, "orders": [...] }.
  let settlement: SettlementDTO | null = null;
  let orders: OrderLineDTO[] = [];
  try {
    const r = await client.GET("/api/merchant/settlements/{id}", {
      params: { path: { id: params.id } },
    });
    if (r.error || !r.data) throw error(404, "找不到對帳單");
    settlement = r.data.settlement ?? null;
    orders = r.data.orders ?? [];
  } catch (e: unknown) {
    if (e && typeof e === "object" && "status" in e) throw e;
    throw error(404, "找不到對帳單");
  }

  return { user: locals.user, settlement, orders };
};
