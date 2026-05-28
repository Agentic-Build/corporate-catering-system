import { redirect, fail } from "@sveltejs/kit";
import type { Actions, PageServerLoad } from "./$types";
import { createApiClient } from "@tbite/api-client";
import { API_BASE_URL } from "$lib/server/env";

export const load: PageServerLoad = async ({ locals, url }) => {
  if (!locals.user) {
    throw redirect(303, "/login?return_to=" + encodeURIComponent(url.pathname));
  }
  const client = createApiClient(API_BASE_URL, locals.apiToken);
  const r = await client.GET("/api/employee/complaints");
  return {
    user: locals.user,
    complaints: r.data ? (r.data.items ?? []) : [],
    error: r.error ? (r.error.detail ?? "載入客訴失敗") : undefined,
  };
};

export const actions: Actions = {
  // Backend enforces a 24h gate; too-early returns 4xx (surfaced as a message).
  escalate: async ({ request, locals }) => {
    if (!locals.user) return fail(401, { error: "unauthenticated" });
    const fd = await request.formData();
    const id = String(fd.get("id") ?? "");
    if (!id) return fail(400, { error: "complaint id required" });
    const client = createApiClient(API_BASE_URL, locals.apiToken);
    const r = await client.POST("/api/employee/complaints/{id}/escalate", {
      params: { path: { id } },
    });
    if (r.error) {
      const status = r.response.status;
      const msg =
        status === 409
          ? "尚未滿 24 小時或狀態不允許升級。"
          : (r.error.detail ?? "升級失敗，請稍後再試。");
      return fail(status, { error: msg });
    }
    return { ok: true };
  },

  resolve: async ({ request, locals }) => {
    if (!locals.user) return fail(401, { error: "unauthenticated" });
    const fd = await request.formData();
    const id = String(fd.get("id") ?? "");
    if (!id) return fail(400, { error: "complaint id required" });
    const client = createApiClient(API_BASE_URL, locals.apiToken);
    const r = await client.POST("/api/employee/complaints/{id}/resolve", {
      params: { path: { id } },
    });
    if (r.error) {
      return fail(r.response.status, {
        error: r.error.detail ?? "結案失敗，請稍後再試。",
      });
    }
    return { ok: true };
  },
};
