import { redirect } from "@sveltejs/kit";
import type { components } from "@tbite/api-client";
import type { PageServerLoad } from "./$types";
import { apiFor } from "$lib/server/api";

type ReconciliationDTO = components["schemas"]["ReconciliationDTO"];
type SettlementDTO = components["schemas"]["SettlementDTO"];

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

  let reconciliation: ReconciliationDTO | null = null;
  try {
    const r = await client.GET("/api/merchant/reconciliation", {
      params: { query: { period } },
    });
    if (r.data) reconciliation = r.data.reconciliation ?? null;
  } catch {}

  let settlements: SettlementDTO[] = [];
  try {
    const r = await client.GET("/api/merchant/settlements", {});
    if (r.data) settlements = r.data.items ?? [];
  } catch {}

  return { user: locals.user, period, reconciliation, settlements };
};
