import { redirect, fail } from "@sveltejs/kit";
import type { Actions, PageServerLoad } from "./$types";
import { createApiClient, type components } from "@tbite/api-client";
import { API_BASE_URL } from "$lib/server/env";

type EmployeeEntry = components["schemas"]["EmployeeEntryDTO"];
type CurrentPayrollLine = components["schemas"]["CurrentPayrollLineDTO"];
type ComplaintCategory = components["schemas"]["FileComplaintInputBody"]["category"];

const COMPLAINT_CATEGORIES: ComplaintCategory[] = [
  "wrong_item",
  "missing_item",
  "quality",
  "portion",
  "hygiene",
  "other",
];

export const load: PageServerLoad = async ({ locals, url }) => {
  if (!locals.user) {
    throw redirect(303, "/login?return_to=" + encodeURIComponent(url.pathname));
  }
  const client = createApiClient(API_BASE_URL, locals.apiToken);

  let entries: EmployeeEntry[] = [];
  const r = await client.GET("/api/employee/payroll");
  if (r.data) entries = r.data.items ?? [];

  // Unsettled period: per-order lines + live total for the hero + rows.
  let currentLines: CurrentPayrollLine[] = [];
  let currentTotalMinor = 0;
  const cr = await client.GET("/api/employee/payroll/current");
  if (cr.data) {
    currentLines = cr.data.lines ?? [];
    currentTotalMinor = cr.data.total_minor;
  }

  return { user: locals.user, entries, currentLines, currentTotalMinor };
};

export const actions: Actions = {
  // Mirror of orders/[id] ?/rate so PayrollEntrySheet can rate inline.
  rate: async ({ request, locals }) => {
    if (!locals.user) return fail(401, { ratingError: "unauthenticated" });
    const fd = await request.formData();
    const orderId = String(fd.get("order_id") ?? "").trim();
    const score = parseInt(String(fd.get("score") ?? ""), 10);
    const comment = String(fd.get("comment") ?? "").trim();
    if (!orderId) return fail(400, { ratingError: "缺少訂單資訊" });
    if (!Number.isInteger(score) || score < 1 || score > 5) {
      return fail(400, { ratingError: "請選擇 1 至 5 顆星的評分" });
    }
    if (comment.length > 500) {
      return fail(400, { ratingError: "留言不可超過 500 字" });
    }
    const client = createApiClient(API_BASE_URL, locals.apiToken);
    const r = await client.POST("/api/employee/orders/{id}/rating", {
      params: { path: { id: orderId } },
      body: { score, comment },
    });
    if (r.error) {
      const status = r.response.status;
      const msg =
        status === 409 ? "此訂單已評分過了。" : (r.error.detail ?? "送出評分失敗，請稍後再試。");
      return fail(status, { ratingError: msg });
    }
    return { ratingOk: true, orderId, rating: r.data.rating };
  },

  // Mirror of orders/[id] ?/complain.
  complain: async ({ request, locals }) => {
    if (!locals.user) return fail(401, { complaintError: "unauthenticated" });
    const fd = await request.formData();
    const orderId = String(fd.get("order_id") ?? "").trim();
    const category = String(fd.get("category") ?? "") as ComplaintCategory;
    const description = String(fd.get("description") ?? "").trim();
    if (!orderId) return fail(400, { complaintError: "缺少訂單資訊" });
    if (!COMPLAINT_CATEGORIES.includes(category)) {
      return fail(400, { complaintError: "請選擇問題類型" });
    }
    if (description.length < 5 || description.length > 1000) {
      return fail(400, { complaintError: "問題描述需介於 5 至 1000 字" });
    }
    const client = createApiClient(API_BASE_URL, locals.apiToken);
    const r = await client.POST("/api/employee/orders/{id}/complaint", {
      params: { path: { id: orderId } },
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
    return { complaintOk: true, orderId, complaint: r.data.complaint };
  },
};
