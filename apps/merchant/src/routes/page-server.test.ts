import { describe, it, expect, vi, beforeEach } from "vitest";

const { mockClient } = vi.hoisted(() => ({
  mockClient: { GET: vi.fn(), POST: vi.fn(), PUT: vi.fn() },
}));
vi.mock("$lib/server/api", () => ({ apiFor: () => mockClient }));

import { actions as complaints } from "./complaints/+page.server";
import { actions as menus } from "./menus/+page.server";
import { actions as settings } from "./settings/+page.server";
import { actions as compliance } from "./compliance/+page.server";
import { load as ordersLoad } from "./orders/+page.server";

const VENDOR = { id: "u1", role: "vendor_operator" };

// Minimal SvelteKit action/load event with a FormData-backed request.
function event(fd: FormData, user: unknown = VENDOR) {
  return { request: { formData: async () => fd }, locals: { user, apiToken: "t" } } as never;
}
function form(entries: Record<string, string>): FormData {
  const fd = new FormData();
  for (const [k, v] of Object.entries(entries)) fd.append(k, v);
  return fd;
}

beforeEach(() => {
  mockClient.GET.mockReset();
  mockClient.POST.mockReset();
  mockClient.PUT.mockReset();
});

describe("complaints.respond", () => {
  it("posts a trimmed response and returns the responded id", async () => {
    mockClient.POST.mockResolvedValue({ data: {} });
    const res = await complaints.respond!(
      event(form({ complaint_id: "c1", response: "  hello there  " })),
    );
    expect(res).toEqual({ success: true, respondedID: "c1" });
    expect(mockClient.POST).toHaveBeenCalledWith(
      "/api/merchant/complaints/{id}/respond",
      expect.objectContaining({ body: { response: "hello there" } }),
    );
  });
});

describe("menus.copy / menus.delete", () => {
  it("copy redirects to the new item", async () => {
    mockClient.POST.mockResolvedValue({ data: { item: { id: "m2" } } });
    await expect(menus.copy!(event(form({ id: "m1" })))).rejects.toMatchObject({
      status: 303,
      location: "/menus/m2",
    });
  });
  it("delete redirects back to the list", async () => {
    mockClient.POST.mockResolvedValue({ data: {} });
    await expect(menus.delete!(event(form({ id: "m1" })))).rejects.toMatchObject({
      status: 303,
      location: "/menus",
    });
  });
});

describe("settings.save", () => {
  it("parses cutoff/window and saves", async () => {
    mockClient.PUT.mockResolvedValue({ data: {} });
    const res = await settings.save!(event(form({ cutoff_hour: "12", preorder_window_days: "7" })));
    expect(res).toEqual({ settingsOk: true });
    expect(mockClient.PUT).toHaveBeenCalledWith(
      "/api/merchant/settings",
      expect.objectContaining({ body: { cutoff_hour: 12, preorder_window_days: 7 } }),
    );
  });
});

describe("compliance.uploadDocument", () => {
  it("reads kind/expires/supersedes before rejecting a missing file", async () => {
    const res = await compliance.uploadDocument!(
      event(form({ kind: "insurance", expires_at: " 2026-12-31 ", supersedes: " old " })),
    );
    expect(res).toMatchObject({ status: 400, data: { uploadError: "請選擇要上傳的檔案" } });
  });
});

describe("orders.load", () => {
  it("groups orders by plant", async () => {
    mockClient.GET.mockImplementation((path: string) =>
      Promise.resolve(
        path === "/api/merchant/orders"
          ? { data: { items: [{ plant: "A" }, { plant: "A" }, { plant: "B" }] } }
          : { data: { items: [] } },
      ),
    );
    const res = (await ordersLoad({
      locals: { user: VENDOR, apiToken: "t" },
      url: new URL("http://x/orders"),
      depends: vi.fn(),
    } as never)) as { byPlant: Record<string, unknown[]>; totalCount: number };
    expect(res.totalCount).toBe(3);
    expect(res.byPlant.A).toHaveLength(2);
    expect(res.byPlant.B).toHaveLength(1);
  });
});
