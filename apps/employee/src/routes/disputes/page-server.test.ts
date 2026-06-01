import { describe, it, expect, vi, beforeEach } from "vitest";

const { mockClient } = vi.hoisted(() => ({ mockClient: { GET: vi.fn() } }));
vi.mock("$lib/server/env", () => ({ API_BASE_URL: "http://x" }));
vi.mock("@tbite/api-client", () => ({ createApiClient: () => mockClient }));

import { load } from "./+page.server";

function loadEvent(user: unknown = { id: "u1" }) {
  return { locals: { user, apiToken: "t" }, url: new URL("http://h/disputes") } as never;
}
beforeEach(() => mockClient.GET.mockReset());

describe("disputes load", () => {
  it("redirects to login when unauthenticated", async () => {
    await expect(load(loadEvent(null))).rejects.toMatchObject({
      status: 303,
      location: "/login?return_to=%2Fdisputes",
    });
  });
  it("returns disputes from data", async () => {
    mockClient.GET.mockResolvedValue({ data: { items: [{ id: "d1" }] } });
    expect(((await load(loadEvent())) as { disputes: unknown[] }).disputes).toEqual([{ id: "d1" }]);
  });
  it("defaults missing items to empty", async () => {
    mockClient.GET.mockResolvedValue({ data: {} });
    expect(((await load(loadEvent())) as { disputes: unknown[] }).disputes).toEqual([]);
  });
  it("returns empty disputes when response carries no data (no throw needed)", async () => {
    mockClient.GET.mockResolvedValue({});
    expect(((await load(loadEvent())) as { disputes: unknown[] }).disputes).toEqual([]);
  });
});
