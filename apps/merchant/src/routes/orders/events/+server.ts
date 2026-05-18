import { error } from "@sveltejs/kit";
import type { RequestHandler } from "./$types";
import { API_BASE_URL } from "$lib/server/env";

// SSE proxy: the browser's EventSource cannot attach the bearer token, so the
// merchant app streams /api/merchant/orders/events from the Go API through
// this same-origin route, injecting the session token server-side. The
// upstream stream is piped straight to the browser.
export const GET: RequestHandler = async ({ locals }) => {
  if (!locals.user || locals.user.role !== "vendor_operator") {
    throw error(403, "vendor operator required");
  }
  const upstream = await fetch(`${API_BASE_URL}/api/merchant/orders/events`, {
    headers: { authorization: `Bearer ${locals.apiToken ?? ""}` },
  });
  if (!upstream.ok || !upstream.body) {
    throw error(502, "order event stream unavailable");
  }
  return new Response(upstream.body, {
    headers: {
      "content-type": "text/event-stream",
      "cache-control": "no-cache",
      connection: "keep-alive",
    },
  });
};
