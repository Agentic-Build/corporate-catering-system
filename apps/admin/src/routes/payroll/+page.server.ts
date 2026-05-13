import { redirect } from "@sveltejs/kit";
import type { PageServerLoad } from "./$types";
import { apiFor } from "$lib/server/api";

export const load: PageServerLoad = async ({ locals, url }) => {
  if (!locals.user) throw redirect(303, "/login?return_to=" + encodeURIComponent(url.pathname));
  if (locals.user.role !== "welfare_admin") throw redirect(303, "/login");

  const status = url.searchParams.get("status") ?? "";
  const client = apiFor(locals.apiToken);
  let batches: any[] = [];
  try {
    const r = await client.GET("/api/admin/payroll/batches", {
      params: { query: (status ? { status } : {}) as any },
    });
    if (r.data) batches = (r.data as any).items ?? [];
  } catch {}
  return { user: locals.user, batches, status };
};
