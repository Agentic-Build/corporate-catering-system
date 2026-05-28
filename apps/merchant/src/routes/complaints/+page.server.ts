import { redirect, fail, type Actions } from "@sveltejs/kit";
import type { components } from "@tbite/api-client";
import type { PageServerLoad } from "./$types";
import { apiFor } from "$lib/server/api";

type ComplaintDTO = components["schemas"]["ComplaintDTO"];

const STATUS_VALUES = ["open", "vendor_responded", "escalated", "resolved"] as const;
type MerchantComplaintStatus = (typeof STATUS_VALUES)[number];

export const load: PageServerLoad = async ({ locals, url }) => {
  if (!locals.user) throw redirect(303, "/login?return_to=" + encodeURIComponent(url.pathname));
  if (locals.user.role !== "vendor_operator") throw redirect(303, "/login");

  const statusParam = url.searchParams.get("status") ?? "";
  const status: MerchantComplaintStatus | "" = (STATUS_VALUES as readonly string[]).includes(
    statusParam,
  )
    ? (statusParam as MerchantComplaintStatus)
    : "";

  const client = apiFor(locals.apiToken);
  let items: ComplaintDTO[] = [];
  try {
    const r = await client.GET("/api/merchant/complaints", {
      params: { query: status ? { status } : {} },
    });
    if (r.data) items = r.data.items ?? [];
  } catch {}

  return { user: locals.user, items, status };
};

export const actions: Actions = {
  respond: async ({ request, locals }) => {
    const fd = await request.formData();
    const id = fd.get("complaint_id")?.toString() ?? "";
    const response = fd.get("response")?.toString() ?? "".trim();
    if (!id) return fail(400, { error: "缺少客訴編號" });
    if (response.length < 5) return fail(400, { error: "回覆內容至少需 5 個字", complaintID: id });

    const client = apiFor(locals.apiToken);
    const r = await client.POST("/api/merchant/complaints/{id}/respond", {
      params: { path: { id } },
      body: { response },
    });
    if (r.error) {
      return fail(400, { error: "回覆失敗，請稍後再試", complaintID: id });
    }
    return { success: true, respondedID: id };
  },
};
