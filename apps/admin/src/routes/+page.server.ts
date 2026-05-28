import { redirect, fail } from "@sveltejs/kit";
import type { Actions, PageServerLoad } from "./$types";
import { apiFor } from "$lib/server/api";

export const load: PageServerLoad = async ({ locals, url }) => {
  if (!locals.user) throw redirect(303, "/login?return_to=" + encodeURIComponent(url.pathname));
  if (locals.user.role !== "welfare_admin") throw redirect(303, "/login");

  const client = apiFor(locals.apiToken);

  const [vendorsRes, anomaliesRes, batchesRes, plantsRes] = await Promise.allSettled([
    client.GET("/api/admin/vendors", { params: { query: {} } }),
    client.GET("/api/admin/anomalies", { params: { query: {} } }),
    client.GET("/api/admin/payroll/batches", { params: { query: {} } }),
    client.GET("/api/admin/plants"),
  ]);

  const vendors: any[] =
    vendorsRes.status === "fulfilled" ? ((vendorsRes.value.data as any)?.items ?? []) : [];
  const anomalies: any[] =
    anomaliesRes.status === "fulfilled" ? ((anomaliesRes.value.data as any)?.items ?? []) : [];
  const batches: any[] =
    batchesRes.status === "fulfilled" ? ((batchesRes.value.data as any)?.items ?? []) : [];
  const knownPlants: { code: string; label: string; active: boolean }[] =
    plantsRes.status === "fulfilled" ? ((plantsRes.value.data as any)?.items ?? []) : [];

  // Latest batch (most recent period_start) + its entries.
  const latestBatch =
    batches.length > 0
      ? [...batches].sort((a, b) => String(b.period_start).localeCompare(String(a.period_start)))[0]
      : null;
  let payrollBatch: any = null;
  let payrollEntries: any[] = [];
  if (latestBatch) {
    try {
      const detail = await client.GET("/api/admin/payroll/batches/{id}", {
        params: { path: { id: latestBatch.id } },
      });
      if (detail.data) {
        payrollBatch = (detail.data as any).batch ?? latestBatch;
        payrollEntries = (detail.data as any).entries ?? [];
      }
    } catch {
      payrollBatch = latestBatch;
    }
  }

  const pendingVendors = vendors.filter((v) => v.status === "pending");

  const weekAgo = Date.now() - 7 * 24 * 60 * 60 * 1000;
  const recentAnomalies = anomalies.filter((a) => {
    const t = Date.parse(a.created_at ?? "");
    return Number.isFinite(t) && t >= weekAgo;
  });
  const severeCount = recentAnomalies.filter(
    (a) => a.severity === "critical" || a.severity === "high",
  ).length;
  const openAnomalies = recentAnomalies
    .filter((a) => a.status !== "closed")
    .sort((a, b) => String(b.created_at).localeCompare(String(a.created_at)));

  let payrollTotal = 0;
  let payrollRefunded = 0;
  for (const e of payrollEntries) {
    payrollTotal += Number(e.amount_minor ?? 0);
    payrollRefunded += Number(e.refunded_minor ?? 0);
  }

  return {
    user: locals.user,
    knownPlants,
    pendingVendors,
    counts: {
      pending: pendingVendors.length,
      approved: vendors.filter((v) => v.status === "approved").length,
      anomalies7d: recentAnomalies.length,
      anomaliesSevere: severeCount,
    },
    anomalies: openAnomalies,
    payroll: {
      batch: payrollBatch,
      entries: payrollEntries,
      total: payrollTotal,
      refunded: payrollRefunded,
    },
  };
};

export const actions: Actions = {
  approveVendor: async ({ request, locals }) => {
    const fd = await request.formData();
    const id = String(fd.get("id") ?? "");
    if (!id) return fail(400, { error: "vendor id required" });
    const plants = fd.getAll("plants").map(String);
    const client = apiFor(locals.apiToken);
    const r = await client.POST("/api/admin/vendors/{id}/approve", {
      params: { path: { id } },
      body: { plants } as any,
    });
    if (r.error) return fail(500, { error: JSON.stringify(r.error) });
    return { ok: true, approved: id };
  },
};
