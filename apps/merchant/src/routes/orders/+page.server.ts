import { redirect, fail, type Actions } from "@sveltejs/kit";
import type { PageServerLoad } from "./$types";
import { apiFor } from "$lib/server/api";
import { taipeiISO, dayId } from "$lib/date";

export const load: PageServerLoad = async ({ locals, url, depends }) => {
  if (!locals.user) throw redirect(303, "/login?return_to=" + encodeURIComponent(url.pathname));
  if (locals.user.role !== "vendor_operator") throw redirect(303, "/login");
  // SSE board events invalidate only this fragment, not the whole page.
  depends("app:orders");

  const date = url.searchParams.get("date") ?? taipeiISO();
  const client = apiFor(locals.apiToken);
  let items: any[] = [];
  try {
    const r = await client.GET("/api/merchant/orders", { params: { query: { date } as any } });
    if (r.data) items = (r.data as any).items ?? [];
  } catch {}

  // Load menu items for name lookup (include archived so old orders still resolve)
  let menuItems: any[] = [];
  try {
    const r = await client.GET("/api/merchant/menu-items", {
      params: { query: { include_archived: true } as any },
    });
    if (r.data) menuItems = (r.data as any).items ?? [];
  } catch {}
  const itemsById: Record<string, { name: string }> = Object.fromEntries(
    menuItems.map((i: any) => [i.id, { name: i.name }]),
  );

  // Group by plant
  const byPlant: Record<string, any[]> = {};
  for (const o of items) {
    (byPlant[o.plant] ??= []).push(o);
  }

  // 7-day picker (today + next 6), in Asia/Taipei.
  const days: { id: string; label: string }[] = [];
  for (let i = 0; i < 7; i++) {
    const id = dayId(i);
    const label = i === 0 ? "今天" : i === 1 ? "明天" : id.slice(5);
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
      body: { order_ids: ids } as any,
    });
    if (r.error) return fail(500, { error: JSON.stringify(r.error) });
    return { success: true, count: ids.length };
  },
};
