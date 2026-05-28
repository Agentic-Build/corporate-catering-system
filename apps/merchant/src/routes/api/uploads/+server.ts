import { json, error } from "@sveltejs/kit";
import type { RequestHandler } from "./$types";
import { API_BASE_URL } from "$lib/server/env";

// Proxy to the Go API: keeps API_BASE_URL + session token off the browser.
export const POST: RequestHandler = async ({ request, locals }) => {
  if (!locals.user) throw error(401, "unauthenticated");
  const fd = await request.formData();
  const file = fd.get("file");
  if (!(file instanceof File)) throw error(400, "missing file");

  const forward = new FormData();
  forward.set("file", file);

  const r = await fetch(`${API_BASE_URL}/api/merchant/uploads`, {
    method: "POST",
    headers: locals.apiToken ? { Authorization: `Bearer ${locals.apiToken}` } : {},
    body: forward,
  });
  if (!r.ok) {
    const detail = await r.text();
    throw error(r.status, detail || "upload failed");
  }
  const data = (await r.json()) as { url: string };
  return json(data);
};
