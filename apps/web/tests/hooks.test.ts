import assert from "node:assert/strict";
import { describe, it } from "node:test";

import { handle } from "../src/hooks.server";

import { createRequestEvent, MemoryCookies } from "./helpers";

describe("auth middleware hooks", () => {
  it("rejects guarded API-style requests without an authenticated actor", async () => {
    const cookies = new MemoryCookies();
    const event = createRequestEvent("/admin", cookies, {
      headers: { accept: "application/json" }
    });

    await expectHttpStatus(
      Promise.resolve(
        handle({
          event,
          resolve: async () => new Response("ok")
        } as never)
      ),
      401
    );
  });

  it("soft-redirects unauthenticated page navigation back to the sign-in landing", async () => {
    const cookies = new MemoryCookies();
    const event = createRequestEvent("/admin", cookies, {
      headers: { accept: "text/html" }
    });

    await expectRedirect(
      Promise.resolve(
        handle({
          event,
          resolve: async () => new Response("ok")
        } as never)
      ),
      303,
      /^\/\?flash=auth-required&next=/
    );
  });

  it("rejects guarded API-style requests for roles outside guard policy", async () => {
    const cookies = new MemoryCookies();
    const event = createRequestEvent("/admin", cookies, {
      headers: {
        "x-mock-role": "employee",
        accept: "application/json"
      }
    });

    await expectHttpStatus(
      Promise.resolve(
        handle({
          event,
          resolve: async () => new Response("ok")
        } as never)
      ),
      403
    );
  });

  it("soft-redirects cross-role page navigation to the actor's own portal", async () => {
    const cookies = new MemoryCookies();
    const event = createRequestEvent("/admin", cookies, {
      headers: {
        "x-mock-role": "employee",
        accept: "text/html"
      }
    });

    await expectRedirect(
      Promise.resolve(
        handle({
          event,
          resolve: async () => new Response("ok")
        } as never)
      ),
      303,
      /^\/employee\?flash=cross-role&attempted=/
    );
  });

  it("rejects scoped vendor paths outside actor vendor scope", async () => {
    const cookies = new MemoryCookies();
    const event = createRequestEvent("/vendor/vendors/ven-out-of-scope", cookies, {
      headers: {
        "x-mock-role": "vendor"
      }
    });

    await expectHttpStatus(
      Promise.resolve(
        handle({
          event,
          resolve: async () => new Response("ok")
        } as never)
      ),
      403
    );
  });

  it("returns 400 for malformed encoded scoped path segments", async () => {
    const cookies = new MemoryCookies();
    const event = createRequestEvent("/vendor/vendors/%E0%A4%A", cookies, {
      headers: {
        "x-mock-role": "vendor"
      }
    });

    await expectHttpStatus(
      Promise.resolve(
        handle({
          event,
          resolve: async () => new Response("ok")
        } as never)
      ),
      400
    );
  });

  it("allows guarded route when role and scope are valid", async () => {
    const cookies = new MemoryCookies();
    const event = createRequestEvent("/vendor/vendors/ven-load-gate-a", cookies, {
      headers: {
        "x-mock-role": "vendor"
      }
    });

    let resolveCalls = 0;
    const response = await handle({
      event,
      resolve: async () => {
        resolveCalls += 1;
        return new Response("ok");
      }
    } as never);

    assert.equal(response.status, 200);
    assert.equal(resolveCalls, 1);
    assert.equal(event.locals.actor?.role, "vendor");
    assert.ok(event.locals.actor?.scope.vendorIds.includes("ven-load-gate-a"));
  });

  it("applies no-store cache policy to dynamic route responses", async () => {
    const cookies = new MemoryCookies();
    const event = createRequestEvent("/employee", cookies, {
      headers: {
        "x-mock-role": "employee"
      }
    });

    const response = await handle({
      event,
      resolve: async () => new Response("ok")
    } as never);

    assert.equal(response.headers.get("cache-control"), "no-store");
  });

  it("applies immutable cache policy to built immutable asset paths", async () => {
    const cookies = new MemoryCookies();
    const event = createRequestEvent("/_app/immutable/chunks/main.js", cookies);

    const response = await handle({
      event,
      resolve: async () => new Response("asset")
    } as never);

    assert.equal(
      response.headers.get("cache-control"),
      "public, max-age=31536000, immutable"
    );
  });
});

async function expectHttpStatus(promise: Promise<unknown>, status: number) {
  await assert.rejects(promise, (error: unknown) => {
    if (!error || typeof error !== "object") {
      return false;
    }

    return (error as { status?: number }).status === status;
  });
}

async function expectRedirect(
  promise: Promise<unknown>,
  status: number,
  locationPattern: RegExp
) {
  await assert.rejects(promise, (error: unknown) => {
    if (!error || typeof error !== "object") {
      return false;
    }
    const err = error as { status?: number; location?: string };
    return err.status === status && typeof err.location === "string" && locationPattern.test(err.location);
  });
}
