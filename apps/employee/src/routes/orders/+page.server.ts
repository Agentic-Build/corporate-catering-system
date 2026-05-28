import { redirect } from "@sveltejs/kit";
import type { PageServerLoad } from "./$types";
import { createApiClient, type components } from "@tbite/api-client";
import { API_BASE_URL } from "$lib/server/env";

type OrderDTO = components["schemas"]["OrderDTO"];

export const load: PageServerLoad = async ({ locals, url }) => {
  if (!locals.user) {
    throw redirect(303, "/login?return_to=" + encodeURIComponent(url.pathname));
  }
  const client = createApiClient(API_BASE_URL, locals.apiToken);
  let orders: OrderDTO[] = [];
  try {
    const r = await client.GET("/api/employee/orders");
    if (r.data) orders = r.data.items ?? [];
  } catch {}
  return { user: locals.user, orders };
};
