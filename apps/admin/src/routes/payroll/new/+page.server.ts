import { redirect, fail } from "@sveltejs/kit";
import type { Actions, PageServerLoad } from "./$types";
import { apiFor } from "$lib/server/api";

function firstOfMonth(d: Date): string {
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-01`;
}
function lastOfMonth(d: Date): string {
  const last = new Date(d.getFullYear(), d.getMonth() + 1, 0);
  return `${last.getFullYear()}-${String(last.getMonth() + 1).padStart(2, "0")}-${String(last.getDate()).padStart(2, "0")}`;
}

export const load: PageServerLoad = async ({ locals, url }) => {
  if (!locals.user) throw redirect(303, "/login?return_to=" + encodeURIComponent(url.pathname));
  if (locals.user.role !== "welfare_admin") throw redirect(303, "/login");

  const now = new Date();
  return {
    user: locals.user,
    defaultStart: firstOfMonth(now),
    defaultEnd: lastOfMonth(now),
  };
};

export const actions: Actions = {
  default: async ({ request, locals }) => {
    const fd = await request.formData();
    const periodStart = String(fd.get("period_start") ?? "").trim();
    const periodEnd = String(fd.get("period_end") ?? "").trim();
    if (!periodStart || !periodEnd)
      return fail(400, { error: "period_start and period_end required" });
    const client = apiFor(locals.apiToken);
    const r = await client.POST("/api/admin/payroll/batches", {
      body: { period_start: periodStart, period_end: periodEnd } as any,
    });
    if (r.error) return fail(500, { error: JSON.stringify(r.error) });
    const id = (r.data as any)?.batch?.id;
    if (!id) return fail(500, { error: "no batch id in response" });
    throw redirect(303, `/payroll/${id}`);
  },
};
