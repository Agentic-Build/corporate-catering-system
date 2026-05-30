import { describe, it, expect, vi, beforeEach } from "vitest";

const { mockClient } = vi.hoisted(() => ({
  mockClient: { GET: vi.fn(), PATCH: vi.fn() },
}));
vi.mock("$lib/server/api", () => ({ apiFor: () => mockClient }));

import { actions, load } from "./+page.server";

const VENDOR = { id: "u1", role: "vendor_operator" };

function loadEvent(id: string, user: unknown = VENDOR) {
  return {
    locals: { user, apiToken: "t" },
    params: { id },
    url: new URL("http://x/menus/" + id),
  } as never;
}
function actionEvent(fd: FormData, id = "m1") {
  return {
    request: { formData: async () => fd },
    params: { id },
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
  mockClient.PATCH.mockReset();
});

describe("menus/[id] load", () => {
  it("redirects unauthenticated", async () => {
    await expect(
      load({ locals: {}, params: { id: "m1" }, url: new URL("http://x/menus/m1") } as never),
    ).rejects.toMatchObject({ status: 303 });
  });

  it("returns the matching item", async () => {
    mockClient.GET.mockResolvedValue({ data: { items: [{ id: "m1", name: "A" }, { id: "m2" }] } });
    const res = (await load(loadEvent("m1"))) as { item: { id: string } };
    expect(res.item.id).toBe("m1");
  });

  it("404s when item not found (and when data missing)", async () => {
    mockClient.GET.mockResolvedValueOnce({ data: { items: [{ id: "m2" }] } });
    await expect(load(loadEvent("m1"))).rejects.toMatchObject({ status: 404 });
    mockClient.GET.mockResolvedValueOnce({ data: null });
    await expect(load(loadEvent("m1"))).rejects.toMatchObject({ status: 404 });
  });
});

describe("menus/[id] update branches", () => {
  it("parses tags/images and redirects on success", async () => {
    mockClient.PATCH.mockResolvedValue({ data: {} });
    await expect(
      actions.update!(
        actionEvent(
          form({
            name: "Dish",
            description: "tasty",
            price: "120",
            tags: "spicy  vegan ",
            images: JSON.stringify(["a.png", 1, "b.png"]),
          }),
        ),
      ),
    ).rejects.toMatchObject({ status: 303, location: "/menus" });
    expect(mockClient.PATCH).toHaveBeenCalledWith(
      "/api/merchant/menu-items/{id}",
      expect.objectContaining({
        params: { path: { id: "m1" } },
        body: expect.objectContaining({
          name: "Dish",
          price_minor: 120,
          tags: ["spicy", "vegan"],
          images: ["a.png", "b.png"],
        }),
      }),
    );
  });

  it("treats invalid JSON images as empty array", async () => {
    mockClient.PATCH.mockResolvedValue({ data: {} });
    await expect(
      actions.update!(actionEvent(form({ name: "X", price: "0", tags: "", images: "{not json" }))),
    ).rejects.toMatchObject({ status: 303 });
    expect(mockClient.PATCH.mock.calls[0]![1].body.images).toEqual([]);
  });

  it("treats non-array JSON images as empty array", async () => {
    mockClient.PATCH.mockResolvedValue({ data: {} });
    await expect(
      actions.update!(actionEvent(form({ name: "X", price: "0", tags: "", images: '{"a":1}' }))),
    ).rejects.toMatchObject({ status: 303 });
    expect(mockClient.PATCH.mock.calls[0]![1].body.images).toEqual([]);
  });

  it("returns 500 on API error", async () => {
    mockClient.PATCH.mockResolvedValue({ error: { detail: "boom" } });
    const res = await actions.update!(actionEvent(form({ name: "X", price: "1", tags: "" })));
    expect(res).toMatchObject({ status: 500 });
  });
});
