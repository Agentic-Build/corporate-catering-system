import type { Actions, PageServerLoad } from "./$types";
import { buildDays, problemMessage, taipeiISO } from "@tbite/web-shared";
import { redirect, fail } from "@sveltejs/kit";
import { createApiClient, type operations } from "@tbite/api-client";
import { API_BASE_URL } from "$lib/server/env";
import { formStr } from "@tbite/web-shared";

type MenuQuery = NonNullable<operations["listEmployeeMenu"]["parameters"]["query"]>;
type MenuSort = NonNullable<MenuQuery["sort"]>;

type MenuFilter = {
  q: string;
  tags: string[];
  priceMin: number;
  priceMax: number;
  inStock: boolean;
  sort: MenuSort | "";
};

type HomePayload = {
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
};

const MENU_SORTS = new Set(["name", "price_asc", "price_desc", "remain"]);

function parseMenuFilter(sp: URLSearchParams): MenuFilter {
  const sortParam = sp.get("sort") ?? "";
  return {
    q: sp.get("q")?.trim() ?? "",
    tags: sp.getAll("tags").filter(Boolean),
    priceMin: Number(sp.get("price_min") ?? "") || 0,
    priceMax: Number(sp.get("price_max") ?? "") || 0,
    inStock: sp.get("in_stock") === "1",
    sort: (MENU_SORTS.has(sortParam) ? sortParam : "") as MenuSort | "",
  };
}

function isFilterActive(f: MenuFilter): boolean {
  return (
    f.q !== "" ||
    f.tags.length > 0 ||
    f.priceMin > 0 ||
    f.priceMax > 0 ||
    f.inStock ||
    f.sort !== ""
  );
}

function emptyHome(targetDay: string): HomePayload {
  return {
    target_day: targetDay,
    has_ordered: false,
    order_summary: undefined,
    reorder_chips: [],
    favorite_chips: [],
    recommend_chips: [],
    day_menu: [],
  };
}

async function fetchHome(
  token: string | undefined,
  dayOverride: string | undefined,
  fallbackDay: string,
): Promise<{ home: HomePayload; error?: string }> {
  try {
    const client = createApiClient(API_BASE_URL, token);
    const res = await client.GET("/api/employee/home", {
      params: { query: dayOverride ? { day: dayOverride } : {} },
    });
    if (res.data) {
      const d = res.data;
      return {
        home: {
          target_day: d.target_day,
          has_ordered: d.has_ordered,
          order_summary: d.order_summary,
          reorder_chips: (d.reorder_chips ?? []) as NonNullable<unknown>[],
          favorite_chips: (d.favorite_chips ?? []) as NonNullable<unknown>[],
          recommend_chips: (d.recommend_chips ?? []) as NonNullable<unknown>[],
          day_menu: (d.day_menu ?? []) as NonNullable<unknown>[],
        },
      };
    }
    if (res.error) {
      return { home: emptyHome(fallbackDay), error: problemMessage(res.error) };
    }
    return { home: emptyHome(fallbackDay) };
  } catch (e) {
    return { home: emptyHome(fallbackDay), error: e instanceof Error ? e.message : String(e) };
  }
}

function buildMenuQuery(plant: string, day: string, f: MenuFilter): MenuQuery {
  const query: MenuQuery = { plant, day };
  if (f.q) query.q = f.q;
  if (f.tags.length > 0) query.tags = f.tags;
  if (f.priceMin > 0) query.price_min = f.priceMin;
  if (f.priceMax > 0) query.price_max = f.priceMax;
  if (f.inStock) query.in_stock = true;
  if (f.sort) query.sort = f.sort;
  return query;
}

async function fetchFilteredMenu(
  token: string | undefined,
  plant: string,
  day: string,
  filter: MenuFilter,
): Promise<{ items?: NonNullable<unknown>[]; error?: string }> {
  try {
    const client = createApiClient(API_BASE_URL, token);
    const mr = await client.GET("/api/employee/menu", {
      params: { query: buildMenuQuery(plant, day, filter) },
    });
    if (mr.data) {
      return { items: (mr.data.items ?? []) as NonNullable<unknown>[] };
    }
    if (mr.error) {
      return { error: problemMessage(mr.error) };
    }
    return {};
  } catch (e) {
    return { error: e instanceof Error ? e.message : String(e) };
  }
}

function collectTags(...lists: Array<NonNullable<unknown>[] | undefined>): string[] {
  const pool = new Set<string>();
  for (const list of lists) {
    for (const m of (list ?? []) as Array<{ tags?: string[] | null }>) {
      for (const t of m.tags ?? []) pool.add(t);
    }
  }
  return Array.from(pool).sort((a, b) => a.localeCompare(b));
}

export const load: PageServerLoad = async ({ locals, url, parent, depends }) => {
  if (!locals.user) {
    throw redirect(303, "/login?return_to=" + encodeURIComponent(url.pathname + url.search));
  }
  depends("app:home");

  const { plants } = await parent();
  const selectedPlant = url.searchParams.get("plant") ?? locals.user.plant ?? plants[0]?.id ?? "";
  const dayOverride = url.searchParams.get("day") ?? undefined;
  const menuFilter = parseMenuFilter(url.searchParams);
  const filterActive = isFilterActive(menuFilter);

  const { home, error: homeError } = await fetchHome(
    locals.apiToken,
    dayOverride,
    dayOverride ?? taipeiISO(),
  );
  let error = homeError;

  let filteredMenu: NonNullable<unknown>[] | undefined;
  if (filterActive) {
    const r = await fetchFilteredMenu(locals.apiToken, selectedPlant, home.target_day, menuFilter);
    filteredMenu = r.items;
    if (r.error && !error) error = r.error;
  }

  const favoriteIds = new Set(
    (home.favorite_chips as Array<{ menu_item_id: string }>).map((c) => c.menu_item_id),
  );

  return {
    user: locals.user,
    plants,
    days: buildDays(new Date(), home.target_day),
    selectedPlant,
    selectedDay: home.target_day,
    home,
    favoriteIds: Array.from(favoriteIds),
    menuFilter,
    filterActive,
    filteredMenu,
    tagPool: collectTags(home.day_menu, filteredMenu),
    error,
  };
};

export const actions: Actions = {
  placeOrder: async ({ request, locals }) => {
    if (!locals.user) throw redirect(303, "/login");
    const fd = await request.formData();
    const plant = formStr(fd, "plant");
    const supplyDate = formStr(fd, "supply_date");
    const notes = formStr(fd, "notes").trim();
    const itemIDs = fd.getAll("item_id").map(String);
    const qtys = fd.getAll("qty").map((q) => Number.parseInt(String(q), 10));
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

  // May produce partial result with unavailable_items[].
  reorderPast: async ({ request, locals }) => {
    if (!locals.user) throw redirect(303, "/login");
    const fd = await request.formData();
    const sourceOrderId = formStr(fd, "source_order_id");
    const supplyDate = formStr(fd, "supply_date");
    if (!sourceOrderId || !supplyDate)
      return fail(400, { error: "source_order_id and supply_date required" });

    const client = createApiClient(API_BASE_URL, locals.apiToken);
    const r = await client.POST("/api/employee/orders/reorder", {
      body: { source_order_id: sourceOrderId, supply_date: supplyDate },
    });

    if (r.error) {
      // Backend embeds unavailable_items in the problem-details error body.
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

  addFavorite: async ({ request, locals }) => {
    if (!locals.user) return fail(401, { error: "unauthenticated" });
    const fd = await request.formData();
    const menuItemId = formStr(fd, "menu_item_id");
    if (!menuItemId) return fail(400, { error: "menu_item_id required" });

    const client = createApiClient(API_BASE_URL, locals.apiToken);
    const r = await client.POST("/api/employee/favorites", {
      body: { menu_item_id: menuItemId },
    });
    if (r.error) return fail(400, { error: problemMessage(r.error) });
    return { ok: true };
  },

  removeFavorite: async ({ request, locals }) => {
    if (!locals.user) return fail(401, { error: "unauthenticated" });
    const fd = await request.formData();
    const menuItemId = formStr(fd, "menu_item_id");
    if (!menuItemId) return fail(400, { error: "menu_item_id required" });

    const client = createApiClient(API_BASE_URL, locals.apiToken);
    const r = await client.DELETE("/api/employee/favorites/{menu_item_id}", {
      params: { path: { menu_item_id: menuItemId } },
    });
    if (r.error) return fail(400, { error: problemMessage(r.error) });
    return { ok: true };
  },
};
