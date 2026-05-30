import { describe, it, expect, vi, beforeEach } from "vitest";

const { mockClient } = vi.hoisted(() => ({
  mockClient: { GET: vi.fn(), POST: vi.fn() },
}));
vi.mock("$lib/server/api", () => ({ apiFor: () => mockClient }));

import { actions, load } from "./+page.server";

const VENDOR = { id: "u1", role: "vendor_operator" };

function loadEvent(user: unknown = VENDOR) {
  return { locals: { user, apiToken: "t" }, url: new URL("http://x/compliance") } as never;
}
function actionEvent(fd: FormData, user: unknown = VENDOR) {
  return { request: { formData: async () => fd }, locals: { user, apiToken: "t" } } as never;
}
function form(entries: Record<string, string | File>): FormData {
  const fd = new FormData();
  for (const [k, v] of Object.entries(entries)) fd.append(k, v);
  return fd;
}

beforeEach(() => {
  mockClient.GET.mockReset();
  mockClient.POST.mockReset();
});

describe("compliance load", () => {
  it("redirects unauthenticated", async () => {
    await expect(
      load({ locals: {}, url: new URL("http://x/compliance") } as never),
    ).rejects.toMatchObject({ status: 303 });
  });

  it("redirects non-vendor", async () => {
    await expect(load(loadEvent({ role: "employee" }))).rejects.toMatchObject({
      status: 303,
      location: "/login",
    });
  });

  it("returns vendor/documents/warnings", async () => {
    mockClient.GET.mockResolvedValue({
      data: { vendor: { id: "v1" }, documents: [{ id: "d1" }], warnings: [{ id: "w1" }] },
    });
    const res = (await load(loadEvent())) as {
      vendor: unknown;
      documents: unknown[];
      warnings: unknown[];
    };
    expect(res.vendor).toEqual({ id: "v1" });
    expect(res.documents).toHaveLength(1);
    expect(res.warnings).toHaveLength(1);
  });

  it("defaults to null/empty on missing fields and on throw", async () => {
    mockClient.GET.mockResolvedValueOnce({ data: {} });
    let res = (await load(loadEvent())) as {
      vendor: unknown;
      documents: unknown[];
      warnings: unknown[];
    };
    expect(res.vendor).toBeNull();
    expect(res.documents).toEqual([]);
    expect(res.warnings).toEqual([]);

    mockClient.GET.mockRejectedValueOnce(new Error("boom"));
    res = (await load(loadEvent())) as {
      vendor: unknown;
      documents: unknown[];
      warnings: unknown[];
    };
    expect(res.vendor).toBeNull();
    expect(res.documents).toEqual([]);
  });
});

describe("compliance.uploadDocument branches", () => {
  it("rejects unauthenticated", async () => {
    const res = await actions.uploadDocument!(actionEvent(form({}), null));
    expect(res).toMatchObject({ status: 401, data: { uploadError: "unauthenticated" } });
  });

  it("rejects invalid kind", async () => {
    const res = await actions.uploadDocument!(actionEvent(form({ kind: "weird" })));
    expect(res).toMatchObject({ status: 400, data: { uploadError: "請選擇文件種類" } });
  });

  it("rejects missing file", async () => {
    const res = await actions.uploadDocument!(actionEvent(form({ kind: "insurance" })));
    expect(res).toMatchObject({ status: 400, data: { uploadError: "請選擇要上傳的檔案" } });
  });

  it("rejects empty file", async () => {
    const empty = new File([], "x.pdf");
    const res = await actions.uploadDocument!(
      actionEvent(form({ kind: "insurance", file: empty })),
    );
    expect(res).toMatchObject({ status: 400, data: { uploadError: "請選擇要上傳的檔案" } });
  });

  it("rejects oversize file", async () => {
    const big = new File([new Uint8Array(11 * 1024 * 1024)], "big.pdf");
    const res = await actions.uploadDocument!(actionEvent(form({ kind: "insurance", file: big })));
    expect(res).toMatchObject({ status: 400, data: { uploadError: "檔案大小不可超過 10MB" } });
  });

  it("uploads with expires_at and supersedes set", async () => {
    mockClient.POST.mockResolvedValue({ data: {} });
    const file = new File([new Uint8Array([1, 2, 3])], "license.pdf");
    const res = await actions.uploadDocument!(
      actionEvent(
        form({ kind: "insurance", file, expires_at: " 2026-12-31 ", supersedes: " old-id " }),
      ),
    );
    expect(res).toEqual({ uploadOk: true });
    expect(mockClient.POST).toHaveBeenCalledWith(
      "/api/merchant/documents",
      expect.objectContaining({
        body: expect.objectContaining({
          kind: "insurance",
          filename: "license.pdf",
          content_base64: Buffer.from(new Uint8Array([1, 2, 3])).toString("base64"),
          expires_at: "2026-12-31",
          supersedes: "old-id",
        }),
      }),
    );
  });

  it("uploads without optional fields", async () => {
    mockClient.POST.mockResolvedValue({ data: {} });
    const file = new File([new Uint8Array([9])], "doc.pdf");
    await actions.uploadDocument!(actionEvent(form({ kind: "other", file })));
    const body = mockClient.POST.mock.calls[0]![1].body;
    expect(body.expires_at).toBeUndefined();
    expect(body.supersedes).toBeUndefined();
  });

  it("returns API error status/detail", async () => {
    mockClient.POST.mockResolvedValue({ error: { status: 409, detail: "duplicate" } });
    const file = new File([new Uint8Array([1])], "doc.pdf");
    const res = await actions.uploadDocument!(actionEvent(form({ kind: "other", file })));
    expect(res).toMatchObject({ status: 409, data: { uploadError: "duplicate" } });
  });

  it("falls back to defaults when error has no status/detail", async () => {
    mockClient.POST.mockResolvedValue({ error: {} });
    const file = new File([new Uint8Array([1])], "doc.pdf");
    const res = await actions.uploadDocument!(actionEvent(form({ kind: "other", file })));
    expect(res).toMatchObject({ status: 400, data: { uploadError: "上傳文件失敗，請稍後再試。" } });
  });
});
