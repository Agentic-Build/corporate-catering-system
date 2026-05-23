import { redirect } from "@sveltejs/kit";
import type { PageServerLoad } from "./$types";
import { apiFor } from "$lib/server/api";

export const load: PageServerLoad = async ({ locals, url }) => {
  if (!locals.user) throw redirect(303, "/login?return_to=" + encodeURIComponent(url.pathname));
  if (locals.user.role !== "vendor_operator") throw redirect(303, "/login");

  const date = url.searchParams.get("date") ?? new Date().toISOString().slice(0, 10);
  const client = apiFor(locals.apiToken);
  let items: any[] = [];
  try {
    const r = await client.GET("/api/merchant/orders", { params: { query: { date } as any } });
    if (r.data) items = (r.data as any).items ?? [];
  } catch {}

  // 7-day picker (today + next 6) — mirrors the prep board.
  const today = new Date();
  const days: { id: string; label: string }[] = [];
  for (let i = 0; i < 7; i++) {
    const d = new Date(today);
    d.setDate(today.getDate() + i);
    const id = d.toISOString().slice(0, 10);
    const label = i === 0 ? "今天" : i === 1 ? "明天" : id.slice(5);
    days.push({ id, label });
  }

  return { user: locals.user, date, days, orders: items, totalCount: items.length };
};
