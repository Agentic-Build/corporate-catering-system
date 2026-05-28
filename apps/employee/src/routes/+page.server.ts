import type { Actions, PageServerLoad } from "./$types";
import { problemMessage } from "@tbite/web-shared";
import { redirect, fail } from "@sveltejs/kit";
import { createApiClient, type operations } from "@tbite/api-client";
import { API_BASE_URL } from "$lib/server/env";
import { buildDays, taipeiISO } from "@tbite/web-shared";

type MenuQuery = NonNullable<operations["listEmployeeMenu"]["parameters"]["query"]>;
type MenuSort = NonNullable<MenuQuery["sort"]>;

// F3: when any filter-bar key is set in the URL we fetch the filtered grid
// from /api/employee/menu; otherwise the home payload's day_menu is used.
const MENU_SORTS = new Set(["name", "price_asc", "price_desc", "remain"]);

export const load: PageServerLoad = async ({ locals, url, parent, depends }) => {
  if (!locals.user) {
    throw redirect(303, "/login?return_to=" + encodeURIComponent(url.pathname + url.search));
  }
  // SSE menu "changed" events invalidate only this fragment, not the whole page.
  depends("app:home");

  const { plants } = await parent();
  const selectedPlant = url.searchParams.get("plant") ?? locals.user.plant ?? plants[0]?.id ?? "";
  const dayOverride = url.searchParams.get("day") ?? undefined;

  // F3 menu filter parsed from the URL query.
  const sp = url.searchParams;
  const sortParam = sp.get("sort") ?? "";
  const menuFilter = {
    q: sp.get("q")?.trim() ?? "",
    tags: sp.getAll("tags").filter(Boolean),
    priceMin: Number(sp.get("price_min") ?? "") || 0,
    priceMax: Number(sp.get("price_max") ?? "") || 0,
    inStock: sp.get("in_stock") === "1",
    sort: (MENU_SORTS.has(sortParam) ? sortParam : "") as MenuSort | "",
  };
  const filterActive =
    menuFilter.q !== "" ||
    menuFilter.tags.length > 0 ||
    menuFilter.priceMin > 0 ||
    menuFilter.priceMax > 0 ||
    menuFilter.inStock ||
    menuFilter.sort !== "";

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
    target_day: dayOverride ?? taipeiISO(),
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
      error = problemMessage(res.error);
    }
  } catch (e) {
    error = e instanceof Error ? e.message : String(e);
  }

  // When filter bar is active, fetch filtered grid in place of day_menu.
  let filteredMenu: NonNullable<unknown>[] | undefined;
  if (filterActive) {
    try {
      const client = createApiClient(API_BASE_URL, locals.apiToken);
      const query: MenuQuery = {
        plant: selectedPlant,
        day: home.target_day,
      };
      if (menuFilter.q) query.q = menuFilter.q;
      if (menuFilter.tags.length > 0) query.tags = menuFilter.tags;
      if (menuFilter.priceMin > 0) query.price_min = menuFilter.priceMin;
      if (menuFilter.priceMax > 0) query.price_max = menuFilter.priceMax;
      if (menuFilter.inStock) query.in_stock = true;
      if (menuFilter.sort) query.sort = menuFilter.sort;
      const mr = await client.GET("/api/employee/menu", {
        params: { query },
      });
      if (mr.data) {
        filteredMenu = (mr.data.items ?? []) as NonNullable<unknown>[];
      } else if (mr.error && !error) {
        error = problemMessage(mr.error);
      }
    } catch (e) {
      if (!error) error = e instanceof Error ? e.message : String(e);
    }
  }

  const favoriteIds = new Set(
    (home.favorite_chips as Array<{ menu_item_id: string }>).map((c) => c.menu_item_id),
  );

  const today = new Date();
  const days = buildDays(today, home.target_day);

  // Distinct tag universe for filter chips, across the day's menu.
  const tagPool = new Set<string>();
  for (const m of home.day_menu as Array<{ tags?: string[] | null }>) {
    for (const t of m.tags ?? []) tagPool.add(t);
  }
  for (const m of (filteredMenu ?? []) as Array<{ tags?: string[] | null }>) {
    for (const t of m.tags ?? []) tagPool.add(t);
  }

  return {
    user: locals.user,
    plants,
    days,
    selectedPlant,
    selectedDay: home.target_day,
    home,
    favoriteIds: Array.from(favoriteIds),
    menuFilter,
    filterActive,
    filteredMenu,
    tagPool: Array.from(tagPool).sort(),
    error,
  };
};

export const actions: Actions = {
  placeOrder: async ({ request, locals }) => {
    if (!locals.user) throw redirect(303, "/login");
    const fd = await request.formData();
    const plant = String(fd.get("plant") ?? "");
    const supplyDate = String(fd.get("supply_date") ?? "");
    const notes = String(fd.get("notes") ?? "").trim();
    const itemIDs = fd.getAll("item_id").map(String);
    const qtys = fd.getAll("qty").map((q) => parseInt(String(q), 10));
    if (itemIDs.length === 0) return fail(400, { error: "cart is empty" });
    if (notes.length > 500) return fail(400, { error: "備註不可超過 500 字" });

    const items = itemIDs
      .map((id, i) => ({ menu_item_id: id, qty: qtys[i] ?? 0 }))
      .filter((it) => it.qty > 0);
    if (items.length === 0) return fail(400, { error: "no items selected" });

    const client = createApiClient(API_BASE_URL, locals.apiToken);
    const r = await client.POST("/api/employee/orders", {
      body: { plant, supply_date: supplyDate, notes, items },
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
    const orderID = r.data?.order.id;
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
      body: { source_order_id: sourceOrderId, supply_date: supplyDate },
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

  // Add a menu_item to favorites (idempotent).
  addFavorite: async ({ request, locals }) => {
    if (!locals.user) return fail(401, { error: "unauthenticated" });
    const fd = await request.formData();
    const menuItemId = String(fd.get("menu_item_id") ?? "");
    if (!menuItemId) return fail(400, { error: "menu_item_id required" });

    const client = createApiClient(API_BASE_URL, locals.apiToken);
    const r = await client.POST("/api/employee/favorites", {
      body: { menu_item_id: menuItemId },
    });
    if (r.error) return fail(400, { error: problemMessage(r.error) });
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
    if (r.error) return fail(400, { error: problemMessage(r.error) });
    return { ok: true };
  },
};
