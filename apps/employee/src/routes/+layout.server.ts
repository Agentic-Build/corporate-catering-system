import type { LayoutServerLoad } from "./$types";
import { createApiClient } from "@tbite/api-client";
import { API_BASE_URL } from "$lib/server/env";
import type { PlantOption } from "$lib/plants";

// Shell data: user, active-order count for sidebar badge, plants for LocationBar.
export const load: LayoutServerLoad = async ({ locals }) => {
  if (!locals.user) {
    return { user: null, activeOrders: 0, plants: [] as PlantOption[] };
  }

  const client = createApiClient(API_BASE_URL, locals.apiToken);

  let activeOrders = 0;
  let plants: PlantOption[] = [];

  const [ordersRes, plantsRes] = await Promise.allSettled([
    client.GET("/api/employee/orders"),
    client.GET("/api/plants"),
  ]);

  if (ordersRes.status === "fulfilled") {
    const items = (ordersRes.value.data as { items?: unknown[] } | undefined)?.items ?? [];
    type O = { status: string };
    const orders = items as O[];
    activeOrders = orders.filter((o) => o.status === "placed" || o.status === "cutoff").length;
  }

  if (plantsRes.status === "fulfilled") {
    const items =
      (plantsRes.value.data as { items?: Array<{ code: string; label: string }> } | undefined)
        ?.items ?? [];
    plants = items.map((p) => ({ id: p.code, label: p.label }));
  }

  return { user: locals.user, activeOrders, plants };
};
