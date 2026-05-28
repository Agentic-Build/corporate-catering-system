import { redirect, fail } from "@sveltejs/kit";
import { problemMessage } from "@tbite/web-shared";
import type { Actions, PageServerLoad } from "./$types";
import type { components } from "@tbite/api-client";
import { apiFor } from "$lib/server/api";
import { defaultCutoffAt } from "$lib/cutoff";
import { dayId } from "$lib/date";

type MerchantItemDTO = components["schemas"]["MerchantItemDTO"];
type MerchantOrderDTO = components["schemas"]["MerchantOrderDTO"];
type SupplyDTO = components["schemas"]["SupplyDTO"];

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
    return { id, head, weekday: WEEKDAY[d.getDay()] ?? "", offset: i };
  });

  // Library = all menu items incl. archived (drawer + name lookups).
  let items: MerchantItemDTO[] = [];
  try {
    const r = await client.GET("/api/merchant/menu-items", {
      params: { query: { include_archived: true } },
    });
    if (r.data) items = r.data.items ?? [];
  } catch {}
  let todayOrders: MerchantOrderDTO[] = [];
  try {
    const r = await client.GET("/api/merchant/orders", {
      params: { query: { date: today } },
    });
    if (r.data) todayOrders = r.data.items ?? [];
  } catch {}

  const supplyResults = await Promise.all(
    days.map(async (d) => {
      try {
        const r = await client.GET("/api/merchant/supply", {
          params: { query: { date: d.id } },
        });
        return { date: d.id, items: (r.data?.items ?? []) as SupplyDTO[] };
      } catch {
        return { date: d.id, items: [] as SupplyDTO[] };
      }
    }),
  );
  const supplyByDate: Record<string, SupplyDTO[]> = Object.fromEntries(
    supplyResults.map((s) => [s.date, s.items]),
  );

  const todaySupply: SupplyDTO[] = supplyByDate[today] ?? [];
  const totalCapacity = todaySupply.reduce((a, s) => a + s.capacity, 0);
  const totalSold = todaySupply.reduce((a, s) => a + (s.capacity - s.remain), 0);
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
    const cutoffAt = String(fd.get("cutoff_at") || defaultCutoffAt(date));

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
      },
    });
    if (r.error) return fail(500, { error: problemMessage(r.error) });
    return { success: true };
  },

  /** Toggle a supply's temporary sold-out flag for the given day. */
  toggleSoldOut: async ({ request, locals }) => {
    const fd = await request.formData();
    const itemId = String(fd.get("item_id") ?? "");
    const date = String(fd.get("date") ?? "");
    const soldOut = String(fd.get("sold_out") ?? "") === "true";
    if (!itemId || !date) return fail(400, { error: "缺少餐點或日期" });
    const client = apiFor(locals.apiToken);
    const r = await client.POST("/api/merchant/supply/{itemID}/{date}/sold-out", {
      params: { path: { itemID: itemId, date } },
      body: { sold_out: soldOut },
    });
    if (r.error) return fail(500, { error: problemMessage(r.error) });
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
    if (r.error) return fail(500, { error: problemMessage(r.error) });
    return { success: true };
  },
};
