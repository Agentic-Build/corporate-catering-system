import { redirect, fail, type Actions } from "@sveltejs/kit";
import type { PageServerLoad } from "./$types";
import type { components } from "@tbite/api-client";
import { apiFor } from "$lib/server/api";

type PlantDTO = components["schemas"]["PlantDTO"];
type VendorSettingsDTO = components["schemas"]["VendorSettingsDTO"];

export const load: PageServerLoad = async ({ locals, url }) => {
  if (!locals.user) throw redirect(303, "/login?return_to=" + encodeURIComponent(url.pathname));
  if (locals.user.role !== "vendor_operator") throw redirect(303, "/login");

  const client = apiFor(locals.apiToken);
  let settings: VendorSettingsDTO = { cutoff_hour: 17, preorder_window_days: 7 };
  try {
    const r = await client.GET("/api/merchant/settings", {});
    if (r.data) settings = r.data.settings;
  } catch {}

  const [allRes, myRes] = await Promise.allSettled([
    client.GET("/api/plants"),
    client.GET("/api/merchant/plants"),
  ]);
  const allPlants: PlantDTO[] =
    allRes.status === "fulfilled" ? (allRes.value.data?.items ?? []) : [];
  const myPlantCodes: string[] =
    myRes.status === "fulfilled" ? (myRes.value.data?.items ?? []).map((p) => p.code) : [];

  return { user: locals.user, settings, allPlants, myPlantCodes };
};

export const actions: Actions = {
  save: async ({ request, locals }) => {
    if (!locals.user) return fail(401, { error: "unauthenticated" });
    const fd = await request.formData();
    const cutoffHour = Number.parseInt(String(fd.get("cutoff_hour") ?? ""), 10);
    const windowDays = Number.parseInt(String(fd.get("preorder_window_days") ?? ""), 10);
    if (!Number.isInteger(cutoffHour) || cutoffHour < 0 || cutoffHour > 23) {
      return fail(400, { error: "截單時間需為 0–23 之間的整數" });
    }
    if (!Number.isInteger(windowDays) || windowDays < 1 || windowDays > 30) {
      return fail(400, { error: "預購開放天數需為 1–30 之間的整數" });
    }
    const client = apiFor(locals.apiToken);
    const r = await client.PUT("/api/merchant/settings", {
      body: { cutoff_hour: cutoffHour, preorder_window_days: windowDays },
    });
    if (r.error) {
      const err = r.error as { detail?: string };
      return fail(400, { error: err.detail ?? "儲存設定失敗，請稍後再試。" });
    }
    return { settingsOk: true };
  },

  savePlants: async ({ request, locals }) => {
    if (!locals.user) return fail(401, { error: "unauthenticated" });
    const fd = await request.formData();
    const plants = fd.getAll("plants").map(String);
    const client = apiFor(locals.apiToken);
    const r = await client.PUT("/api/merchant/plants", { body: { plants } });
    if (r.error) {
      const err = r.error as { detail?: string };
      return fail(400, { error: err.detail ?? "儲存服務廠區失敗，請稍後再試。" });
    }
    return { plantsOk: true };
  },
};
