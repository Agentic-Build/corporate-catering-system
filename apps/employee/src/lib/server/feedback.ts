// Server-side helpers for the F1 員工回饋 (rating + complaint) endpoints.
// These endpoints are not present in the generated `@tbite/api-client`
// schema, so they are called with a thin bearer-token fetch wrapper that
// mirrors how `createApiClient` attaches `Authorization`.
import { API_BASE_URL } from "$lib/server/env";

export type ComplaintCategory =
  | "wrong_item"
  | "missing_item"
  | "quality"
  | "portion"
  | "hygiene"
  | "other";

export type ComplaintStatus = "open" | "vendor_responded" | "escalated" | "resolved";

export interface MealRating {
  id: string;
  order_id: string;
  score: number;
  comment: string;
  created_at: string;
}

export interface MealComplaint {
  id: string;
  order_id: string;
  vendor_id?: string;
  category: ComplaintCategory;
  description: string;
  status: ComplaintStatus;
  vendor_response: string;
  vendor_responded_at?: string | null;
  escalated_at?: string | null;
  resolution: string;
  resolved_at?: string | null;
  created_at: string;
  updated_at?: string;
}

export interface FeedbackResult<T> {
  ok: boolean;
  status: number;
  data?: T;
  error?: string;
}

function authHeaders(token: string | undefined): Record<string, string> {
  const h: Record<string, string> = { "Content-Type": "application/json" };
  if (token) h.Authorization = `Bearer ${token}`;
  return h;
}

// RFC 9457 problem-details bodies carry a human `detail`; fall back to text.
async function readError(res: Response): Promise<string> {
  try {
    const body = (await res.json()) as { detail?: string; title?: string };
    return body.detail ?? body.title ?? `請求失敗（${res.status}）`;
  } catch {
    return `請求失敗（${res.status}）`;
  }
}

async function request<T>(
  token: string | undefined,
  path: string,
  init: RequestInit,
): Promise<FeedbackResult<T>> {
  try {
    const res = await fetch(`${API_BASE_URL}${path}`, {
      ...init,
      headers: authHeaders(token),
    });
    if (!res.ok) {
      return { ok: false, status: res.status, error: await readError(res) };
    }
    if (res.status === 204) return { ok: true, status: res.status };
    const data = (await res.json()) as T;
    return { ok: true, status: res.status, data };
  } catch (e) {
    return { ok: false, status: 0, error: e instanceof Error ? e.message : String(e) };
  }
}

export async function submitRating(
  token: string | undefined,
  orderId: string,
  score: number,
  comment: string,
): Promise<FeedbackResult<MealRating>> {
  // The handler wraps the DTO: { "rating": {...} }. Unwrap to the DTO.
  const r = await request<{ rating: MealRating }>(token, `/api/employee/orders/${orderId}/rating`, {
    method: "POST",
    body: JSON.stringify({ score, comment }),
  });
  if (!r.ok) return { ...r, data: undefined };
  return { ok: true, status: r.status, data: r.data?.rating };
}

export async function submitComplaint(
  token: string | undefined,
  orderId: string,
  category: ComplaintCategory,
  description: string,
): Promise<FeedbackResult<MealComplaint>> {
  // The handler wraps the DTO: { "complaint": {...} }. Unwrap to the DTO.
  const r = await request<{ complaint: MealComplaint }>(
    token,
    `/api/employee/orders/${orderId}/complaint`,
    { method: "POST", body: JSON.stringify({ category, description }) },
  );
  if (!r.ok) return { ...r, data: undefined };
  return { ok: true, status: r.status, data: r.data?.complaint };
}

export async function listComplaints(
  token: string | undefined,
): Promise<FeedbackResult<MealComplaint[]>> {
  const r = await request<{ items?: MealComplaint[] | null }>(token, "/api/employee/complaints", {
    method: "GET",
  });
  if (!r.ok) return { ...r, data: undefined };
  return { ok: true, status: r.status, data: r.data?.items ?? [] };
}

// escalate / resolve return 204 No Content — there is no response body.
export function escalateComplaint(
  token: string | undefined,
  complaintId: string,
): Promise<FeedbackResult<void>> {
  return request<void>(token, `/api/employee/complaints/${complaintId}/escalate`, {
    method: "POST",
  });
}

export function resolveComplaint(
  token: string | undefined,
  complaintId: string,
): Promise<FeedbackResult<void>> {
  return request<void>(token, `/api/employee/complaints/${complaintId}/resolve`, {
    method: "POST",
  });
}
