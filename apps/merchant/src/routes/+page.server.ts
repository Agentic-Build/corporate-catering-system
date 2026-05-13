import { redirect } from "@sveltejs/kit";
import type { PageServerLoad } from "./$types";
import { apiFor } from "$lib/server/api";

export const load: PageServerLoad = async ({ locals, url }) => {
  if (!locals.user) throw redirect(303, "/login?return_to=" + encodeURIComponent(url.pathname));
  if (locals.user.role !== "vendor_operator") throw redirect(303, "/login");

  const client = apiFor(locals.apiToken);
  const today = new Date().toISOString().slice(0, 10);

  // Best-effort fetches; show empty cards if anything fails.
  let supplies: any[] = [];
  try {
    const r = await client.GET("/api/merchant/supply", { params: { query: { date: today } } });
    if (r.data) supplies = (r.data as any).items ?? [];
  } catch {}

  let items: any[] = [];
  try {
    const r = await client.GET("/api/merchant/menu-items", { params: { query: {} } });
    if (r.data) items = (r.data as any).items ?? [];
  } catch {}

  const totalCapacity = supplies.reduce((a: number, s: any) => a + s.capacity, 0);
  const totalSold = supplies.reduce((a: number, s: any) => a + (s.capacity - s.remain), 0);
  const activeItems = items.filter((i: any) => i.status === "active").length;

  return { user: locals.user, today, totalCapacity, totalSold, activeItems, items };
};
