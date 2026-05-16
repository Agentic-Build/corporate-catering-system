import { redirect, fail } from "@sveltejs/kit";
import type { Actions, PageServerLoad } from "./$types";
import { apiFor } from "$lib/server/api";

/** Local YYYY-MM-DD for a Date offset by `addDays` from today. */
function dayId(addDays: number): string {
  const d = new Date();
  d.setDate(d.getDate() + addDays);
  const y = d.getFullYear();
  const m = String(d.getMonth() + 1).padStart(2, "0");
  const day = String(d.getDate()).padStart(2, "0");
  return `${y}-${m}-${day}`;
}

const WEEKDAY = ["週日", "週一", "週二", "週三", "週四", "週五", "週六"];

export const load: PageServerLoad = async ({ locals, url }) => {
  if (!locals.user) throw redirect(303, "/login?return_to=" + encodeURIComponent(url.pathname));
  if (locals.user.role !== "vendor_operator") throw redirect(303, "/login");

  const client = apiFor(locals.apiToken);
  const today = dayId(0);

  // 7-day window for the schedule planner.
  const days = Array.from({ length: 7 }, (_, i) => {
    const id = dayId(i);
    const d = new Date(id + "T00:00:00");
    const head = i === 0 ? "今天" : i === 1 ? "明天" : `${id.slice(5).replace("-", "/")}`;
    return { id, head, weekday: WEEKDAY[d.getDay()], offset: i };
  });

  // Meal library — all menu items incl. archived (drawer + name lookups).
  let items: any[] = [];
  try {
    const r = await client.GET("/api/merchant/menu-items", {
      params: { query: { include_archived: true } },
    });
    if (r.data) items = (r.data as any).items ?? [];
  } catch {}
  const itemById: Record<string, any> = Object.fromEntries(items.map((i) => [i.id, i]));

  // Today's orders, grouped by plant for the prep aggregation cards.
  let todayOrders: any[] = [];
  try {
    const r = await client.GET("/api/merchant/orders", {
      params: { query: { date: today } as any },
    });
    if (r.data) todayOrders = (r.data as any).items ?? [];
  } catch {}

  const plantMap: Record<string, { qty: Record<string, number>; orderCount: number }> = {};
  for (const o of todayOrders) {
    const p = (plantMap[o.plant] ??= { qty: {}, orderCount: 0 });
    p.orderCount += 1;
    for (const li of o.items ?? []) {
      p.qty[li.menu_item_id] = (p.qty[li.menu_item_id] ?? 0) + li.qty;
    }
  }
  const plants = Object.entries(plantMap)
    .map(([plant, agg]) => {
      const lineItems = Object.entries(agg.qty)
        .map(([id, qty]) => ({ name: itemById[id]?.name ?? "未知餐點", qty }))
        .sort((a, b) => b.qty - a.qty);
      const total = lineItems.reduce((s, x) => s + x.qty, 0);
      return { plant, total, items: lineItems, orderCount: agg.orderCount };
    })
    .sort((a, b) => b.total - a.total);

  // 7-day supply, fetched in parallel — one request per day.
  const supplyResults = await Promise.all(
    days.map(async (d) => {
      try {
        const r = await client.GET("/api/merchant/supply", {
          params: { query: { date: d.id } },
        });
        return { date: d.id, items: r.data ? ((r.data as any).items ?? []) : [] };
      } catch {
        return { date: d.id, items: [] as any[] };
      }
    }),
  );
  const supplyByDate: Record<string, any[]> = Object.fromEntries(
    supplyResults.map((s) => [s.date, s.items]),
  );

  // Today's totals for the StatCard row.
  const todaySupply: any[] = supplyByDate[today] ?? [];
  const totalCapacity = todaySupply.reduce((a: number, s: any) => a + s.capacity, 0);
  const totalSold = todaySupply.reduce((a: number, s: any) => a + (s.capacity - s.remain), 0);
  const todayOrderCount = todayOrders.length;
  const pickedUp = todayOrders.filter((o) => o.status === "picked_up").length;
  const pendingPrep = todayOrders.filter(
    (o) => o.status === "placed" || o.status === "cutoff",
  ).length;
  const revenue = todayOrders
    .filter((o) => o.status !== "cancelled")
    .reduce((a, o) => a + (o.total_price_minor ?? 0), 0);

  return {
    user: locals.user,
    today,
    days,
    plants,
    items,
    supplyByDate,
    stats: { totalCapacity, totalSold, todayOrderCount, pickedUp, pendingPrep, revenue },
  };
};

export const actions: Actions = {
  /** Set or update a day's capacity for one menu item. */
  setSupply: async ({ request, locals }) => {
    const fd = await request.formData();
    const itemId = String(fd.get("item_id") ?? "");
    const date = String(fd.get("date") ?? "");
    const capacity = parseInt(String(fd.get("capacity") ?? "0"), 10);
    const pickupWindow = String(fd.get("pickup_window") ?? "11:50-12:10");
    const cutoffAt = String(fd.get("cutoff_at") ?? `${date}T17:00:00Z`);

    if (!itemId || !date) return fail(400, { error: "缺少餐點或日期" });
    if (!Number.isFinite(capacity) || capacity < 0) return fail(400, { error: "上限數值無效" });

    const client = apiFor(locals.apiToken);
    const r = await client.PUT("/api/merchant/supply/{itemID}/{date}", {
      params: { path: { itemID: itemId, date } },
      body: {
        capacity,
        pickup_window: pickupWindow,
        eta_label: pickupWindow,
        cutoff_at: cutoffAt,
      } as any,
    });
    if (r.error) return fail(500, { error: JSON.stringify(r.error) });
    return { success: true };
  },

  /** Publish a menu item — used before adding an archived item to a day. */
  publishItem: async ({ request, locals }) => {
    const fd = await request.formData();
    const id = String(fd.get("item_id") ?? "");
    if (!id) return fail(400, { error: "缺少餐點" });
    const client = apiFor(locals.apiToken);
    const r = await client.POST("/api/merchant/menu-items/{id}/publish", {
      params: { path: { id } },
    });
    if (r.error) return fail(500, { error: JSON.stringify(r.error) });
    return { success: true };
  },
};
