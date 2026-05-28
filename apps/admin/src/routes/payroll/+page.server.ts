import { redirect } from "@sveltejs/kit";
import type { PageServerLoad } from "./$types";
import type { components, operations } from "@tbite/api-client";
import { apiFor } from "$lib/server/api";

type BatchDTO = components["schemas"]["BatchDTO"];
type BatchStatus = NonNullable<operations["listPayrollBatches"]["parameters"]["query"]>["status"];

export const load: PageServerLoad = async ({ locals, url }) => {
  if (!locals.user) throw redirect(303, "/login?return_to=" + encodeURIComponent(url.pathname));
  if (locals.user.role !== "welfare_admin") throw redirect(303, "/login");

  const status = (url.searchParams.get("status") ?? "") as BatchStatus;
  const client = apiFor(locals.apiToken);
  let batches: BatchDTO[] = [];
  try {
    const r = await client.GET("/api/admin/payroll/batches", {
      params: { query: status ? { status } : {} },
    });
    if (r.data) batches = r.data.items ?? [];
  } catch {}
  return { user: locals.user, batches, status };
};
