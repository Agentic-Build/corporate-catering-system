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

async function pickup(client: ReturnType<typeof createApiClient>, id: string) {
  const r = await client.POST("/api/employee/orders/{id}/pickup", {
    params: { path: { id } },
  });
  return r.error ? JSON.stringify(r.error) : null;
}

export const actions: Actions = {
  // Camera scan → full order id parsed from the QR. Mark it picked up directly.
  scan: async ({ request, locals }) => {
    if (!locals.user) return fail(401, { error: "尚未登入" });
    const fd = await request.formData();
    const id = String(fd.get("orderId") ?? "").trim();
    if (!id) return fail(400, { error: "未取得訂單編號" });

    const client = createApiClient(API_BASE_URL, locals.apiToken);
    const err = await pickup(client, id);
    if (err)
      return fail(400, { error: "核銷失敗，請確認這是您本人的餐點且尚未領取。", orderId: id });
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
      status: string;
    }[];

    const matches = items.filter(
      (o) =>
        o.status === "ready" &&
        (o.id.toLowerCase() === code || o.id.slice(0, 8).toLowerCase() === code),
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

    const err = await pickup(client, matches[0].id);
    if (err) return fail(400, { error: "核銷失敗，請稍後再試。", manual: true });
    return { ok: true, pickedUpId: matches[0].id };
  },
};
