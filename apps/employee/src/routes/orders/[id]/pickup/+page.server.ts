import { redirect, error } from "@sveltejs/kit";
import type { PageServerLoad } from "./$types";
import { createApiClient } from "@tbite/api-client";
import { API_BASE_URL } from "$lib/server/env";

export const load: PageServerLoad = async ({ locals, params, url }) => {
  if (!locals.user) {
    throw redirect(303, "/login?return_to=" + encodeURIComponent(url.pathname));
  }
  const client = createApiClient(API_BASE_URL, locals.apiToken);

  // Confirm order exists + status == ready before requesting code
  const orderRes = await client.GET("/api/employee/orders/{id}", {
    params: { path: { id: params.id } },
  });
  if (orderRes.error || !orderRes.data) throw error(404, "order not found");
  const order = (orderRes.data as any).order;
  if (order.status !== "ready") {
    throw redirect(303, `/orders/${params.id}`);
  }

  const codeRes = await client.GET("/api/employee/orders/{id}/pickup-code", {
    params: { path: { id: params.id } },
  });
  if (codeRes.error || !codeRes.data) throw error(409, "cannot get pickup code");
  const code = codeRes.data as any;

  return {
    user: locals.user,
    order,
    code, // {order_id, code, expires_in_seconds}
  };
};
