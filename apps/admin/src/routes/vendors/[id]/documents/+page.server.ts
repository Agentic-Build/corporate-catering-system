import { redirect, fail, error } from "@sveltejs/kit";
import { problemMessage } from "@tbite/web-shared";
import type { Actions, PageServerLoad } from "./$types";
import type { components, operations } from "@tbite/api-client";
import { apiFor } from "$lib/server/api";
import { formStr } from "@tbite/web-shared";

type VendorDTO = components["schemas"]["VendorDTO"];
type DocumentDTO = components["schemas"]["DocumentDTO"];
type UploadDocumentBody =
  operations["uploadVendorDocument"]["requestBody"]["content"]["application/json"];
type ReviewDocumentBody =
  operations["reviewVendorDocument"]["requestBody"]["content"]["application/json"];

export const load: PageServerLoad = async ({ locals, params, url }) => {
  if (!locals.user) throw redirect(303, "/login?return_to=" + encodeURIComponent(url.pathname));
  if (locals.user.role !== "welfare_admin") throw redirect(303, "/login");

  const client = apiFor(locals.apiToken);

  const vr = await client.GET("/api/admin/vendors", { params: { query: {} } });
  const all: VendorDTO[] = vr.data?.items ?? [];
  const vendor = all.find((v) => v.id === params.id);
  if (!vendor) throw error(404, "vendor not found");

  let documents: DocumentDTO[] = [];
  try {
    const r = await client.GET("/api/admin/vendors/{vendor_id}/documents", {
      params: { path: { vendor_id: params.id }, query: { include_all: true } },
    });
    if (r.data) documents = r.data.items ?? [];
  } catch {}

  return { user: locals.user, vendor, documents };
};

export const actions: Actions = {
  upload: async ({ request, params, locals }) => {
    const fd = await request.formData();
    const filename = formStr(fd, "filename").trim();
    const kind = formStr(fd, "kind") as UploadDocumentBody["kind"];
    const expires_at = formStr(fd, "expires_at").trim();
    const content_base64 = formStr(fd, "content_base64");
    if (!filename || !kind || !content_base64) {
      return fail(400, { error: "filename / kind / content required" });
    }
    const body: UploadDocumentBody = { filename, kind, content_base64 };
    // Backend wants a bare YYYY-MM-DD; an ISO datetime is rejected with 400.
    if (expires_at) body.expires_at = expires_at;

    const client = apiFor(locals.apiToken);
    const r = await client.POST("/api/admin/vendors/{vendor_id}/documents", {
      params: { path: { vendor_id: params.id } },
      body,
    });
    if (r.error) return fail(500, { error: problemMessage(r.error) });
    throw redirect(303, `/vendors/${params.id}/documents`);
  },
  review: async ({ request, locals }) => {
    const fd = await request.formData();
    const id = formStr(fd, "id");
    const status = formStr(fd, "status");
    const notes = formStr(fd, "notes");
    if (!id || (status !== "approved" && status !== "rejected")) {
      return fail(400, { error: "invalid review input" });
    }
    const body: ReviewDocumentBody = { status, notes };
    const client = apiFor(locals.apiToken);
    const r = await client.POST("/api/admin/documents/{id}/review", {
      params: { path: { id } },
      body,
    });
    if (r.error) return fail(500, { error: problemMessage(r.error) });
    return { ok: true };
  },
};
