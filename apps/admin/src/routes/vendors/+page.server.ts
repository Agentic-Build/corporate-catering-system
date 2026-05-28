import { redirect, fail } from "@sveltejs/kit";
import type { Actions, PageServerLoad } from "./$types";
import type { components, operations } from "@tbite/api-client";
import { apiFor } from "$lib/server/api";

type VendorDTO = components["schemas"]["VendorDTO"];
type VendorStatus = NonNullable<operations["listVendors"]["parameters"]["query"]>["status"];

export const load: PageServerLoad = async ({ locals, url }) => {
  if (!locals.user || locals.user.role !== "welfare_admin") throw redirect(303, "/login");
  const status = (url.searchParams.get("status") ?? "") as VendorStatus;
  const client = apiFor(locals.apiToken);
  let vendors: VendorDTO[] = [];
  try {
    const r = await client.GET("/api/admin/vendors", {
      params: { query: status ? { status } : {} },
    });
    if (r.data) vendors = r.data.items ?? [];
  } catch {}
  return { user: locals.user, vendors, status };
};

export const actions: Actions = {
  create: async ({ request, locals }) => {
    const fd = await request.formData();
    const displayName = String(fd.get("display_name") ?? "").trim();
    const legalName = String(fd.get("legal_name") ?? "").trim();
    const email = String(fd.get("contact_email") ?? "")
      .trim()
      .toLowerCase();
    if (!displayName || !legalName || !email) return fail(400, { error: "all fields required" });
    const client = apiFor(locals.apiToken);
    const r = await client.POST("/api/admin/vendors", {
      body: { display_name: displayName, legal_name: legalName, contact_email: email },
    });
    if (r.error) return fail(500, { error: JSON.stringify(r.error) });
    throw redirect(303, "/vendors");
  },
};
