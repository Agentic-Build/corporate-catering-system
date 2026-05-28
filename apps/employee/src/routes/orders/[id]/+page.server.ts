import { redirect, fail, error } from "@sveltejs/kit";
import { problemMessage } from "@tbite/web-shared";
import type { Actions, PageServerLoad } from "./$types";
import { createApiClient, type components } from "@tbite/api-client";
import { API_BASE_URL } from "$lib/server/env";

type ComplaintCategory = components["schemas"]["FileComplaintInputBody"]["category"];

const COMPLAINT_CATEGORIES: ComplaintCategory[] = [
  "wrong_item",
  "missing_item",
  "quality",
  "portion",
  "hygiene",
  "other",
];

export const load: PageServerLoad = async ({ locals, params, url }) => {
  if (!locals.user) {
    throw redirect(303, "/login?return_to=" + encodeURIComponent(url.pathname));
  }
  const client = createApiClient(API_BASE_URL, locals.apiToken);
  const r = await client.GET("/api/employee/orders/{id}", {
    params: { path: { id: params.id } },
  });
  if (r.error || !r.data) throw error(404, "order not found");
  const order = r.data.order;

  // No per-order complaint GET; match by order_id in the list.
  let complaint = undefined;
  // 404 from rating endpoint = not rated yet.
  let rating = undefined;
  if (order.status === "picked_up") {
    const cr = await client.GET("/api/employee/complaints");
    if (cr.data) complaint = (cr.data.items ?? []).find((c) => c.order_id === params.id);
    const rr = await client.GET("/api/employee/orders/{id}/rating", {
      params: { path: { id: params.id } },
    });
    if (rr.data) rating = rr.data.rating;
  }

  let menu = undefined;
  if (order.status === "placed") {
    const mr = await client.GET("/api/employee/menu", {
      params: { query: { plant: order.plant, day: order.supply_date } },
    });
    if (mr.data) menu = (mr.data.items ?? []).filter((m) => m.vendor_id === order.vendor_id);
  }

  return { user: locals.user, order, complaint, rating, menu };
};

export const actions: Actions = {
  cancel: async ({ locals, params }) => {
    if (!locals.user) return fail(401, { error: "unauthenticated" });
    const client = createApiClient(API_BASE_URL, locals.apiToken);
    const r = await client.POST("/api/employee/orders/{id}/cancel", {
      params: { path: { id: params.id } },
    });
    if (r.error) return fail(400, { error: problemMessage(r.error) });
    throw redirect(303, `/orders/${params.id}`);
  },

  // qty 0 entries are dropped; empty list rejected (must use cancel instead).
  modify: async ({ request, locals, params }) => {
    if (!locals.user) return fail(401, { modifyError: "unauthenticated" });
    const fd = await request.formData();
    let parsed: unknown;
    try {
      parsed = JSON.parse(String(fd.get("items") ?? "[]"));
    } catch {
      return fail(400, { modifyError: "資料格式錯誤，請重新操作。" });
    }
    const items = (Array.isArray(parsed) ? parsed : [])
      .filter(
        (it): it is { menu_item_id: string; qty: number } =>
          !!it && typeof it.menu_item_id === "string" && Number.isInteger(it.qty) && it.qty > 0,
      )
      .map((it) => ({ menu_item_id: it.menu_item_id, qty: it.qty }));
    if (items.length === 0) {
      return fail(400, { modifyError: "訂單至少需保留一個餐點；若要清空請改用取消訂單。" });
    }
    const notes = String(fd.get("notes") ?? "").trim();
    if (notes.length > 500) return fail(400, { modifyError: "備註不可超過 500 字" });
    const client = createApiClient(API_BASE_URL, locals.apiToken);
    const r = await client.PUT("/api/employee/orders/{id}", {
      params: { path: { id: params.id } },
      body: { items, notes },
    });
    if (r.error) {
      const status = r.response.status;
      const msg =
        status === 409
          ? (r.error.detail ?? "餐點數量超過剩餘供應量，或已過截單時間。")
          : (r.error.detail ?? "修改訂單失敗，請稍後再試。");
      return fail(status, { modifyError: msg });
    }
    throw redirect(303, `/orders/${params.id}`);
  },

  rate: async ({ request, locals, params }) => {
    if (!locals.user) return fail(401, { ratingError: "unauthenticated" });
    const fd = await request.formData();
    const score = parseInt(String(fd.get("score") ?? ""), 10);
    const comment = String(fd.get("comment") ?? "").trim();
    if (!Number.isInteger(score) || score < 1 || score > 5) {
      return fail(400, { ratingError: "請選擇 1 至 5 顆星的評分" });
    }
    if (comment.length > 500) {
      return fail(400, { ratingError: "留言不可超過 500 字" });
    }
    const client = createApiClient(API_BASE_URL, locals.apiToken);
    const r = await client.POST("/api/employee/orders/{id}/rating", {
      params: { path: { id: params.id } },
      body: { score, comment },
    });
    if (r.error) {
      const status = r.response.status;
      const msg =
        status === 409 ? "此訂單已評分過了。" : (r.error.detail ?? "送出評分失敗，請稍後再試。");
      return fail(status, { ratingError: msg });
    }
    return { ratingOk: true, rating: r.data.rating };
  },

  complain: async ({ request, locals, params }) => {
    if (!locals.user) return fail(401, { complaintError: "unauthenticated" });
    const fd = await request.formData();
    const category = String(fd.get("category") ?? "") as ComplaintCategory;
    const description = String(fd.get("description") ?? "").trim();
    if (!COMPLAINT_CATEGORIES.includes(category)) {
      return fail(400, { complaintError: "請選擇問題類型" });
    }
    if (description.length < 5 || description.length > 1000) {
      return fail(400, { complaintError: "問題描述需介於 5 至 1000 字" });
    }
    const client = createApiClient(API_BASE_URL, locals.apiToken);
    const r = await client.POST("/api/employee/orders/{id}/complaint", {
      params: { path: { id: params.id } },
      body: { category, description },
    });
    if (r.error) {
      const status = r.response.status;
      const msg =
        status === 409
          ? "此訂單已有未結案的客訴。"
          : (r.error.detail ?? "送出客訴失敗，請稍後再試。");
      return fail(status, { complaintError: msg });
    }
    return { complaintOk: true, complaint: r.data.complaint };
  },
};
