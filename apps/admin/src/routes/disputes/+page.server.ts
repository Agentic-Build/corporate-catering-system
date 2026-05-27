import { redirect, fail } from "@sveltejs/kit";
import type { Actions, PageServerLoad } from "./$types";
import { apiFor } from "$lib/server/api";

export const load: PageServerLoad = async ({ locals, url }) => {
  if (!locals.user) throw redirect(303, "/login?return_to=" + encodeURIComponent(url.pathname));
  if (locals.user.role !== "welfare_admin") throw redirect(303, "/login");

  const status = url.searchParams.get("status") ?? "";
  const client = apiFor(locals.apiToken);
  let disputes: any[] = [];
  try {
    const r = await client.GET("/api/admin/payroll/disputes", {
      params: { query: (status ? { status } : {}) as any },
    });
    if (r.data) disputes = (r.data as any).items ?? [];
  } catch {}
  return { user: locals.user, disputes, status };
};

export const actions: Actions = {
  resolveRefund: async ({ request, locals }) => {
    const fd = await request.formData();
    const disputeId = String(fd.get("dispute_id") ?? "");
    const resolution = String(fd.get("resolution") ?? "").trim();
    const refundMinor = Number(fd.get("refund_minor") ?? 0);
    if (!disputeId) return fail(400, { error: "dispute_id required" });
    if (!Number.isFinite(refundMinor) || refundMinor < 0) {
      return fail(400, { error: "refund_minor must be >= 0" });
    }
    const client = apiFor(locals.apiToken);
    const r = await client.POST("/api/admin/payroll/disputes/{id}/resolve", {
      params: { path: { id: disputeId } },
      body: {
        status: "resolved_refund",
        resolution,
        refund_minor: refundMinor,
      } as any,
    });
    if (r.error) return fail(500, { error: JSON.stringify(r.error) });
    return { ok: true };
  },
  resolveReject: async ({ request, locals }) => {
    const fd = await request.formData();
    const disputeId = String(fd.get("dispute_id") ?? "");
    const resolution = String(fd.get("resolution") ?? "").trim();
    if (!disputeId) return fail(400, { error: "dispute_id required" });
    if (!resolution) return fail(400, { error: "resolution required" });
    const client = apiFor(locals.apiToken);
    const r = await client.POST("/api/admin/payroll/disputes/{id}/resolve", {
      params: { path: { id: disputeId } },
      body: {
        status: "resolved_reject",
        resolution,
        refund_minor: 0,
      } as any,
    });
    if (r.error) return fail(500, { error: JSON.stringify(r.error) });
    return { ok: true };
  },
};
