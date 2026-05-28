import { redirect } from "@sveltejs/kit";
import type { PageServerLoad } from "./$types";
import { apiFor } from "$lib/server/api";
import { taipeiISO } from "@tbite/web-shared";

export const load: PageServerLoad = async ({ locals, url }) => {
  if (!locals.user) throw redirect(303, "/login?return_to=" + encodeURIComponent(url.pathname));
  if (locals.user.role !== "vendor_operator") throw redirect(303, "/login");

  const date = url.searchParams.get("date") ?? taipeiISO();
  const client = apiFor(locals.apiToken);

  let sheet: { date: string; total_orders: number; total_portions: number; plants: unknown[] } = {
    date,
    total_orders: 0,
    total_portions: 0,
    plants: [],
  };
  try {
    const r = await client.GET("/api/merchant/prep-sheet", {
      params: { query: { date } },
    });
    if (r.data) sheet = r.data as typeof sheet;
  } catch {}

  return { user: locals.user, date, sheet };
};
