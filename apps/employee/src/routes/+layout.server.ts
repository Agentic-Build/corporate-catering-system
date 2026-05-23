import type { LayoutServerLoad } from "./$types";
import { createApiClient } from "@tbite/api-client";
import { API_BASE_URL } from "$lib/server/env";

// Layout load — provides the user plus the data the global shell needs:
// the active plant/day so the header LocationBar + cart checkout can submit a
// valid order, and the count of in-progress orders for the sidebar badge.
export const load: LayoutServerLoad = async ({ locals }) => {
  if (!locals.user) {
    return { user: null, activeOrders: 0 };
  }

  let activeOrders = 0;

  try {
    const client = createApiClient(API_BASE_URL, locals.apiToken);
    const r = await client.GET("/api/employee/orders");
    const items = (r.data as { items?: unknown[] } | undefined)?.items ?? [];
    type O = { status: string };
    const orders = items as O[];
    activeOrders = orders.filter((o) => o.status === "placed" || o.status === "cutoff").length;
  } catch {
    // Non-fatal — the shell still renders without the badge.
  }

  return { user: locals.user, activeOrders };
};
