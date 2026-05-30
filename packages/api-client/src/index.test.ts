import { describe, it, expect, vi, beforeEach } from "vitest";

const { mockCreateClient, sentinelClient } = vi.hoisted(() => {
  const sentinelClient = { GET: vi.fn(), POST: vi.fn() };
  return { mockCreateClient: vi.fn(() => sentinelClient), sentinelClient };
});

vi.mock("openapi-fetch", () => ({ default: mockCreateClient }));

import { createApiClient } from "./index";

beforeEach(() => {
  mockCreateClient.mockClear();
});

describe("createApiClient", () => {
  it("returns the client produced by openapi-fetch", () => {
    const client = createApiClient("http://api.local");
    expect(client).toBe(sentinelClient);
    expect(mockCreateClient).toHaveBeenCalledTimes(1);
  });

  it("passes the baseUrl through unchanged", () => {
    createApiClient("http://api.local/v1");
    expect(mockCreateClient.mock.calls[0][0]).toMatchObject({ baseUrl: "http://api.local/v1" });
  });

  it("sets an Authorization Bearer header when an access token is given", () => {
    createApiClient("http://api.local", "tok-123");
    const cfg = mockCreateClient.mock.calls[0][0];
    expect(cfg.headers).toEqual({ Authorization: "Bearer tok-123" });
  });

  it("uses empty headers when no access token is given", () => {
    createApiClient("http://api.local");
    const cfg = mockCreateClient.mock.calls[0][0];
    expect(cfg.headers).toEqual({});
  });

  it("uses empty headers when access token is an empty string (falsy)", () => {
    createApiClient("http://api.local", "");
    const cfg = mockCreateClient.mock.calls[0][0];
    expect(cfg.headers).toEqual({});
  });

  it("configures a comma-delimited (explode:false form) array query serializer", () => {
    createApiClient("http://api.local");
    const cfg = mockCreateClient.mock.calls[0][0];
    expect(cfg.querySerializer).toEqual({ array: { style: "form", explode: false } });
  });
});
