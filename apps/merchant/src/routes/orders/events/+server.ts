import { error } from "@sveltejs/kit";
import type { RequestHandler } from "./$types";
import { API_BASE_URL } from "$lib/server/env";

// SSE proxy: EventSource can't send bearer tokens, so inject it server-side.
// Forward the downstream AbortSignal so a client disconnect cancels upstream.
export const GET: RequestHandler = async ({ locals, request }) => {
  if (!locals.user || locals.user.role !== "vendor_operator") {
    throw error(403, "vendor operator required");
  }
  const upstream = await fetch(`${API_BASE_URL}/api/merchant/orders/events`, {
    headers: { authorization: `Bearer ${locals.apiToken ?? ""}` },
    signal: request.signal,
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
