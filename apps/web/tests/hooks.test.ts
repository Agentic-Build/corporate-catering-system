import assert from "node:assert/strict";
import { describe, it } from "node:test";

import { handle } from "../src/hooks.server";

import { createRequestEvent, MemoryCookies } from "./helpers";

describe("auth middleware hooks", () => {
  it("rejects guarded routes without an authenticated actor", async () => {
    const cookies = new MemoryCookies();
    const event = createRequestEvent("/admin", cookies);

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

  it("rejects guarded routes for roles outside guard policy", async () => {
    const cookies = new MemoryCookies();
    const event = createRequestEvent("/admin", cookies, {
      headers: {
        "x-mock-role": "employee"
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
    const event = createRequestEvent("/vendor/vendors/ven-mock-001", cookies, {
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
    assert.ok(event.locals.actor?.scope.vendorIds.includes("ven-mock-001"));
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
