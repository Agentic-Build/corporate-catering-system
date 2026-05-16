import { redirect, fail, type Actions } from "@sveltejs/kit";
import type { PageServerLoad } from "./$types";
import { apiFor } from "$lib/server/api";

// Complaint statuses surfaced to the merchant inbox.
const STATUS_VALUES = ["open", "vendor_responded", "escalated", "resolved"] as const;

export const load: PageServerLoad = async ({ locals, url }) => {
  if (!locals.user) throw redirect(303, "/login?return_to=" + encodeURIComponent(url.pathname));
  if (locals.user.role !== "vendor_operator") throw redirect(303, "/login");

  const statusParam = url.searchParams.get("status") ?? "";
  const status = (STATUS_VALUES as readonly string[]).includes(statusParam) ? statusParam : "";

  const client = apiFor(locals.apiToken);
  let items: any[] = [];
  try {
    const r = await client.GET("/api/merchant/complaints" as any, {
      params: { query: status ? { status } : {} } as any,
    });
    if ((r as any).data) items = ((r as any).data as any).items ?? [];
  } catch {}

  return { user: locals.user, items, status };
};

export const actions: Actions = {
  respond: async ({ request, locals }) => {
    const fd = await request.formData();
    const id = String(fd.get("complaint_id") ?? "");
    const response = String(fd.get("response") ?? "").trim();
    if (!id) return fail(400, { error: "缺少客訴編號" });
    if (response.length < 5) return fail(400, { error: "回覆內容至少需 5 個字", complaintID: id });

    const client = apiFor(locals.apiToken);
    const r = await client.POST("/api/merchant/complaints/{id}/respond" as any, {
      params: { path: { id } } as any,
      body: { response } as any,
    });
    if ((r as any).error) {
      return fail(400, { error: "回覆失敗，請稍後再試", complaintID: id });
    }
    return { success: true, respondedID: id };
  },
};
