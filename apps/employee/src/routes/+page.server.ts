import type { Actions, PageServerLoad } from "./$types";
import { redirect, fail } from "@sveltejs/kit";
import { createApiClient } from "@tbite/api-client";
import { API_BASE_URL } from "$lib/server/env";

const PLANTS = [
  { id: "F12B-3F", label: "F12B · 3F" },
  { id: "F12B-1F", label: "F12B · 1F" },
  { id: "F15-2F", label: "F15 · 2F" },
  { id: "F18-RF", label: "F18 · RF" },
];

function buildDays(today: Date, selectedISO?: string) {
  const wk = ["日", "一", "二", "三", "四", "五", "六"];
  const labels = ["今天", "明天"];
  const out: { id: string; head: string; sub?: string }[] = [];
  for (let i = 0; i < 7; i++) {
    const d = new Date(today);
    d.setDate(today.getDate() + i);
    const m = d.getMonth() + 1;
    const day = d.getDate();
    const w = wk[d.getDay()];
    const head = labels[i] ?? `${m}/${day}(${w})`;
    const id = `${d.getFullYear()}-${String(m).padStart(2, "0")}-${String(day).padStart(2, "0")}`;
    out.push({ id, head, sub: i < 2 ? `${m}/${day}(${w})` : undefined });
  }
  // If the server-derived target_day isn't in the next 7 days, prepend it.
  if (selectedISO && !out.find((d) => d.id === selectedISO)) {
    out.unshift({ id: selectedISO, head: selectedISO });
  }
  return out;
}

export const load: PageServerLoad = async ({ locals, url }) => {
  if (!locals.user) {
    throw redirect(303, "/login?return_to=" + encodeURIComponent(url.pathname + url.search));
  }

  const selectedPlant = url.searchParams.get("plant") ?? locals.user.plant ?? PLANTS[0].id;
  const dayOverride = url.searchParams.get("day") ?? undefined;

  let home: {
    target_day: string;
    has_ordered: boolean;
    order_summary?: {
      order_id: string;
      vendor_id: string;
      status: string;
      cutoff_at: string;
      total_price_minor: number;
    };
    reorder_chips: NonNullable<unknown>[];
    favorite_chips: NonNullable<unknown>[];
    recommend_chips: NonNullable<unknown>[];
    day_menu: NonNullable<unknown>[];
  } = {
    target_day: dayOverride ?? new Date().toISOString().slice(0, 10),
    has_ordered: false,
    order_summary: undefined,
    reorder_chips: [],
    favorite_chips: [],
    recommend_chips: [],
    day_menu: [],
  };
  let error: string | undefined;

  try {
    const client = createApiClient(API_BASE_URL, locals.apiToken);
    const res = await client.GET("/api/employee/home", {
      params: { query: dayOverride ? { day: dayOverride } : {} },
    });
    if (res.data) {
      const d = res.data;
      home = {
        target_day: d.target_day,
        has_ordered: d.has_ordered,
        order_summary: d.order_summary,
        reorder_chips: (d.reorder_chips ?? []) as NonNullable<unknown>[],
        favorite_chips: (d.favorite_chips ?? []) as NonNullable<unknown>[],
        recommend_chips: (d.recommend_chips ?? []) as NonNullable<unknown>[],
        day_menu: (d.day_menu ?? []) as NonNullable<unknown>[],
      };
    } else if (res.error) {
      error = JSON.stringify(res.error);
    }
  } catch (e) {
    error = e instanceof Error ? e.message : String(e);
  }

  // Build favoriteIds set so MealCards can highlight ⭐.
  const favoriteIds = new Set(
    (home.favorite_chips as Array<{ menu_item_id: string }>).map((c) => c.menu_item_id),
  );

  const today = new Date();
  const days = buildDays(today, home.target_day);

  return {
    user: locals.user,
    plants: PLANTS,
    days,
    selectedPlant,
    selectedDay: home.target_day,
    home,
    favoriteIds: Array.from(favoriteIds),
    error,
  };
};

export const actions: Actions = {
  placeOrder: async ({ request, locals }) => {
    if (!locals.user) throw redirect(303, "/login");
    const fd = await request.formData();
    const plant = String(fd.get("plant") ?? "");
    const supplyDate = String(fd.get("supply_date") ?? "");
    const itemIDs = fd.getAll("item_id").map(String);
    const qtys = fd.getAll("qty").map((q) => parseInt(String(q), 10));
    if (itemIDs.length === 0) return fail(400, { error: "cart is empty" });

    const items = itemIDs
      .map((id, i) => ({ menu_item_id: id, qty: qtys[i] ?? 0 }))
      .filter((it) => it.qty > 0);
    if (items.length === 0) return fail(400, { error: "no items selected" });

    const client = createApiClient(API_BASE_URL, locals.apiToken);
    const r = await client.POST("/api/employee/orders", {
      body: { plant, supply_date: supplyDate, items } as never,
    });
    if (r.error) {
      // RFC 9457 problem-details — surface a calm Chinese message, not raw JSON.
      const err = r.error as { status?: number; detail?: string };
      const msg =
        err.detail === "order: cutoff time has passed"
          ? "已超過截單時間，此日已無法預訂。"
          : (err.detail ?? "送出預訂失敗，請稍後再試。");
      return fail(err.status ?? 409, { error: msg });
    }
    const orderID = (r.data as { order?: { id?: string } } | undefined)?.order?.id;
    if (!orderID) return fail(500, { error: "no order id in response" });
    throw redirect(303, `/orders/${orderID}`);
  },

  // Reorder an entire past order. May produce partial result with unavailable_items[].
  reorderPast: async ({ request, locals }) => {
    if (!locals.user) throw redirect(303, "/login");
    const fd = await request.formData();
    const sourceOrderId = String(fd.get("source_order_id") ?? "");
    const supplyDate = String(fd.get("supply_date") ?? "");
    if (!sourceOrderId || !supplyDate)
      return fail(400, { error: "source_order_id and supply_date required" });

    const client = createApiClient(API_BASE_URL, locals.apiToken);
    const r = await client.POST("/api/employee/orders/reorder", {
      body: { source_order_id: sourceOrderId, supply_date: supplyDate } as never,
    });

    if (r.error) {
      // 409 case: huma RFC 9457 problem-details; backend embeds unavailable_items in error body.
      const err = r.error as { unavailable_items?: Array<{ name: string }>; detail?: string };
      const items = err.unavailable_items ?? [];
      const names = items.map((i) => i.name).join("、");
      return fail(409, {
        error: err.detail ?? "reorder failed",
        unavailable_items: items,
        reorderToast: names ? `今日皆無供應：${names}` : (err.detail ?? "今日皆無供應"),
      });
    }

    const data = r.data as
      | { new_order_id?: string; unavailable_items?: Array<{ name: string }> | null }
      | undefined;
    const newOrderId = data?.new_order_id;
    if (!newOrderId) return fail(500, { error: "no new_order_id in response" });
    const unavailable = data?.unavailable_items ?? [];

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

  // Add a menu_item to favorites (idempotent).
  addFavorite: async ({ request, locals }) => {
    if (!locals.user) return fail(401, { error: "unauthenticated" });
    const fd = await request.formData();
    const menuItemId = String(fd.get("menu_item_id") ?? "");
    if (!menuItemId) return fail(400, { error: "menu_item_id required" });

    const client = createApiClient(API_BASE_URL, locals.apiToken);
    const r = await client.POST("/api/employee/favorites", {
      body: { menu_item_id: menuItemId } as never,
    });
    if (r.error) return fail(400, { error: JSON.stringify(r.error) });
    return { ok: true };
  },

  // Remove a favorite (idempotent — 204 even if missing).
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
    return { ok: true };
  },
};
