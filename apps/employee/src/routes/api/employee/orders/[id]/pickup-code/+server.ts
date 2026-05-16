import { json, error } from "@sveltejs/kit";
import type { RequestHandler } from "./$types";
import { createApiClient } from "@tbite/api-client";
import { API_BASE_URL } from "$lib/server/env";

// Server proxy so the global TOTP modal can re-fetch a fresh pickup code
// client-side (on countdown expiry) without exposing API_BASE_URL or the
// session token to the browser. The Go API call still goes through
// createApiClient + locals.apiToken — the established data-flow pattern.
export const GET: RequestHandler = async ({ locals, params }) => {
  if (!locals.user) throw error(401, "unauthenticated");
  const client = createApiClient(API_BASE_URL, locals.apiToken);
  const r = await client.GET("/api/employee/orders/{id}/pickup-code", {
    params: { path: { id: params.id } },
  });
  if (r.error || !r.data) throw error(409, "cannot get pickup code");
  return json(r.data);
};
