import type { Actions, PageServerLoad } from "./$types";
import { redirect, fail } from "@sveltejs/kit";
import { createApiClient } from "@tbite/api-client";
import { API_BASE_URL } from "$lib/server/env";
import { taipeiISO } from "$lib/date";

const PAGE_LIMIT = 20;

export const load: PageServerLoad = async ({ locals, url }) => {
  if (!locals.user) {
    throw redirect(303, "/login?return_to=" + encodeURIComponent(url.pathname));
  }
  const client = createApiClient(API_BASE_URL, locals.apiToken);
  // The favorites endpoint requires `day` (it computes per-day availability).
  const day = taipeiISO();
  const r = await client.GET("/api/employee/favorites", {
    params: { query: { day, limit: PAGE_LIMIT } },
  });
  return {
    user: locals.user,
    chips: (r.data?.chips ?? []) as unknown[],
    nextCursor: r.data?.next_cursor,
    error: r.error ? JSON.stringify(r.error) : undefined,
  };
};

export const actions: Actions = {
  loadMore: async ({ request, locals }) => {
    if (!locals.user) return fail(401, { error: "unauthenticated" });
    const fd = await request.formData();
    const cursor = String(fd.get("cursor") ?? "");
    const client = createApiClient(API_BASE_URL, locals.apiToken);
    const day = taipeiISO();
    const r = await client.GET("/api/employee/favorites", {
      params: { query: { day, cursor, limit: PAGE_LIMIT } },
    });
    if (r.error) return fail(400, { error: JSON.stringify(r.error) });
    return {
      chips: (r.data?.chips ?? []) as unknown[],
      nextCursor: r.data?.next_cursor,
    };
  },
  removeFavorite: async ({ request, locals }) => {
    if (!locals.user) return fail(401, { error: "unauthenticated" });
    const fd = await request.formData();
    const menuItemId = String(fd.get("menu_item_id") ?? "");
    if (!menuItemId) return fail(400, { error: "menu_item_id required" });
    const client = createApiClient(API_BASE_URL, locals.apiToken);
    const r = await client.DELETE("/api/employee/favorites/{menu_item_id}", {
      params: { path: { menu_item_id: menuItemId } },
    });
    if (r.error) return fail(400, { error: JSON.stringify(r.error) });
    return { ok: true, removed: menuItemId };
  },
  addToCart: async ({ request, locals }) => {
    if (!locals.user) throw redirect(303, "/login");
    const fd = await request.formData();
    const menuItemId = String(fd.get("menu_item_id") ?? "");
    if (!menuItemId) return fail(400, { error: "menu_item_id required" });
    const client = createApiClient(API_BASE_URL, locals.apiToken);
    const h = await client.GET("/api/employee/home", { params: { query: {} } });
    const supplyDate = h.data?.target_day ?? taipeiISO();
    const plant = locals.user.plant ?? "tn-a";
    const r = await client.POST("/api/employee/orders", {
      body: {
        plant,
        supply_date: supplyDate,
        items: [{ menu_item_id: menuItemId, qty: 1 }],
      } as never,
    });
    if (r.error) return fail(400, { error: JSON.stringify(r.error) });
    const orderID = (r.data as { order?: { id?: string } } | undefined)?.order?.id;
    if (!orderID) return fail(500, { error: "no order id in response" });
    throw redirect(303, `/orders/${orderID}`);
  },
};
