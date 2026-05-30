import { describe, it, expect, vi, beforeEach } from "vitest";

const { mockClient } = vi.hoisted(() => ({ mockClient: { GET: vi.fn() } }));
vi.mock("$lib/server/env", () => ({ API_BASE_URL: "http://x" }));
vi.mock("@tbite/api-client", () => ({ createApiClient: () => mockClient }));

import { load } from "./+page.server";

function loadEvent(user: unknown = { id: "u1" }) {
  return { locals: { user, apiToken: "t" }, url: new URL("http://h/orders") } as never;
}
beforeEach(() => mockClient.GET.mockReset());

describe("orders load", () => {
  it("redirects to login when unauthenticated", async () => {
    await expect(load(loadEvent(null))).rejects.toMatchObject({
      status: 303,
      location: "/login?return_to=%2Forders",
    });
  });
  it("returns orders from data", async () => {
    mockClient.GET.mockResolvedValue({ data: { items: [{ id: "o1" }] } });
    expect((await load(loadEvent())).orders).toEqual([{ id: "o1" }]);
  });
  it("defaults missing items to empty", async () => {
    mockClient.GET.mockResolvedValue({ data: {} });
    expect((await load(loadEvent())).orders).toEqual([]);
  });
  it("returns empty orders when response carries no data (no throw needed)", async () => {
    mockClient.GET.mockResolvedValue({});
    expect((await load(loadEvent())).orders).toEqual([]);
  });
});
