import { describe, it, expect, vi, beforeEach } from "vitest";

const { mockClient } = vi.hoisted(() => ({
  mockClient: { GET: vi.fn(), PUT: vi.fn() },
}));
vi.mock("$lib/server/api", () => ({ apiFor: () => mockClient }));

import { actions, load } from "./+page.server";

const VENDOR = { id: "u1", role: "vendor_operator" };

function loadEvent(user: unknown = VENDOR) {
  return { locals: { user, apiToken: "t" }, url: new URL("http://x/settings") } as never;
}
function actionEvent(fd: FormData, user: unknown = VENDOR) {
  return { request: { formData: async () => fd }, locals: { user, apiToken: "t" } } as never;
}
function form(entries: Record<string, string | string[]>): FormData {
  const fd = new FormData();
  for (const [k, v] of Object.entries(entries)) {
    if (Array.isArray(v)) v.forEach((x) => fd.append(k, x));
    else fd.append(k, v);
  }
  return fd;
}

beforeEach(() => {
  mockClient.GET.mockReset();
  mockClient.PUT.mockReset();
});

describe("settings load", () => {
  it("redirects unauthenticated", async () => {
    await expect(
      load({ locals: {}, url: new URL("http://x/settings") } as never),
    ).rejects.toMatchObject({
      status: 303,
    });
  });

  it("redirects non-vendor", async () => {
    await expect(load(loadEvent({ role: "employee" }))).rejects.toMatchObject({
      status: 303,
      location: "/login",
    });
  });

  it("returns settings + plant lists", async () => {
    mockClient.GET.mockImplementation((path: string) => {
      if (path === "/api/merchant/settings")
        return Promise.resolve({
          data: { settings: { cutoff_hour: 15, preorder_window_days: 5 } },
        });
      if (path === "/api/plants")
        return Promise.resolve({ data: { items: [{ code: "P1" }, { code: "P2" }] } });
      if (path === "/api/merchant/plants")
        return Promise.resolve({ data: { items: [{ code: "P1" }] } });
      return Promise.resolve({ data: {} });
    });
    const res = (await load(loadEvent())) as {
      settings: { cutoff_hour: number };
      allPlants: unknown[];
      myPlantCodes: string[];
    };
    expect(res.settings.cutoff_hour).toBe(15);
    expect(res.allPlants).toHaveLength(2);
    expect(res.myPlantCodes).toEqual(["P1"]);
  });

  it("uses defaults when settings GET throws and plant calls reject", async () => {
    mockClient.GET.mockImplementation((path: string) => {
      if (path === "/api/merchant/settings") return Promise.reject(new Error("boom"));
      return Promise.reject(new Error("boom"));
    });
    const res = (await load(loadEvent())) as {
      settings: { cutoff_hour: number; preorder_window_days: number };
      allPlants: unknown[];
      myPlantCodes: string[];
    };
    expect(res.settings).toEqual({ cutoff_hour: 17, preorder_window_days: 7 });
    expect(res.allPlants).toEqual([]);
    expect(res.myPlantCodes).toEqual([]);
  });

  it("handles fulfilled plant calls with missing data", async () => {
    mockClient.GET.mockImplementation((path: string) => {
      if (path === "/api/merchant/settings") return Promise.resolve({ data: null });
      return Promise.resolve({ data: null });
    });
    const res = (await load(loadEvent())) as { allPlants: unknown[]; myPlantCodes: string[] };
    expect(res.allPlants).toEqual([]);
    expect(res.myPlantCodes).toEqual([]);
  });
});

describe("settings.save branches", () => {
  it("rejects unauthenticated", async () => {
    const res = await actions.save!(actionEvent(form({}), null));
    expect(res).toMatchObject({ status: 401 });
  });

  it("rejects out-of-range cutoff hour", async () => {
    const res = await actions.save!(
      actionEvent(form({ cutoff_hour: "30", preorder_window_days: "7" })),
    );
    expect(res).toMatchObject({ status: 400, data: { error: "截單時間需為 0–23 之間的整數" } });
  });

  it("rejects out-of-range window days", async () => {
    const res = await actions.save!(
      actionEvent(form({ cutoff_hour: "12", preorder_window_days: "40" })),
    );
    expect(res).toMatchObject({ status: 400, data: { error: "預購開放天數需為 1–30 之間的整數" } });
  });

  it("returns API error detail", async () => {
    mockClient.PUT.mockResolvedValue({ error: { detail: "server says no" } });
    const res = await actions.save!(
      actionEvent(form({ cutoff_hour: "12", preorder_window_days: "7" })),
    );
    expect(res).toMatchObject({ status: 400, data: { error: "server says no" } });
  });

  it("falls back to generic message when error has no detail", async () => {
    mockClient.PUT.mockResolvedValue({ error: {} });
    const res = await actions.save!(
      actionEvent(form({ cutoff_hour: "12", preorder_window_days: "7" })),
    );
    expect(res).toMatchObject({ status: 400, data: { error: "儲存設定失敗，請稍後再試。" } });
  });
});

describe("settings.savePlants branches", () => {
  it("rejects unauthenticated", async () => {
    const res = await actions.savePlants!(actionEvent(form({}), null));
    expect(res).toMatchObject({ status: 401 });
  });

  it("saves selected plants", async () => {
    mockClient.PUT.mockResolvedValue({ data: {} });
    const res = await actions.savePlants!(actionEvent(form({ plants: ["P1", "P2"] })));
    expect(res).toEqual({ plantsOk: true });
    expect(mockClient.PUT).toHaveBeenCalledWith(
      "/api/merchant/plants",
      expect.objectContaining({ body: { plants: ["P1", "P2"] } }),
    );
  });

  it("returns API error detail", async () => {
    mockClient.PUT.mockResolvedValue({ error: { detail: "bad plants" } });
    const res = await actions.savePlants!(actionEvent(form({ plants: ["P1"] })));
    expect(res).toMatchObject({ status: 400, data: { error: "bad plants" } });
  });

  it("falls back to generic message on error without detail", async () => {
    mockClient.PUT.mockResolvedValue({ error: {} });
    const res = await actions.savePlants!(actionEvent(form({ plants: [] })));
    expect(res).toMatchObject({ status: 400, data: { error: "儲存服務廠區失敗，請稍後再試。" } });
  });
});
