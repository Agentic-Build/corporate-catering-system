import { redirect, fail, error } from "@sveltejs/kit";
import type { Actions, PageServerLoad } from "./$types";
import { createApiClient } from "@tbite/api-client";
import { API_BASE_URL } from "$lib/server/env";

const DISPUTABLE = new Set(["ready", "picked_up", "no_show"]);

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
  return { user: locals.user, order, disputable: DISPUTABLE.has(order.status) };
};

export const actions: Actions = {
  default: async ({ request, locals, params }) => {
    if (!locals.user) return fail(401, { error: "unauthenticated" });
    const fd = await request.formData();
    const reason = String(fd.get("reason") ?? "").trim();
    if (!reason) return fail(400, { error: "請填寫申訴原因" });
    const client = createApiClient(API_BASE_URL, locals.apiToken);
    const r = await client.POST("/api/employee/disputes", {
      body: { order_id: params.id, reason } as any,
    });
    if (r.error) {
      // RFC 9457 problem-details — surface a calm Chinese message, never the raw
      // backend string. A 404 here means the order has no payroll entry yet
      // (current, not-yet-settled period); disputes open once it's settled.
      const err = r.error as { status?: number; detail?: string };
      const message =
        err.status === 404
          ? "此訂單尚未產生薪資代扣紀錄，將於結算後開放申訴。"
          : "送出申訴失敗，請稍後再試。";
      return fail(err.status ?? 400, { error: message });
    }
    throw redirect(303, "/disputes");
  },
};
