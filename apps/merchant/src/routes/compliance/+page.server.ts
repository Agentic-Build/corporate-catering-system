import { redirect, fail, type Actions } from "@sveltejs/kit";
import type { components, operations } from "@tbite/api-client";
import type { PageServerLoad } from "./$types";
import { apiFor } from "$lib/server/api";
import { formStr } from "@tbite/web-shared";

type UploadDocBody =
  operations["uploadMerchantDocument"]["requestBody"]["content"]["application/json"];
type DocKind = UploadDocBody["kind"];

const DOC_KINDS: ReadonlySet<DocKind> = new Set([
  "business_license",
  "food_safety_permit",
  "tax_registration",
  "insurance",
  "other",
]);
const MAX_UPLOAD_BYTES = 10 * 1024 * 1024;

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

export const actions: Actions = {
  // Document upload / resupply (`supersedes` triggers backend replace).
  uploadDocument: async ({ request, locals }) => {
    if (!locals.user) return fail(401, { uploadError: "unauthenticated" });
    const fd = await request.formData();
    const kind = formStr(fd, "kind");
    const file = fd.get("file");
    const expiresAt = formStr(fd, "expires_at").trim();
    const supersedes = formStr(fd, "supersedes").trim();

    if (!DOC_KINDS.has(kind as DocKind)) return fail(400, { uploadError: "請選擇文件種類" });
    if (!(file instanceof File) || file.size === 0) {
      return fail(400, { uploadError: "請選擇要上傳的檔案" });
    }
    if (file.size > MAX_UPLOAD_BYTES) {
      return fail(400, { uploadError: "檔案大小不可超過 10MB" });
    }

    const contentBase64 = Buffer.from(await file.arrayBuffer()).toString("base64");
    const body: UploadDocBody = {
      kind: kind as DocKind,
      filename: file.name,
      content_base64: contentBase64,
    };
    if (expiresAt) body.expires_at = expiresAt;
    if (supersedes) body.supersedes = supersedes;

    const client = apiFor(locals.apiToken);
    const r = await client.POST("/api/merchant/documents", { body });
    if (r.error) {
      const err = r.error as { status?: number; detail?: string };
      return fail(err.status ?? 400, {
        uploadError: err.detail ?? "上傳文件失敗，請稍後再試。",
      });
    }
    return { uploadOk: true };
  },
};
