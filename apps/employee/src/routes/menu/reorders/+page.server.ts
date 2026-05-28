import type { Actions, PageServerLoad } from "./$types";
import { problemMessage } from "@tbite/web-shared";
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
  const r = await client.GET("/api/employee/reorders", {
    params: { query: { limit: PAGE_LIMIT } },
  });
  // Pull target_day so chip form submit knows the supply_date.
  const h = await client.GET("/api/employee/home", { params: { query: {} } });
  const targetDay = h.data?.target_day ?? taipeiISO();

  return {
    user: locals.user,
    chips: (r.data?.chips ?? []) as unknown[],
    nextCursor: r.data?.next_cursor,
    targetDay,
    error: r.error ? problemMessage(r.error) : undefined,
  };
};

export const actions: Actions = {
  loadMore: async ({ request, locals }) => {
    if (!locals.user) return fail(401, { error: "unauthenticated" });
    const fd = await request.formData();
    const cursor = Number(fd.get("cursor") ?? 0);
    const client = createApiClient(API_BASE_URL, locals.apiToken);
    const r = await client.GET("/api/employee/reorders", {
      params: { query: { cursor, limit: PAGE_LIMIT } },
    });
    if (r.error) return fail(400, { error: problemMessage(r.error) });
    return {
      chips: (r.data?.chips ?? []) as unknown[],
      nextCursor: r.data?.next_cursor,
    };
  },
  reorderPast: async ({ request, locals }) => {
    if (!locals.user) throw redirect(303, "/login");
    const fd = await request.formData();
    const sourceOrderId = String(fd.get("source_order_id") ?? "");
    const supplyDate = String(fd.get("supply_date") ?? "");
    if (!sourceOrderId || !supplyDate)
      return fail(400, { error: "source_order_id and supply_date required" });
    const client = createApiClient(API_BASE_URL, locals.apiToken);
    const r = await client.POST("/api/employee/orders/reorder", {
      body: { source_order_id: sourceOrderId, supply_date: supplyDate },
    });
    if (r.error) {
      const err = r.error as { unavailable_items?: Array<{ name: string }>; detail?: string };
      const items = err.unavailable_items ?? [];
      const names = items.map((i) => i.name).join("、");
      return fail(409, {
        error: err.detail ?? "reorder failed",
        reorderToast: names ? `今日皆無供應：${names}` : (err.detail ?? "今日皆無供應"),
      });
    }
    const newOrderId = r.data?.new_order_id;
    if (!newOrderId) return fail(500, { error: "no new_order_id in response" });
    const unavailable = r.data?.unavailable_items ?? [];
    if (unavailable.length > 0) {
      const names = unavailable.map((i) => i.name).join("、");
      const qs = new URLSearchParams({
        reorder: "partial",
        order_id: newOrderId,
        unavailable: names,
        unavailable_count: String(unavailable.length),
      });
      throw redirect(303, `/orders/${newOrderId}?${qs.toString()}`);
    }
    throw redirect(303, `/orders/${newOrderId}`);
  },
};
