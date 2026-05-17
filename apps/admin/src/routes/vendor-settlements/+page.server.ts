import { redirect, fail } from "@sveltejs/kit";
import type { components } from "@tbite/api-client";
import type { Actions, PageServerLoad } from "./$types";
import { apiFor } from "$lib/server/api";

type Settlement = components["schemas"]["SettlementDTO"];

/** Current month as YYYY-MM. */
function currentMonth(): string {
  const d = new Date();
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}`;
}

/** First / last calendar day (YYYY-MM-DD) of a YYYY-MM period. */
function monthBounds(period: string): { start: string; end: string } | null {
  const m = /^(\d{4})-(\d{2})$/.exec(period);
  if (!m) return null;
  const year = Number(m[1]);
  const month = Number(m[2]);
  if (month < 1 || month > 12) return null;
  const last = new Date(year, month, 0).getDate();
  return {
    start: `${m[1]}-${m[2]}-01`,
    end: `${m[1]}-${m[2]}-${String(last).padStart(2, "0")}`,
  };
}

export const load: PageServerLoad = async ({ locals, url }) => {
  if (!locals.user) throw redirect(303, "/login?return_to=" + encodeURIComponent(url.pathname));
  if (locals.user.role !== "welfare_admin") throw redirect(303, "/login");

  const period = url.searchParams.get("period") || currentMonth();

  const client = apiFor(locals.apiToken);
  let settlements: Settlement[] = [];
  try {
    const r = await client.GET("/api/admin/vendor-settlements", {
      params: { query: { period } },
    });
    if (r.data) settlements = r.data.items ?? [];
  } catch {}

  return { user: locals.user, period, settlements };
};

export const actions: Actions = {
  // Close the selected period: cut one settlement per vendor with orders.
  close: async ({ request, locals }) => {
    const fd = await request.formData();
    const period = String(fd.get("period") ?? "").trim();
    const bounds = monthBounds(period);
    if (!bounds) return fail(400, { error: "期間格式錯誤（YYYY-MM）" });

    const client = apiFor(locals.apiToken);
    const r = await client.POST("/api/admin/vendor-settlements/close", {
      body: { period_start: bounds.start, period_end: bounds.end },
    });
    if (r.error) return fail(500, { error: JSON.stringify(r.error) });
    throw redirect(303, `/vendor-settlements?period=${period}`);
  },

  // Void a closed settlement so the period can be re-closed.
  voidSettlement: async ({ request, locals, url }) => {
    const fd = await request.formData();
    const id = String(fd.get("id") ?? "");
    if (!id) return fail(400, { error: "缺少結算單編號" });

    const client = apiFor(locals.apiToken);
    const r = await client.POST("/api/admin/vendor-settlements/{id}/void", {
      params: { path: { id } },
    });
    if (r.error) return fail(500, { error: JSON.stringify(r.error) });
    const period = url.searchParams.get("period") ?? "";
    throw redirect(303, period ? `/vendor-settlements?period=${period}` : "/vendor-settlements");
  },
};
