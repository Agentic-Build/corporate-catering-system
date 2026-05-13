import { redirect } from "@sveltejs/kit";
import type { PageServerLoad } from "./$types";
import { createApiClient } from "@tbite/api-client";
import { API_BASE_URL } from "$lib/server/env";

export const load: PageServerLoad = async ({ locals, url }) => {
  if (!locals.user) {
    throw redirect(303, "/login?return_to=" + encodeURIComponent(url.pathname));
  }
  const client = createApiClient(API_BASE_URL, locals.apiToken);
  let orders: any[] = [];
  try {
    const r = await client.GET("/api/employee/orders");
    if (r.data) orders = (r.data as any).items ?? [];
  } catch {
    // surface no error — empty list is acceptable
  }
  return { user: locals.user, orders };
};
