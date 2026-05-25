import { redirect, fail } from "@sveltejs/kit";
import type { Actions, PageServerLoad } from "./$types";
import { apiFor } from "$lib/server/api";

export const load: PageServerLoad = async ({ locals, url }) => {
  if (!locals.user) throw redirect(303, "/login?return_to=" + encodeURIComponent(url.pathname));
  if (locals.user.role !== "vendor_operator") throw redirect(303, "/login");

  const client = apiFor(locals.apiToken);
  const [allRes, myRes] = await Promise.allSettled([
    client.GET("/api/plants"),
    client.GET("/api/merchant/plants"),
  ]);

  const allPlants: { code: string; label: string; address: string }[] =
    allRes.status === "fulfilled" ? ((allRes.value.data as any)?.items ?? []) : [];
  const myPlantCodes = new Set<string>(
    myRes.status === "fulfilled"
      ? ((myRes.value.data as any)?.items ?? []).map((p: { code: string }) => p.code)
      : [],
  );

  return { user: locals.user, allPlants, myPlantCodes: Array.from(myPlantCodes) };
};

export const actions: Actions = {
  save: async ({ request, locals }) => {
    if (!locals.user) return fail(401, { error: "unauthenticated" });
    const fd = await request.formData();
    const plants = fd.getAll("plants").map(String);

    const client = apiFor(locals.apiToken);
    const r = await client.PUT("/api/merchant/plants", {
      body: { plants } as any,
    });
    if (r.error) {
      const err = r.error as { detail?: string };
      return fail(400, { error: err.detail ?? "儲存失敗，請稍後再試。" });
    }
    return { ok: true };
  },
};
