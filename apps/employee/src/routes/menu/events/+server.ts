import { error } from "@sveltejs/kit";
import type { RequestHandler } from "./$types";
import { API_BASE_URL } from "$lib/server/env";

// SSE proxy: EventSource can't send bearer tokens, so inject it server-side.
export const GET: RequestHandler = async ({ locals }) => {
  if (!locals.user) throw error(403, "unauthenticated");
  const upstream = await fetch(`${API_BASE_URL}/api/employee/menu/events`, {
    headers: { authorization: `Bearer ${locals.apiToken ?? ""}` },
  });
  if (!upstream.ok || !upstream.body) {
    throw error(502, "menu event stream unavailable");
  }
  return new Response(upstream.body, {
    headers: {
      "content-type": "text/event-stream",
      "cache-control": "no-cache",
      connection: "keep-alive",
    },
  });
};
