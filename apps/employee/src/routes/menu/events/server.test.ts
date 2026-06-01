import { describe, it, expect, vi, beforeEach } from "vitest";

vi.mock("$lib/server/env", () => ({ API_BASE_URL: "http://x" }));

import { GET } from "./+server";

function event(user: unknown, signal = new AbortController().signal) {
  return { locals: { user, apiToken: "tok" }, request: { signal } } as never;
}
beforeEach(() => vi.restoreAllMocks());

describe("menu/events GET", () => {
  it("throws 403 when unauthenticated", async () => {
    await expect(GET(event(null))).rejects.toMatchObject({ status: 403 });
  });

  it("throws 502 when upstream not ok", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue({ ok: false, body: null } as Response);
    await expect(GET(event({ id: "u1" }))).rejects.toMatchObject({ status: 502 });
  });

  it("throws 502 when upstream has no body", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue({ ok: true, body: null } as Response);
    await expect(GET(event({ id: "u1" }))).rejects.toMatchObject({ status: 502 });
  });

  it("proxies the upstream stream with SSE headers and bearer token", async () => {
    const body = new ReadableStream();
    const fetchSpy = vi
      .spyOn(globalThis, "fetch")
      .mockResolvedValue({ ok: true, body } as Response);
    const res = await GET(event({ id: "u1" }));
    expect(res).toBeInstanceOf(Response);
    expect(res.headers.get("content-type")).toBe("text/event-stream");
    expect(res.headers.get("cache-control")).toBe("no-cache");
    expect(fetchSpy).toHaveBeenCalledWith(
      "http://x/api/employee/menu/events",
      expect.objectContaining({ headers: { authorization: "Bearer tok" } }),
    );
  });

  it("sends an empty bearer token when apiToken is absent", async () => {
    const fetchSpy = vi
      .spyOn(globalThis, "fetch")
      .mockResolvedValue({ ok: true, body: new ReadableStream() } as Response);
    await GET({ locals: { user: { id: "u1" } }, request: { signal: undefined } } as never);
    expect(fetchSpy).toHaveBeenCalledWith(
      "http://x/api/employee/menu/events",
      expect.objectContaining({ headers: { authorization: "Bearer " } }),
    );
  });
});
