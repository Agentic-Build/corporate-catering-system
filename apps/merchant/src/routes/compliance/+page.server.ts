import { redirect } from "@sveltejs/kit";
import type { components } from "@tbite/api-client";
import type { PageServerLoad } from "./$types";
import { apiFor } from "$lib/server/api";

type VendorInfoDTO = components["schemas"]["VendorInfoDTO"];
type DocumentDTO = components["schemas"]["DocumentDTO"];
type WarningDTO = components["schemas"]["WarningDTO"];

export const load: PageServerLoad = async ({ locals, url }) => {
  if (!locals.user) throw redirect(303, "/login?return_to=" + encodeURIComponent(url.pathname));
  if (locals.user.role !== "vendor_operator") throw redirect(303, "/login");

  const client = apiFor(locals.apiToken);
  let vendor: VendorInfoDTO | null = null;
  let documents: DocumentDTO[] = [];
  let warnings: WarningDTO[] = [];
  try {
    const r = await client.GET("/api/merchant/compliance", {});
    if (r.data) {
      vendor = r.data.vendor ?? null;
      documents = r.data.documents ?? [];
      warnings = r.data.warnings ?? [];
    }
  } catch {}

  return { user: locals.user, vendor, documents, warnings };
};
