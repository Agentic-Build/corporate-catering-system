import { describe, it, expect, vi, beforeEach } from "vitest";

const { createApiClient } = vi.hoisted(() => ({
  createApiClient: vi.fn(() => ({ tag: "client" })),
}));
vi.mock("$lib/server/env", () => ({ API_BASE_URL: "http://custom" }));
vi.mock("@tbite/api-client", () => ({ createApiClient }));

import { apiFor } from "./api";

beforeEach(() => {
  createApiClient.mockClear();
});

describe("apiFor", () => {
  it("builds a client with the base url and token", () => {
    const client = apiFor("tok");
    expect(createApiClient).toHaveBeenCalledWith("http://custom", "tok");
    expect(client).toEqual({ tag: "client" });
  });
  it("passes undefined token through", () => {
    apiFor(undefined);
    expect(createApiClient).toHaveBeenCalledWith("http://custom", undefined);
  });
});
