import { redirect, fail, error } from "@sveltejs/kit";
import { problemMessage } from "@tbite/web-shared";
import type { Actions, PageServerLoad } from "./$types";
import type { components } from "@tbite/api-client";
import { apiFor } from "$lib/server/api";
import { formStr } from "@tbite/web-shared";

type ExceptionDTO = components["schemas"]["ExceptionDTO"];

export const load: PageServerLoad = async ({ locals, params, url }) => {
  if (!locals.user) throw redirect(303, "/login?return_to=" + encodeURIComponent(url.pathname));
  if (locals.user.role !== "welfare_admin") throw redirect(303, "/login");

  const client = apiFor(locals.apiToken);
  const r = await client.GET("/api/admin/payroll/batches/{id}", {
    params: { path: { id: params.id } },
  });
  if (r.error || !r.data) throw error(404, "batch not found");

  // GET re-runs departed-employee detection as a side-effect.
  let exceptions: ExceptionDTO[] = [];
  const ex = await client.GET("/api/admin/payroll/batches/{id}/exceptions", {
    params: { path: { id: params.id } },
  });
  if (ex.data) exceptions = ex.data.items ?? [];

  return {
    user: locals.user,
    batch: r.data.batch,
    entries: r.data.entries ?? [],
    exceptions,
  };
};

export const actions: Actions = {
  lock: async ({ params, locals }) => {
    const client = apiFor(locals.apiToken);
    const r = await client.POST("/api/admin/payroll/batches/{id}/lock", {
      params: { path: { id: params.id } },
    });
    if (r.error) return fail(500, { error: problemMessage(r.error) });
    throw redirect(303, `/payroll/${params.id}`);
  },

  flagException: async ({ request, params, locals }) => {
    const fd = await request.formData();
    const entryId = formStr(fd, "entry_id");
    const detail = formStr(fd, "detail").trim();
    if (!entryId) return fail(400, { exError: "請選擇要標記的月結明細" });
    const client = apiFor(locals.apiToken);
    const r = await client.POST("/api/admin/payroll/batches/{id}/exceptions", {
      params: { path: { id: params.id } },
      body: { entry_id: entryId, detail },
    });
    if (r.error) {
      const err = r.error as { detail?: string };
      return fail(400, { exError: err.detail ?? "標記例外失敗，請稍後再試。" });
    }
    throw redirect(303, `/payroll/${params.id}`);
  },

  // status=resolved still deducts; status=excluded drops from CSV.
  resolveException: async ({ request, params, locals }) => {
    const fd = await request.formData();
    const exId = formStr(fd, "exception_id");
    const status = formStr(fd, "status");
    const resolution = formStr(fd, "resolution").trim();
    if (!exId || (status !== "resolved" && status !== "excluded")) {
      return fail(400, { exError: "例外解決參數不正確" });
    }
    const client = apiFor(locals.apiToken);
    const r = await client.POST("/api/admin/payroll/exceptions/{id}/resolve", {
      params: { path: { id: exId } },
      body: { status, resolution },
    });
    if (r.error) {
      const err = r.error as { detail?: string };
      return fail(400, { exError: err.detail ?? "解決例外失敗，請稍後再試。" });
    }
    throw redirect(303, `/payroll/${params.id}`);
  },
};
