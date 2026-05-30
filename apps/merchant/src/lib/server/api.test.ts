import { describe, it, expect, vi } from "vitest";

vi.mock("$lib/server/env", () => ({ API_BASE_URL: "http://api.test" }));

const { createApiClient } = vi.hoisted(() => ({ createApiClient: vi.fn(() => ({ tag: "client" })) }));
vi.mock("@tbite/api-client", () => ({ createApiClient }));

import { apiFor } from "./api";

describe("apiFor", () => {
  it("builds a client for the configured base url with the given token", () => {
    const client = apiFor("tok");
    expect(client).toEqual({ tag: "client" });
    expect(createApiClient).toHaveBeenCalledWith("http://api.test", "tok");
  });

  it("passes through an undefined token", () => {
    apiFor(undefined);
    expect(createApiClient).toHaveBeenCalledWith("http://api.test", undefined);
  });
});
