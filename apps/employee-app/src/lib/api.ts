// Typed data layer for the employee app. Wraps `@tbite/api-client`
// (openapi-fetch) with the session Bearer token and exposes thin
// per-screen helpers. Every call hits the real `/api/employee/*` API.

import { createApiClient, type components } from "@tbite/api-client";
import { API_BASE_URL } from "./config";
import { session } from "./session.svelte";

/** Build a client carrying the current session token. */
function client() {
  return createApiClient(API_BASE_URL, session.token ?? undefined);
}

// ── Re-exported DTO types the screens consume ──────────────────────────
export type MenuItem = components["schemas"]["EmployeeMenuItemDTO"];
export type HomePayload = components["schemas"]["HomeOutputBody"];
export type Order = components["schemas"]["OrderDTO"];
export type OrderItem = components["schemas"]["OrderItemDTO"];
export type PayrollLine = components["schemas"]["CurrentPayrollLineDTO"];
export type FavoriteChip = components["schemas"]["FavoriteChipDTO"];

export interface CurrentPayroll {
  total_minor: number;
  lines: PayrollLine[];
}

export interface PickupCode {
  code: string;
  order_id: string;
  expires_in_seconds: number;
}

/** A vendor card aggregated client-side from the day's flat menu list. */
export interface VendorGroup {
  vendor_id: string;
  vendor: string;
  eta_label: string;
  items: MenuItem[];
}

// ── Home / menu ─────────────────────────────────────────────────────────

/** The day's menu, plus favorites, for the home screen. */
export async function getHome(day?: string): Promise<HomePayload> {
  const res = await client().GET("/api/employee/home", {
    params: { query: day ? { day } : {} },
  });
  if (res.error) throw new Error(problem(res.error));
  return res.data;
}

/** Filtered menu grid (keyword / price / sort / in-stock). */
export async function getMenu(query: {
  plant?: string;
  day?: string;
  q?: string;
  tags?: string[];
  price_min?: number;
  price_max?: number;
  in_stock?: boolean;
  sort?: "name" | "price_asc" | "price_desc" | "remain";
}): Promise<MenuItem[]> {
  const res = await client().GET("/api/employee/menu", { params: { query } });
  if (res.error) throw new Error(problem(res.error));
  return res.data.items ?? [];
}

/** Group a flat menu list into vendor cards (mockup's vendor-first browse). */
export function groupByVendor(items: MenuItem[]): VendorGroup[] {
  const map = new Map<string, VendorGroup>();
  for (const it of items) {
    let g = map.get(it.vendor_id);
    if (!g) {
      g = { vendor_id: it.vendor_id, vendor: it.vendor, eta_label: it.eta_label, items: [] };
      map.set(it.vendor_id, g);
    }
    g.items.push(it);
  }
  return [...map.values()];
}

// ── Orders ──────────────────────────────────────────────────────────────

export async function listOrders(): Promise<Order[]> {
  const res = await client().GET("/api/employee/orders");
  if (res.error) throw new Error(problem(res.error));
  return res.data.items ?? [];
}

export async function getOrder(id: string): Promise<Order> {
  const res = await client().GET("/api/employee/orders/{id}", {
    params: { path: { id } },
  });
  if (res.error) throw new Error(problem(res.error));
  return res.data.order;
}

export interface PlaceOrderInput {
  plant: string;
  supply_date: string;
  notes: string;
  items: { menu_item_id: string; qty: number }[];
}

export async function placeOrder(input: PlaceOrderInput): Promise<string> {
  const res = await client().POST("/api/employee/orders", {
    body: input as never,
  });
  if (res.error) throw new Error(problem(res.error));
  const id = (res.data as { order?: { id?: string } })?.order?.id;
  if (!id) throw new Error("送出預訂失敗:回應缺少訂單編號");
  return id;
}

// ── Pickup code (TOTP) ──────────────────────────────────────────────────

export async function getPickupCode(orderId: string): Promise<PickupCode> {
  const res = await client().GET("/api/employee/orders/{id}/pickup-code", {
    params: { path: { id: orderId } },
  });
  if (res.error) throw new Error(problem(res.error));
  return res.data;
}

// ── Payroll ─────────────────────────────────────────────────────────────

/** Current (open) period: live running total + per-order lines (B2). */
export async function getCurrentPayroll(): Promise<CurrentPayroll> {
  const res = await client().GET("/api/employee/payroll/current");
  if (res.error) throw new Error(problem(res.error));
  return { total_minor: res.data.total_minor, lines: res.data.lines ?? [] };
}

// ── Ratings & complaints ────────────────────────────────────────────────

export async function rateOrder(
  orderId: string,
  stars: number,
  tags: string[],
  comment: string,
): Promise<void> {
  const res = await client().POST("/api/employee/orders/{id}/rating", {
    params: { path: { id: orderId } },
    body: { stars, tags, comment } as never,
  });
  if (res.error) throw new Error(problem(res.error));
}

export async function fileComplaint(
  orderId: string,
  tags: string[],
  description: string,
): Promise<void> {
  const res = await client().POST("/api/employee/orders/{id}/complaint", {
    params: { path: { id: orderId } },
    body: { tags, description } as never,
  });
  if (res.error) throw new Error(problem(res.error));
}

// ── Favorites ───────────────────────────────────────────────────────────

export async function addFavorite(menuItemId: string): Promise<void> {
  const res = await client().POST("/api/employee/favorites", {
    body: { menu_item_id: menuItemId } as never,
  });
  if (res.error) throw new Error(problem(res.error));
}

export async function removeFavorite(menuItemId: string): Promise<void> {
  const res = await client().DELETE("/api/employee/favorites/{menu_item_id}", {
    params: { path: { menu_item_id: menuItemId } },
  });
  if (res.error) throw new Error(problem(res.error));
}

// ── Helpers ─────────────────────────────────────────────────────────────

/** Flatten an RFC 9457 problem-details body into a readable message. */
function problem(err: unknown): string {
  if (err && typeof err === "object" && "detail" in err) {
    return String((err as { detail?: unknown }).detail ?? "請求失敗");
  }
  return typeof err === "string" ? err : "請求失敗,請稍後再試。";
}
