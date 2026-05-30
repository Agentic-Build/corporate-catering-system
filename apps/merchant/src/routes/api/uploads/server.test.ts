import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";

vi.mock("$lib/server/env", () => ({ API_BASE_URL: "http://api.test" }));

import { POST } from "./+server";

const USER = { id: "u1", role: "vendor_operator" };

function event(fd: FormData, locals: Record<string, unknown> = { user: USER, apiToken: "tok" }) {
  return { request: { formData: async () => fd }, locals } as never;
}

const fetchMock = vi.fn();

beforeEach(() => {
  fetchMock.mockReset();
  vi.stubGlobal("fetch", fetchMock);
});
afterEach(() => {
  vi.unstubAllGlobals();
});

describe("uploads POST proxy", () => {
  it("401s when unauthenticated", async () => {
    const fd = new FormData();
    await expect(POST(event(fd, {}))).rejects.toMatchObject({ status: 401 });
  });

  it("400s when no file provided", async () => {
    const fd = new FormData();
    fd.append("file", "not-a-file");
    await expect(POST(event(fd))).rejects.toMatchObject({ status: 400 });
  });

  it("forwards the file with bearer auth and returns the url JSON", async () => {
    fetchMock.mockResolvedValue({ ok: true, json: async () => ({ url: "http://files/x.png" }) });
    const fd = new FormData();
    fd.append("file", new File([new Uint8Array([1])], "x.png"));
    const res = await POST(event(fd));
    expect(await res.json()).toEqual({ url: "http://files/x.png" });
    const [url, init] = fetchMock.mock.calls[0]!;
    expect(url).toBe("http://api.test/api/merchant/uploads");
    expect(init.method).toBe("POST");
    expect(init.headers).toEqual({ Authorization: "Bearer tok" });
    expect(init.body).toBeInstanceOf(FormData);
  });

  it("sends no auth header when there is no apiToken", async () => {
    fetchMock.mockResolvedValue({ ok: true, json: async () => ({ url: "u" }) });
    const fd = new FormData();
    fd.append("file", new File([new Uint8Array([1])], "x.png"));
    await POST(event(fd, { user: USER }));
    expect(fetchMock.mock.calls[0]![1].headers).toEqual({});
  });

  it("propagates an upstream error with its detail text", async () => {
    fetchMock.mockResolvedValue({ ok: false, status: 413, text: async () => "too big" });
    const fd = new FormData();
    fd.append("file", new File([new Uint8Array([1])], "x.png"));
    await expect(POST(event(fd))).rejects.toMatchObject({ status: 413 });
  });

  it("falls back to a generic message when upstream gives no detail", async () => {
    fetchMock.mockResolvedValue({ ok: false, status: 500, text: async () => "" });
    const fd = new FormData();
    fd.append("file", new File([new Uint8Array([1])], "x.png"));
    await expect(POST(event(fd))).rejects.toMatchObject({ status: 500 });
  });
});
