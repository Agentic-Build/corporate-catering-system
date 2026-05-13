import { redirect } from "@sveltejs/kit";
import type { PageServerLoad } from "./$types";
import { apiFor } from "$lib/server/api";

export const load: PageServerLoad = async ({ locals, url }) => {
  if (!locals.user) throw redirect(303, "/login?return_to=" + encodeURIComponent(url.pathname));
  if (locals.user.role !== "welfare_admin") throw redirect(303, "/login");

  const client = apiFor(locals.apiToken);
  let vendors: any[] = [];
  try {
    const r = await client.GET("/api/admin/vendors", { params: { query: {} } });
    if (r.data) vendors = (r.data as any).items ?? [];
  } catch {}

  const counts = {
    pending:    vendors.filter((v: any) => v.status === "pending").length,
    approved:   vendors.filter((v: any) => v.status === "approved").length,
    suspended:  vendors.filter((v: any) => v.status === "suspended").length,
    terminated: vendors.filter((v: any) => v.status === "terminated").length,
  };
  return { user: locals.user, counts, vendors };
};
