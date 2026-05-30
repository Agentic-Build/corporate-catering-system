import { redirect, fail, type Actions } from "@sveltejs/kit";
import { dayId, formStr, problemMessage, taipeiISO } from "@tbite/web-shared";
import type { PageServerLoad } from "./$types";
import type { components } from "@tbite/api-client";
import { apiFor } from "$lib/server/api";

type MerchantOrderDTO = components["schemas"]["MerchantOrderDTO"];
type MerchantItemDTO = components["schemas"]["MerchantItemDTO"];

export const load: PageServerLoad = async ({ locals, url, depends }) => {
  if (!locals.user) throw redirect(303, "/login?return_to=" + encodeURIComponent(url.pathname));
  if (locals.user.role !== "vendor_operator") throw redirect(303, "/login");
  // SSE board events invalidate only this fragment, not the whole page.
  depends("app:orders");

  const date = url.searchParams.get("date") ?? taipeiISO();
  const client = apiFor(locals.apiToken);
  let items: MerchantOrderDTO[] = [];
  try {
    const r = await client.GET("/api/merchant/orders", { params: { query: { date } } });
    if (r.data) items = r.data.items ?? [];
  } catch {}

  // Menu items for name lookup; include_archived so old orders still resolve.
  let menuItems: MerchantItemDTO[] = [];
  try {
    const r = await client.GET("/api/merchant/menu-items", {
      params: { query: { include_archived: true } },
    });
    if (r.data) menuItems = r.data.items ?? [];
  } catch {}
  const itemsById: Record<string, { name: string }> = Object.fromEntries(
    menuItems.map((i) => [i.id, { name: i.name }]),
  );

  const byPlant: Record<string, MerchantOrderDTO[]> = {};
  for (const o of items) {
    const list = byPlant[o.plant];
    if (list) list.push(o);
    else byPlant[o.plant] = [o];
  }

  const days: { id: string; label: string }[] = [];
  for (let i = 0; i < 7; i++) {
    const id = dayId(i);
    let label: string;
    if (i === 0) label = "今天";
    else if (i === 1) label = "明天";
    else label = id.slice(5);
    days.push({ id, label });
  }

  return { user: locals.user, date, days, byPlant, totalCount: items.length, itemsById };
};

export const actions: Actions = {
  markReady: async ({ request, locals }) => {
    const fd = await request.formData();
    const ids = fd.getAll("order_id").map(String);
    if (ids.length === 0) return fail(400, { error: "no orders selected" });
    const client = apiFor(locals.apiToken);
    const r = await client.POST("/api/merchant/orders/mark-ready", {
      body: { order_ids: ids },
    });
    if (r.error) return fail(500, { error: problemMessage(r.error) });
    return { success: true, count: ids.length };
  },

  // Manual fallback when the camera can't scan: resolve the sticker's
  // order_number against the day's orders, then mark that one ready.
  markReadyManual: async ({ request, locals }) => {
    const fd = await request.formData();
    const code = formStr(fd, "code").trim();
    const date = formStr(fd, "date").trim();
    if (!code) return fail(400, { error: "請輸入訂單編號" });

    const client = apiFor(locals.apiToken);
    let items: MerchantOrderDTO[] = [];
    try {
      const r = await client.GET("/api/merchant/orders", { params: { query: { date } } });
      if (r.data) items = r.data.items ?? [];
    } catch {}

    const target = items.find((o) => String(o.order_number) === code);
    if (!target) return fail(404, { error: `找不到單號 #${code} 的訂單，請確認日期與單號。` });
    if (target.status === "ready" || target.status === "picked_up" || target.status === "no_show") {
      return fail(400, { error: `單號 #${code} 已出餐或已領取，無需重複操作。` });
    }
    if (target.status === "cancelled") {
      return fail(400, { error: `單號 #${code} 已取消。` });
    }

    const r = await client.POST("/api/merchant/orders/mark-ready", {
      body: { order_ids: [target.id] },
    });
    if (r.error) return fail(500, { error: problemMessage(r.error) });
    return { success: true, count: 1 };
  },
};
