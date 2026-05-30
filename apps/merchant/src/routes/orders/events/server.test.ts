import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";

vi.mock("$lib/server/env", () => ({ API_BASE_URL: "http://api.test" }));

import { GET } from "./+server";

const VENDOR = { role: "vendor_operator" };

function event(locals: Record<string, unknown>, signal: AbortSignal = new AbortController().signal) {
  return { locals, request: { signal } } as never;
}

const fetchMock = vi.fn();

beforeEach(() => {
  fetchMock.mockReset();
  vi.stubGlobal("fetch", fetchMock);
});
afterEach(() => {
  vi.unstubAllGlobals();
});

describe("orders events SSE proxy", () => {
  it("403s for non-vendor users", async () => {
    await expect(GET(event({ user: { role: "employee" } }))).rejects.toMatchObject({ status: 403 });
  });

  it("403s when there is no user", async () => {
    await expect(GET(event({}))).rejects.toMatchObject({ status: 403 });
  });

  it("502s when upstream is not ok", async () => {
    fetchMock.mockResolvedValue({ ok: false, body: null });
    await expect(GET(event({ user: VENDOR, apiToken: "t" }))).rejects.toMatchObject({ status: 502 });
  });

  it("502s when upstream has no body", async () => {
    fetchMock.mockResolvedValue({ ok: true, body: null });
    await expect(GET(event({ user: VENDOR, apiToken: "t" }))).rejects.toMatchObject({ status: 502 });
  });

  it("streams the upstream body with SSE headers and forwards token + signal", async () => {
    const body = new ReadableStream();
    fetchMock.mockResolvedValue({ ok: true, body });
    const signal = new AbortController().signal;
    const res = await GET(event({ user: VENDOR, apiToken: "tok" }, signal));
    expect(res.headers.get("content-type")).toBe("text/event-stream");
    expect(res.headers.get("cache-control")).toBe("no-cache");
    const [url, init] = fetchMock.mock.calls[0]!;
    expect(url).toBe("http://api.test/api/merchant/orders/events");
    expect(init.headers).toEqual({ authorization: "Bearer tok" });
    expect(init.signal).toBe(signal);
  });

  it("sends an empty bearer token when apiToken is absent", async () => {
    fetchMock.mockResolvedValue({ ok: true, body: new ReadableStream() });
    await GET(event({ user: VENDOR }));
    expect(fetchMock.mock.calls[0]![1].headers).toEqual({ authorization: "Bearer " });
  });
});
