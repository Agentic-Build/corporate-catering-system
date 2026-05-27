import { redirect, fail } from "@sveltejs/kit";
import type { Actions, PageServerLoad } from "./$types";
import { createApiClient } from "@tbite/api-client";
import { API_BASE_URL } from "$lib/server/env";

export const load: PageServerLoad = async ({ locals, url }) => {
  if (!locals.user) {
    throw redirect(303, "/login?return_to=" + encodeURIComponent(url.pathname));
  }
  return { user: locals.user };
};

async function pickup(
  client: ReturnType<typeof createApiClient>,
  id: string,
): Promise<number | null> {
  const r = await client.POST("/api/employee/orders/{id}/pickup", {
    params: { path: { id } },
  });
  if (!r.error) return null;
  return r.response?.status ?? 0;
}

function pickupError(status: number): string {
  switch (status) {
    case 403:
      return "這不是您本人的訂單，無法核銷。";
    case 404:
      return "找不到這筆訂單。";
    case 409:
      return "此訂單目前無法取餐：可能供應日尚未到、商家尚未備餐完成，或已領取過。";
    default:
      return "核銷失敗，請稍後再試。";
  }
}

export const actions: Actions = {
  // Camera scan → full order id parsed from the QR. Mark it picked up directly.
  scan: async ({ request, locals }) => {
    if (!locals.user) return fail(401, { error: "尚未登入" });
    const fd = await request.formData();
    const id = String(fd.get("orderId") ?? "").trim();
    if (!id) return fail(400, { error: "未取得訂單編號" });

    const client = createApiClient(API_BASE_URL, locals.apiToken);
    const status = await pickup(client, id);
    if (status !== null) return fail(400, { error: pickupError(status), orderId: id });
    return { ok: true, pickedUpId: id };
  },

  // Manual fallback → order-number prefix (first 8 chars printed on the sticker).
  // Resolve against the employee's own `ready` orders before marking pickup.
  manual: async ({ request, locals }) => {
    if (!locals.user) return fail(401, { error: "尚未登入" });
    const fd = await request.formData();
    const code = String(fd.get("code") ?? "")
      .trim()
      .toLowerCase();
    if (!code) return fail(400, { error: "請輸入訂單編號", manual: true });

    const client = createApiClient(API_BASE_URL, locals.apiToken);
    const listRes = await client.GET("/api/employee/orders");
    const items = ((listRes.data as { items?: unknown[] } | undefined)?.items ?? []) as {
      id: string;
      order_number: number;
      status: string;
    }[];

    const matches = items.filter(
      (o) =>
        o.status === "ready" &&
        (String(o.order_number) === code ||
          o.id.toLowerCase() === code ||
          o.id.slice(0, 8).toLowerCase() === code),
    );
    if (matches.length === 0) {
      return fail(404, { error: "找不到符合的待領訂單，請確認編號或改用相機掃描。", manual: true });
    }
    if (matches.length > 1) {
      return fail(400, {
        error: "有多筆訂單符合此編號，請輸入完整訂單編號或改用相機掃描。",
        manual: true,
      });
    }

    const status = await pickup(client, matches[0].id);
    if (status !== null) return fail(400, { error: pickupError(status), manual: true });
    return { ok: true, pickedUpId: matches[0].id };
  },
};
