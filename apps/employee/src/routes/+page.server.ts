import type { PageServerLoad } from "./$types";
import { redirect } from "@sveltejs/kit";
import { createApiClient } from "@tbite/api-client";
import { API_BASE_URL } from "$lib/server/env";

const PLANTS = [
  { id: "F12B-3F", label: "F12B · 3F" },
  { id: "F12B-1F", label: "F12B · 1F" },
  { id: "F15-2F", label: "F15 · 2F" },
  { id: "F18-RF", label: "F18 · RF" },
];

function buildDays(today: Date) {
  const wk = ["日", "一", "二", "三", "四", "五", "六"];
  const labels = ["今天", "明天"];
  const out: { id: string; head: string; sub?: string }[] = [];
  for (let i = 0; i < 7; i++) {
    const d = new Date(today);
    d.setDate(today.getDate() + i);
    const m = d.getMonth() + 1;
    const day = d.getDate();
    const w = wk[d.getDay()];
    const head = labels[i] ?? `${m}/${day}(${w})`;
    const id = `${d.getFullYear()}-${String(m).padStart(2, "0")}-${String(day).padStart(2, "0")}`;
    out.push({ id, head, sub: i < 2 ? `${m}/${day}(${w})` : undefined });
  }
  return out;
}

export const load: PageServerLoad = async ({ locals, url }) => {
  if (!locals.user) {
    throw redirect(303, "/login?return_to=" + encodeURIComponent(url.pathname + url.search));
  }

  const today = new Date();
  const days = buildDays(today);
  const selectedDay = url.searchParams.get("day") ?? days[0].id;
  const selectedPlant = url.searchParams.get("plant") ?? locals.user.plant ?? PLANTS[0].id;

  let items: any[] = [];
  let error: string | undefined;
  try {
    const client = createApiClient(API_BASE_URL, locals.apiToken);
    const res = await client.GET("/api/employee/menu", {
      params: { query: { plant: selectedPlant, day: selectedDay } },
    });
    if (res.data) {
      items = (res.data as any).items ?? [];
    } else if (res.error) {
      error = JSON.stringify(res.error);
    }
  } catch (e) {
    error = e instanceof Error ? e.message : String(e);
  }

  return {
    user: locals.user,
    plants: PLANTS,
    days,
    selectedPlant,
    selectedDay,
    items,
    error,
  };
};
