import { redirect, fail } from "@sveltejs/kit";
import type { Actions, PageServerLoad } from "./$types";
import { apiFor } from "$lib/server/api";

export const load: PageServerLoad = async ({ locals, url }) => {
  if (!locals.user) throw redirect(303, "/login?return_to=" + encodeURIComponent(url.pathname));
  const today = new Date();
  const date = url.searchParams.get("date") ?? today.toISOString().slice(0, 10);

  const client = apiFor(locals.apiToken);
  let supplies: any[] = [];
  try {
    const r = await client.GET("/api/merchant/supply", { params: { query: { date } } });
    if (r.data) supplies = (r.data as any).items ?? [];
  } catch {}

  let items: any[] = [];
  try {
    const r = await client.GET("/api/merchant/menu-items", { params: { query: {} } });
    if (r.data) items = ((r.data as any).items ?? []).filter((i: any) => i.status === "active");
  } catch {}

  // Build 7 candidate days for the picker
  const days: { id: string; label: string }[] = [];
  for (let i = 0; i < 7; i++) {
    const d = new Date(today);
    d.setDate(today.getDate() + i);
    const id = d.toISOString().slice(0, 10);
    const label = i === 0 ? "今天" : i === 1 ? "明天" : id.slice(5);
    days.push({ id, label });
  }

  return { user: locals.user, date, supplies, items, days };
};

export const actions: Actions = {
  set: async ({ request, locals }) => {
    const fd = await request.formData();
    const itemId = String(fd.get("item_id") ?? "");
    const date = String(fd.get("date") ?? "");
    const capacity = parseInt(String(fd.get("capacity") ?? "0"), 10);
    const pickupWindow = String(fd.get("pickup_window") ?? "11:50-12:10");
    const cutoffAt = String(fd.get("cutoff_at") ?? `${date}T17:00:00Z`);

    if (!itemId || !date) return fail(400, { error: "item + date required" });
    if (!Number.isFinite(capacity) || capacity < 0) return fail(400, { error: "capacity invalid" });

    const client = apiFor(locals.apiToken);
    const r = await client.PUT("/api/merchant/supply/{itemID}/{date}", {
      params: { path: { itemID: itemId, date } },
      body: {
        capacity,
        pickup_window: pickupWindow,
        eta_label: pickupWindow,
        cutoff_at: cutoffAt,
      } as any,
    });
    if (r.error) return fail(500, { error: JSON.stringify(r.error) });
    return { success: true };
  },
};
