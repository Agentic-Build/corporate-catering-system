import { redirect, fail } from "@sveltejs/kit";
import type { Actions, PageServerLoad } from "./$types";
import { listComplaints, escalateComplaint, resolveComplaint } from "$lib/server/feedback";

export const load: PageServerLoad = async ({ locals, url }) => {
  if (!locals.user) {
    throw redirect(303, "/login?return_to=" + encodeURIComponent(url.pathname));
  }
  const r = await listComplaints(locals.apiToken);
  return {
    user: locals.user,
    complaints: r.ok ? (r.data ?? []) : [],
    error: r.ok ? undefined : r.error,
  };
};

export const actions: Actions = {
  // Escalate to the welfare committee. The backend enforces the 24h gate;
  // a too-early call comes back as a 4xx which is surfaced as a message.
  escalate: async ({ request, locals }) => {
    if (!locals.user) return fail(401, { error: "unauthenticated" });
    const fd = await request.formData();
    const id = String(fd.get("id") ?? "");
    if (!id) return fail(400, { error: "complaint id required" });
    const r = await escalateComplaint(locals.apiToken, id);
    if (!r.ok) {
      const msg =
        r.status === 409
          ? "尚未滿 24 小時或狀態不允許升級。"
          : (r.error ?? "升級失敗，請稍後再試。");
      return fail(r.status === 0 ? 500 : r.status, { error: msg });
    }
    return { ok: true };
  },

  // Employee marks the complaint resolved (satisfied).
  resolve: async ({ request, locals }) => {
    if (!locals.user) return fail(401, { error: "unauthenticated" });
    const fd = await request.formData();
    const id = String(fd.get("id") ?? "");
    if (!id) return fail(400, { error: "complaint id required" });
    const r = await resolveComplaint(locals.apiToken, id);
    if (!r.ok) {
      return fail(r.status === 0 ? 500 : r.status, {
        error: r.error ?? "結案失敗，請稍後再試。",
      });
    }
    return { ok: true };
  },
};
