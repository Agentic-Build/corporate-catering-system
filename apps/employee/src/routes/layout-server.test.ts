import { describe, it, expect, vi, beforeEach } from "vitest";

const { mockClient } = vi.hoisted(() => ({
  mockClient: { GET: vi.fn(), POST: vi.fn(), PUT: vi.fn(), DELETE: vi.fn() },
}));
vi.mock("$lib/server/env", () => ({ API_BASE_URL: "http://x" }));
vi.mock("@tbite/api-client", () => ({ createApiClient: () => mockClient }));

import { load } from "./+layout.server";

const USER = { id: "u1", role: "employee" };

function event(user: unknown = USER) {
  return { locals: { user, apiToken: "t" } } as never;
}

beforeEach(() => {
  mockClient.GET.mockReset();
});

describe("+layout.server load", () => {
  it("returns anonymous shape when no user", async () => {
    const res = await load(event(null));
    expect(res).toEqual({ user: null, activeOrders: 0, plants: [] });
    expect(mockClient.GET).not.toHaveBeenCalled();
  });

  it("counts active orders and maps plants", async () => {
    mockClient.GET.mockImplementation((path: string) =>
      Promise.resolve(
        path === "/api/employee/orders"
          ? {
              data: {
                items: [
                  { status: "placed" },
                  { status: "cutoff" },
                  { status: "picked_up" },
                ],
              },
            }
          : { data: { items: [{ code: "tn-a", label: "Plant A" }] } },
      ),
    );
    const res = await load(event());
    expect(res.activeOrders).toBe(2);
    expect(res.plants).toEqual([{ id: "tn-a", label: "Plant A" }]);
    expect(res.user).toEqual(USER);
  });

  it("handles rejected requests and missing items gracefully", async () => {
    mockClient.GET.mockImplementation((path: string) =>
      path === "/api/employee/orders"
        ? Promise.reject(new Error("boom"))
        : Promise.resolve({ data: undefined }),
    );
    const res = await load(event());
    expect(res.activeOrders).toBe(0);
    expect(res.plants).toEqual([]);
  });
});
