import type { LayoutServerLoad } from "./$types";
import { createApiClient } from "@tbite/api-client";
import { API_BASE_URL } from "$lib/server/env";
import type { ReadyOrder } from "$lib/components/TotpModal.svelte";

// Layout load — provides the user plus the data the global shell needs:
// today's `ready` orders for the 領餐碼 modal, and the active plant/day so
// the header LocationBar + cart checkout can submit a valid order.
export const load: LayoutServerLoad = async ({ locals }) => {
  if (!locals.user) {
    return { user: null, readyOrders: [] as ReadyOrder[], activeOrders: 0 };
  }

  const today = new Date().toISOString().slice(0, 10);
  let readyOrders: ReadyOrder[] = [];
  let activeOrders = 0;

  try {
    const client = createApiClient(API_BASE_URL, locals.apiToken);
    const r = await client.GET("/api/employee/orders");
    const items = (r.data as { items?: unknown[] } | undefined)?.items ?? [];
    type O = {
      id: string;
      status: string;
      supply_date: string;
      plant: string;
      total_price_minor: number;
      items: unknown[] | null;
    };
    const orders = items as O[];
    readyOrders = orders
      .filter((o) => o.status === "ready" && o.supply_date === today)
      .map((o) => ({
        id: o.id,
        supply_date: o.supply_date,
        plant: o.plant,
        total_price_minor: o.total_price_minor,
        item_count: (o.items ?? []).length,
      }));
    activeOrders = orders.filter((o) => o.status === "placed" || o.status === "cutoff").length;
  } catch {
    // Non-fatal — the shell still renders without the badge / modal list.
  }

  return { user: locals.user, readyOrders, activeOrders };
};
