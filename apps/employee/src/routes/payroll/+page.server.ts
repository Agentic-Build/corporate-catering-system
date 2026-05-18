import { redirect } from "@sveltejs/kit";
import type { PageServerLoad } from "./$types";
import { createApiClient, type components } from "@tbite/api-client";
import { API_BASE_URL } from "$lib/server/env";

type EmployeeEntry = components["schemas"]["EmployeeEntryDTO"];

export const load: PageServerLoad = async ({ locals, url }) => {
  if (!locals.user) {
    throw redirect(303, "/login?return_to=" + encodeURIComponent(url.pathname));
  }
  const client = createApiClient(API_BASE_URL, locals.apiToken);
  let entries: EmployeeEntry[] = [];
  const r = await client.GET("/api/employee/payroll");
  if (r.data) entries = r.data.items ?? [];

  return { user: locals.user, entries };
};
