import { redirect, fail, error } from "@sveltejs/kit";
import type { Actions, PageServerLoad } from "./$types";
import { createApiClient } from "@tbite/api-client";
import { API_BASE_URL } from "$lib/server/env";
import {
  listComplaints,
  submitRating,
  submitComplaint,
  type ComplaintCategory,
} from "$lib/server/feedback";

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
  const order = (r.data as any).order;

  // Surface an already-filed complaint for this order (no per-order GET, so
  // pull the employee's complaint list and match on order_id).
  let complaint = undefined;
  if (order.status === "picked_up") {
    const cr = await listComplaints(locals.apiToken);
    if (cr.ok) complaint = (cr.data ?? []).find((c) => c.order_id === params.id);
  }

  return { user: locals.user, order, complaint };
};

export const actions: Actions = {
  cancel: async ({ locals, params }) => {
    if (!locals.user) return fail(401, { error: "unauthenticated" });
    const client = createApiClient(API_BASE_URL, locals.apiToken);
    const r = await client.POST("/api/employee/orders/{id}/cancel", {
      params: { path: { id: params.id } },
    });
    if (r.error) return fail(400, { error: JSON.stringify(r.error) });
    throw redirect(303, `/orders/${params.id}`);
  },

  // Submit a 1–5 star meal rating with an optional comment (≤ 500 chars).
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
    const r = await submitRating(locals.apiToken, params.id, score, comment);
    if (!r.ok) {
      const msg =
        r.status === 409 ? "此訂單已評分過了。" : (r.error ?? "送出評分失敗，請稍後再試。");
      return fail(r.status === 0 ? 500 : r.status, { ratingError: msg });
    }
    return { ratingOk: true, rating: r.data };
  },

  // File a meal complaint (description 5–1000 chars).
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
    const r = await submitComplaint(locals.apiToken, params.id, category, description);
    if (!r.ok) {
      const msg =
        r.status === 409 ? "此訂單已有未結案的客訴。" : (r.error ?? "送出客訴失敗，請稍後再試。");
      return fail(r.status === 0 ? 500 : r.status, { complaintError: msg });
    }
    return { complaintOk: true, complaint: r.data };
  },
};
