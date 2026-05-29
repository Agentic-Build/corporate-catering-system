import type { Actions, PageServerLoad } from "./$types";
import { problemMessage, taipeiISO } from "@tbite/web-shared";
import { redirect, fail } from "@sveltejs/kit";
import { createApiClient } from "@tbite/api-client";
import { API_BASE_URL } from "$lib/server/env";
import { formStr } from "@tbite/web-shared";

const PAGE_LIMIT = 20;

export const load: PageServerLoad = async ({ locals, url }) => {
  if (!locals.user) {
    throw redirect(303, "/login?return_to=" + encodeURIComponent(url.pathname));
  }
  const day = url.searchParams.get("day") ?? undefined;
  const client = createApiClient(API_BASE_URL, locals.apiToken);
  const r = await client.GET("/api/employee/recommendations", {
    params: { query: { ...(day ? { day } : {}), limit: PAGE_LIMIT } },
  });
  return {
    user: locals.user,
    chips: (r.data?.chips ?? []) as unknown[],
    nextCursor: r.data?.next_cursor,
    day,
    error: r.error ? problemMessage(r.error) : undefined,
  };
};

export const actions: Actions = {
  loadMore: async ({ request, locals, url }) => {
    if (!locals.user) return fail(401, { error: "unauthenticated" });
    const fd = await request.formData();
    const cursor = Number(fd.get("cursor") ?? 0);
    const day = url.searchParams.get("day") ?? undefined;
    const client = createApiClient(API_BASE_URL, locals.apiToken);
    const r = await client.GET("/api/employee/recommendations", {
      params: { query: { ...(day ? { day } : {}), cursor, limit: PAGE_LIMIT } },
    });
    if (r.error) return fail(400, { error: problemMessage(r.error) });
    return {
      chips: (r.data?.chips ?? []) as unknown[],
      nextCursor: r.data?.next_cursor,
    };
  },
  addToCart: async ({ request, locals }) => {
    if (!locals.user) throw redirect(303, "/login");
    const fd = await request.formData();
    const menuItemId = formStr(fd, "menu_item_id");
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
      },
    });
    if (r.error) return fail(400, { error: problemMessage(r.error) });
    const orderID = r.data?.order.id;
    if (!orderID) return fail(500, { error: "no order id in response" });
    throw redirect(303, `/orders/${orderID}`);
  },
};
