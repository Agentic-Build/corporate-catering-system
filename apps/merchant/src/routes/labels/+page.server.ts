import { redirect } from "@sveltejs/kit";
import type { PageServerLoad } from "./$types";
import type { components } from "@tbite/api-client";
import { apiFor } from "$lib/server/api";

type MerchantOrderDTO = components["schemas"]["MerchantOrderDTO"];
type MerchantItemDTO = components["schemas"]["MerchantItemDTO"];

export const load: PageServerLoad = async ({ locals, url }) => {
  if (!locals.user) throw redirect(303, "/login?return_to=" + encodeURIComponent(url.pathname));
  if (locals.user.role !== "vendor_operator") throw redirect(303, "/login");

  // 以 Asia/Taipei (UTC+8) 計算「今天」，避免 UTC 午夜後日期少一天。
  const todayStr = new Intl.DateTimeFormat("en-CA", { timeZone: "Asia/Taipei" }).format(new Date());
  const date = url.searchParams.get("date") ?? todayStr;
  const client = apiFor(locals.apiToken);
  let items: MerchantOrderDTO[] = [];
  try {
    const r = await client.GET("/api/merchant/orders", { params: { query: { date } } });
    if (r.data) items = r.data.items ?? [];
  } catch {}

  let menuItems: MerchantItemDTO[] = [];
  try {
    const r = await client.GET("/api/merchant/menu-items", {
      params: { query: { include_archived: true } },
    });
    if (r.data) menuItems = r.data.items ?? [];
  } catch {}
  const itemsById: Record<string, { name: string }> = Object.fromEntries(
    menuItems.map((i) => [i.id, { name: i.name }]),
  );

  const [yy = 0, mm = 1, dd = 1] = todayStr.split("-").map(Number);
  const days: { id: string; label: string }[] = [];
  for (let i = 0; i < 7; i++) {
    const dt = new Date(Date.UTC(yy, mm - 1, dd + i));
    const id = dt.toISOString().slice(0, 10);
    let label: string;
    if (i === 0) label = "今天";
    else if (i === 1) label = "明天";
    else label = id.slice(5);
    days.push({ id, label });
  }

  return { user: locals.user, date, days, orders: items, totalCount: items.length, itemsById };
};
