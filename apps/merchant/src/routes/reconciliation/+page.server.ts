import { redirect } from "@sveltejs/kit";
import type { PageServerLoad } from "./$types";
import { apiFor } from "$lib/server/api";

/** Current month as YYYY-MM. */
function currentPeriod(): string {
  const d = new Date();
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}`;
}

export const load: PageServerLoad = async ({ locals, url }) => {
  if (!locals.user) throw redirect(303, "/login?return_to=" + encodeURIComponent(url.pathname));
  if (locals.user.role !== "vendor_operator") throw redirect(303, "/login");

  const period = url.searchParams.get("period") ?? currentPeriod();
  const client = apiFor(locals.apiToken);

  let reconciliation: any = null;
  try {
    const r = await client.GET("/api/merchant/reconciliation" as any, {
      params: { query: { period } } as any,
    });
    if ((r as any).data) reconciliation = (r as any).data;
  } catch {}

  let settlements: any[] = [];
  try {
    const r = await client.GET("/api/merchant/settlements" as any, {});
    if ((r as any).data) settlements = ((r as any).data as any).items ?? [];
  } catch {}

  return { user: locals.user, period, reconciliation, settlements };
};
