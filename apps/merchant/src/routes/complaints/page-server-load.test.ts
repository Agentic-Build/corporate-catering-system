import { describe, it, expect, vi, beforeEach } from "vitest";

const { mockClient } = vi.hoisted(() => ({
  mockClient: { GET: vi.fn(), POST: vi.fn(), PUT: vi.fn() },
}));
vi.mock("$lib/server/api", () => ({ apiFor: () => mockClient }));

import { actions, load } from "./+page.server";

const VENDOR = { id: "u1", role: "vendor_operator" };

function loadEvent(search = "", user: unknown = VENDOR) {
  return { locals: { user, apiToken: "t" }, url: new URL("http://x/complaints" + search) } as never;
}
function actionEvent(fd: FormData) {
  return {
    request: { formData: async () => fd },
    locals: { user: VENDOR, apiToken: "t" },
  } as never;
}
function form(entries: Record<string, string>): FormData {
  const fd = new FormData();
  for (const [k, v] of Object.entries(entries)) fd.append(k, v);
  return fd;
}

beforeEach(() => {
  mockClient.GET.mockReset();
  mockClient.POST.mockReset();
});

describe("complaints load", () => {
  it("redirects unauthenticated", async () => {
    await expect(
      load({ locals: {}, url: new URL("http://x/complaints") } as never),
    ).rejects.toMatchObject({ status: 303, location: "/login?return_to=%2Fcomplaints" });
  });

  it("redirects non-vendor", async () => {
    await expect(load(loadEvent("", { role: "employee" }))).rejects.toMatchObject({
      status: 303,
      location: "/login",
    });
  });

  it("passes a valid status filter through", async () => {
    mockClient.GET.mockResolvedValue({ data: { items: [{ id: "c1" }] } });
    const res = (await load(loadEvent("?status=open"))) as { items: unknown[]; status: string };
    expect(res.status).toBe("open");
    expect(res.items).toHaveLength(1);
    expect(mockClient.GET).toHaveBeenCalledWith(
      "/api/merchant/complaints",
      expect.objectContaining({ params: { query: { status: "open" } } }),
    );
  });

  it("ignores an invalid status filter", async () => {
    mockClient.GET.mockResolvedValue({ data: { items: [] } });
    const res = (await load(loadEvent("?status=bogus"))) as { status: string };
    expect(res.status).toBe("");
    expect(mockClient.GET).toHaveBeenCalledWith(
      "/api/merchant/complaints",
      expect.objectContaining({ params: { query: {} } }),
    );
  });

  it("defaults items to [] on API throw and on missing data", async () => {
    mockClient.GET.mockRejectedValueOnce(new Error("boom"));
    let res = (await load(loadEvent())) as { items: unknown[] };
    expect(res.items).toEqual([]);
    mockClient.GET.mockResolvedValueOnce({ data: null });
    res = (await load(loadEvent())) as { items: unknown[] };
    expect(res.items).toEqual([]);
    mockClient.GET.mockResolvedValueOnce({ data: {} });
    res = (await load(loadEvent())) as { items: unknown[] };
    expect(res.items).toEqual([]);
  });
});

describe("complaints.respond branches", () => {
  it("fails when complaint_id missing", async () => {
    const res = await actions.respond!(actionEvent(form({ complaint_id: "", response: "hello" })));
    expect(res).toMatchObject({ status: 400, data: { error: "缺少客訴編號" } });
  });

  it("fails when response too short", async () => {
    const res = await actions.respond!(actionEvent(form({ complaint_id: "c1", response: "hi" })));
    expect(res).toMatchObject({
      status: 400,
      data: { error: "回覆內容至少需 5 個字", complaintID: "c1" },
    });
  });

  it("fails when API errors", async () => {
    mockClient.POST.mockResolvedValue({ error: { detail: "x" } });
    const res = await actions.respond!(
      actionEvent(form({ complaint_id: "c1", response: "hello there" })),
    );
    expect(res).toMatchObject({
      status: 400,
      data: { error: "回覆失敗，請稍後再試", complaintID: "c1" },
    });
  });
});
