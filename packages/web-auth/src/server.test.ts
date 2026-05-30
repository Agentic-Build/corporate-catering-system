import { describe, it, expect, vi, beforeEach } from "vitest";

const { mockApiClient } = vi.hoisted(() => ({
  mockApiClient: { GET: vi.fn() },
}));
vi.mock("@tbite/api-client", () => ({
  createApiClient: vi.fn(() => mockApiClient),
}));

import { createApiClient } from "@tbite/api-client";
import {
  COOKIE_NAME,
  getToken,
  setSessionCookie,
  clearSessionCookie,
  createAuthHandle,
  requireRole,
} from "./server";
import type { AuthOptions, SessionUser } from "./types";

function cookieEvent() {
  const cookies = {
    get: vi.fn(),
    set: vi.fn(),
    delete: vi.fn(),
  };
  return { cookies, locals: {} as Record<string, unknown> } as never as {
    cookies: { get: ReturnType<typeof vi.fn>; set: ReturnType<typeof vi.fn>; delete: ReturnType<typeof vi.fn> };
    locals: Record<string, unknown>;
  };
}

const USER: SessionUser = {
  user_id: "u1",
  email: "a@b.c",
  display_name: "A",
  role: "employee",
};

beforeEach(() => {
  mockApiClient.GET.mockReset();
  vi.mocked(createApiClient).mockClear();
});

describe("getToken", () => {
  it("reads the default cookie name when none provided", () => {
    const ev = cookieEvent();
    ev.cookies.get.mockReturnValue("tok");
    expect(getToken(ev as never)).toBe("tok");
    expect(ev.cookies.get).toHaveBeenCalledWith(COOKIE_NAME);
  });

  it("reads a custom cookie name", () => {
    const ev = cookieEvent();
    ev.cookies.get.mockReturnValue("tok2");
    expect(getToken(ev as never, "custom")).toBe("tok2");
    expect(ev.cookies.get).toHaveBeenCalledWith("custom");
  });
});

describe("setSessionCookie", () => {
  it("uses provided cookieName/secure/domain", () => {
    const ev = cookieEvent();
    const opts: AuthOptions = {
      apiBaseUrl: "",
      cookieName: "sid",
      cookieSecure: false,
      cookieDomain: "example.com",
    };
    setSessionCookie(ev as never, "T", opts);
    expect(ev.cookies.set).toHaveBeenCalledWith("sid", "T", {
      path: "/",
      httpOnly: true,
      sameSite: "lax",
      secure: false,
      maxAge: 60 * 60 * 24 * 7,
      domain: "example.com",
    });
  });

  it("defaults cookieName to COOKIE_NAME and secure to true", () => {
    const ev = cookieEvent();
    setSessionCookie(ev as never, "T", { apiBaseUrl: "" });
    expect(ev.cookies.set).toHaveBeenCalledWith(
      COOKIE_NAME,
      "T",
      expect.objectContaining({ secure: true, domain: undefined }),
    );
  });
});

describe("clearSessionCookie", () => {
  it("deletes the provided cookie name with domain", () => {
    const ev = cookieEvent();
    clearSessionCookie(ev as never, { apiBaseUrl: "", cookieName: "sid", cookieDomain: "d.com" });
    expect(ev.cookies.delete).toHaveBeenCalledWith("sid", { path: "/", domain: "d.com" });
  });

  it("defaults to COOKIE_NAME", () => {
    const ev = cookieEvent();
    clearSessionCookie(ev as never, { apiBaseUrl: "" });
    expect(ev.cookies.delete).toHaveBeenCalledWith(COOKIE_NAME, { path: "/", domain: undefined });
  });
});

describe("createAuthHandle", () => {
  const opts: AuthOptions = { apiBaseUrl: "http://api", cookieName: "sid" };

  it("resolves user when token present and /me succeeds", async () => {
    const ev = cookieEvent();
    ev.cookies.get.mockReturnValue("tok");
    mockApiClient.GET.mockResolvedValue({ data: USER, error: undefined });
    const resolve = vi.fn().mockResolvedValue("RESPONSE");

    const handle = createAuthHandle(opts);
    const out = await handle({ event: ev as never, resolve } as never);

    expect(createApiClient).toHaveBeenCalledWith("http://api", "tok");
    expect(mockApiClient.GET).toHaveBeenCalledWith("/me", {});
    expect(ev.locals.user).toEqual(USER);
    expect(ev.locals.apiToken).toBe("tok");
    expect(out).toBe("RESPONSE");
  });

  it("clears the cookie when /me returns an error", async () => {
    const ev = cookieEvent();
    ev.cookies.get.mockReturnValue("tok");
    mockApiClient.GET.mockResolvedValue({ data: undefined, error: { code: 401 } });
    const resolve = vi.fn().mockResolvedValue("R");

    await createAuthHandle(opts)({ event: ev as never, resolve } as never);

    expect(ev.cookies.delete).toHaveBeenCalledWith("sid", { path: "/", domain: undefined });
    expect(ev.locals.user).toBeNull();
    expect(ev.locals.apiToken).toBe("tok");
  });

  it("leaves user null without clearing when data and error both falsy", async () => {
    const ev = cookieEvent();
    ev.cookies.get.mockReturnValue("tok");
    mockApiClient.GET.mockResolvedValue({ data: undefined, error: undefined });
    const resolve = vi.fn().mockResolvedValue("R");

    await createAuthHandle(opts)({ event: ev as never, resolve } as never);

    expect(ev.cookies.delete).not.toHaveBeenCalled();
    expect(ev.locals.user).toBeNull();
  });

  it("skips the API call entirely when no token", async () => {
    const ev = cookieEvent();
    ev.cookies.get.mockReturnValue(undefined);
    const resolve = vi.fn().mockResolvedValue("R");

    await createAuthHandle(opts)({ event: ev as never, resolve } as never);

    expect(createApiClient).not.toHaveBeenCalled();
    expect(ev.locals.user).toBeNull();
    expect(ev.locals.apiToken).toBeUndefined();
  });

  it("uses COOKIE_NAME default when opts.cookieName is omitted", async () => {
    const ev = cookieEvent();
    ev.cookies.get.mockReturnValue(undefined);
    const resolve = vi.fn().mockResolvedValue("R");

    await createAuthHandle({ apiBaseUrl: "http://api" })({ event: ev as never, resolve } as never);

    expect(ev.cookies.get).toHaveBeenCalledWith(COOKIE_NAME);
  });
});

describe("requireRole", () => {
  it("returns the user when role matches", () => {
    expect(requireRole({ user: USER }, "employee")).toBe(USER);
  });

  it("throws unauthenticated when no user", () => {
    expect(() => requireRole({ user: null }, "employee")).toThrow("unauthenticated");
  });

  it("throws forbidden when role mismatches", () => {
    expect(() => requireRole({ user: USER }, "welfare_admin")).toThrow("forbidden");
  });
});
