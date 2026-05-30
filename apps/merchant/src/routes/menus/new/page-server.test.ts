import { describe, it, expect, vi, beforeEach } from "vitest";

const { mockClient } = vi.hoisted(() => ({
  mockClient: { POST: vi.fn() },
}));
vi.mock("$lib/server/api", () => ({ apiFor: () => mockClient }));

import { actions, load } from "./+page.server";

const VENDOR = { id: "u1", role: "vendor_operator" };

function loadEvent(user: unknown = VENDOR) {
  return { locals: { user, apiToken: "t" }, url: new URL("http://x/menus/new") } as never;
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
  mockClient.POST.mockReset();
});

describe("menus/new load", () => {
  it("redirects unauthenticated", async () => {
    await expect(
      load({ locals: {}, url: new URL("http://x/menus/new") } as never),
    ).rejects.toMatchObject({
      status: 303,
      location: "/login?return_to=%2Fmenus%2Fnew",
    });
  });

  it("returns the user when authed", async () => {
    const res = (await load(loadEvent())) as { user: unknown };
    expect(res.user).toEqual(VENDOR);
  });
});

describe("menus/new default action", () => {
  it("fails when name empty", async () => {
    const res = await actions.default!(actionEvent(form({ name: "  ", price: "10" })));
    expect(res).toMatchObject({ status: 400, data: { error: "name 必填" } });
  });

  it("fails when price not a number", async () => {
    const res = await actions.default!(actionEvent(form({ name: "Dish", price: "abc" })));
    expect(res).toMatchObject({ status: 400, data: { error: "price 非數字" } });
  });

  it("fails when price negative", async () => {
    const res = await actions.default!(actionEvent(form({ name: "Dish", price: "-5" })));
    expect(res).toMatchObject({ status: 400, data: { error: "price 非數字" } });
  });

  it("creates with tags + images and redirects", async () => {
    mockClient.POST.mockResolvedValue({ data: {} });
    await expect(
      actions.default!(
        actionEvent(
          form({
            name: " Dish ",
            description: " yum ",
            price: "120",
            tags: "spicy  vegan",
            images: JSON.stringify(["a.png", 2]),
          }),
        ),
      ),
    ).rejects.toMatchObject({ status: 303, location: "/menus" });
    expect(mockClient.POST).toHaveBeenCalledWith(
      "/api/merchant/menu-items",
      expect.objectContaining({
        body: expect.objectContaining({
          name: "Dish",
          description: "yum",
          price_minor: 120,
          tags: ["spicy", "vegan"],
          images: ["a.png"],
        }),
      }),
    );
  });

  it("uses empty tags when tags blank and empty images on invalid JSON", async () => {
    mockClient.POST.mockResolvedValue({ data: {} });
    await expect(
      actions.default!(actionEvent(form({ name: "Dish", price: "0", tags: "", images: "nope" }))),
    ).rejects.toMatchObject({ status: 303 });
    const body = mockClient.POST.mock.calls[0]![1].body;
    expect(body.tags).toEqual([]);
    expect(body.images).toEqual([]);
  });

  it("treats non-array JSON images as empty", async () => {
    mockClient.POST.mockResolvedValue({ data: {} });
    await expect(
      actions.default!(actionEvent(form({ name: "Dish", price: "0", tags: "a", images: "5" }))),
    ).rejects.toMatchObject({ status: 303 });
    expect(mockClient.POST.mock.calls[0]![1].body.images).toEqual([]);
  });

  it("returns 500 on API error", async () => {
    mockClient.POST.mockResolvedValue({ error: { detail: "boom" } });
    const res = await actions.default!(actionEvent(form({ name: "Dish", price: "10" })));
    expect(res).toMatchObject({ status: 500 });
  });
});
