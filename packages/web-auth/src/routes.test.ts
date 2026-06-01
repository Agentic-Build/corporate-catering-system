import { describe, it, expect, vi, beforeEach } from "vitest";

// redirect/error throw tagged objects so we can assert on them via .rejects.
vi.mock("@sveltejs/kit", () => ({
  redirect: (status: number, location: string) => ({ __redirect: true, status, location }),
  error: (status: number, message: string) => ({ __error: true, status, message }),
}));

const { mockServer } = vi.hoisted(() => ({
  mockServer: {
    getToken: vi.fn(),
    setSessionCookie: vi.fn(),
    clearSessionCookie: vi.fn(),
  },
}));
vi.mock("./server", () => mockServer);

import {
  createAuthStartHandler,
  createAuthLandingHandler,
  createAuthLogoutHandler,
  type AuthRouteOptions,
} from "./routes";

const OPTS: AuthRouteOptions = {
  portal: "merchant",
  cookieName: "sid",
  apiBaseUrl: "http://api",
  cookieDomain: "d.com",
  cookieSecure: false,
};

const fetchMock = vi.fn();

beforeEach(() => {
  mockServer.getToken.mockReset();
  mockServer.setSessionCookie.mockReset();
  mockServer.clearSessionCookie.mockReset();
  fetchMock.mockReset();
  vi.stubGlobal("fetch", fetchMock);
});

function startEvent(qs: string) {
  return { url: new URL(`http://x/auth/start${qs}`) } as never;
}

describe("createAuthStartHandler", () => {
  it("rejects an invalid provider with 400", async () => {
    const handler = createAuthStartHandler(OPTS);
    await expect(handler(startEvent("?provider=BAD!!"))).rejects.toMatchObject({
      __error: true,
      status: 400,
    });
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("rejects a missing provider with 400", async () => {
    const handler = createAuthStartHandler(OPTS);
    await expect(handler(startEvent(""))).rejects.toMatchObject({ status: 400 });
  });

  it("rejects 502 when upstream auth start fails", async () => {
    fetchMock.mockResolvedValue({ ok: false });
    const handler = createAuthStartHandler(OPTS);
    await expect(handler(startEvent("?provider=google"))).rejects.toMatchObject({
      __error: true,
      status: 502,
    });
  });

  it("redirects to auth_url and defaults return_to to /", async () => {
    fetchMock.mockResolvedValue({ ok: true, json: async () => ({ auth_url: "http://idp/login" }) });
    const handler = createAuthStartHandler(OPTS);
    await expect(handler(startEvent("?provider=google"))).rejects.toMatchObject({
      __redirect: true,
      status: 303,
      location: "http://idp/login",
    });
    expect(fetchMock).toHaveBeenCalledWith(
      "http://api/auth/google/start",
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({ app: "merchant", return_to: "/" }),
      }),
    );
  });

  it("forwards the return_to query param", async () => {
    fetchMock.mockResolvedValue({ ok: true, json: async () => ({ auth_url: "http://idp" }) });
    const handler = createAuthStartHandler(OPTS);
    await expect(
      handler(startEvent("?provider=oidc.azure&return_to=/dashboard")),
    ).rejects.toMatchObject({ status: 303 });
    expect(fetchMock).toHaveBeenCalledWith(
      "http://api/auth/oidc.azure/start",
      expect.objectContaining({
        body: JSON.stringify({ app: "merchant", return_to: "/dashboard" }),
      }),
    );
  });
});

function landingEvent(qs: string, eventFetch = vi.fn()) {
  return { url: new URL(`http://x/auth/landing${qs}`), fetch: eventFetch } as never;
}

describe("createAuthLandingHandler", () => {
  it("exchanges code for a token then sets cookie and redirects to return_to", async () => {
    const eventFetch = vi.fn().mockResolvedValue({ ok: true, json: async () => ({ token: "T" }) });
    const ev = landingEvent("?code=otc&return_to=/home", eventFetch);
    const handler = createAuthLandingHandler(OPTS);

    await expect(handler(ev)).rejects.toMatchObject({
      __redirect: true,
      status: 303,
      location: "/home",
    });
    expect(eventFetch).toHaveBeenCalledWith(
      "http://api/auth/session",
      expect.objectContaining({ method: "POST", body: JSON.stringify({ code: "otc" }) }),
    );
    expect(mockServer.setSessionCookie).toHaveBeenCalledWith(ev, "T", {
      apiBaseUrl: "",
      cookieSecure: false,
      cookieDomain: "d.com",
      cookieName: "sid",
    });
  });

  it("rejects 401 when code exchange fails", async () => {
    const eventFetch = vi.fn().mockResolvedValue({ ok: false });
    const handler = createAuthLandingHandler(OPTS);
    await expect(handler(landingEvent("?code=otc", eventFetch))).rejects.toMatchObject({
      __error: true,
      status: 401,
    });
  });

  it("rejects 400 when no code and no token", async () => {
    const handler = createAuthLandingHandler(OPTS);
    await expect(handler(landingEvent(""))).rejects.toMatchObject({
      __error: true,
      status: 400,
    });
    expect(mockServer.setSessionCookie).not.toHaveBeenCalled();
  });

  it("uses legacy token query param when no code present", async () => {
    const ev = landingEvent("?token=LEGACY&return_to=/x");
    const handler = createAuthLandingHandler(OPTS);
    await expect(handler(ev)).rejects.toMatchObject({ status: 303, location: "/x" });
    expect(mockServer.setSessionCookie).toHaveBeenCalledWith(ev, "LEGACY", expect.any(Object));
  });

  it("rejects 400 when code exchange returns no token", async () => {
    const eventFetch = vi.fn().mockResolvedValue({ ok: true, json: async () => ({}) });
    const handler = createAuthLandingHandler(OPTS);
    await expect(handler(landingEvent("?code=otc", eventFetch))).rejects.toMatchObject({
      status: 400,
    });
  });

  it("defaults secure to true when cookieSecure omitted", async () => {
    const ev = landingEvent("?token=L");
    const handler = createAuthLandingHandler({
      portal: "admin",
      cookieName: "sid",
      apiBaseUrl: "http://api",
    });
    await expect(handler(ev)).rejects.toMatchObject({ status: 303, location: "/" });
    expect(mockServer.setSessionCookie).toHaveBeenCalledWith(
      ev,
      "L",
      expect.objectContaining({ cookieSecure: true, cookieDomain: undefined }),
    );
  });
});

function logoutEvent() {
  return { url: new URL("http://x/auth/logout"), fetch: vi.fn() } as never;
}

describe("createAuthLogoutHandler", () => {
  it("revokes upstream when a token exists then clears cookie and redirects to /login", async () => {
    mockServer.getToken.mockReturnValue("tok");
    fetchMock.mockResolvedValue({ ok: true });
    const ev = logoutEvent();
    const handler = createAuthLogoutHandler(OPTS);

    await expect(handler(ev)).rejects.toMatchObject({
      __redirect: true,
      status: 303,
      location: "/login",
    });
    expect(fetchMock).toHaveBeenCalledWith(
      "http://api/auth/logout",
      expect.objectContaining({ method: "POST", headers: { Authorization: "Bearer tok" } }),
    );
    expect(mockServer.clearSessionCookie).toHaveBeenCalledWith(ev, {
      apiBaseUrl: "",
      cookieSecure: false,
      cookieDomain: "d.com",
      cookieName: "sid",
    });
  });

  it("skips upstream revoke when no token but still clears and redirects", async () => {
    mockServer.getToken.mockReturnValue(undefined);
    const ev = logoutEvent();
    const handler = createAuthLogoutHandler(OPTS);

    await expect(handler(ev)).rejects.toMatchObject({ status: 303, location: "/login" });
    expect(fetchMock).not.toHaveBeenCalled();
    expect(mockServer.clearSessionCookie).toHaveBeenCalled();
  });

  it("defaults secure to true when cookieSecure omitted", async () => {
    mockServer.getToken.mockReturnValue(undefined);
    const ev = logoutEvent();
    const handler = createAuthLogoutHandler({
      portal: "employee",
      cookieName: "sid",
      apiBaseUrl: "http://api",
    });
    await expect(handler(ev)).rejects.toMatchObject({ status: 303 });
    expect(mockServer.clearSessionCookie).toHaveBeenCalledWith(
      ev,
      expect.objectContaining({ cookieSecure: true, cookieDomain: undefined }),
    );
  });
});
