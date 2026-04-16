import assert from "node:assert/strict";
import { afterEach, beforeEach, describe, it } from "node:test";

import { type AuthRole, validateActorScope } from "../src/lib/server/auth/contracts";
import { MOCK_AUTH_SESSION_COOKIE_NAME } from "../src/lib/server/auth/mock-provider";
import { createAuthRuntime } from "../src/lib/server/auth/runtime";

import { createRequestEvent, MemoryCookies } from "./helpers";

const ORIGINAL_DATE_NOW = Date.now;
let nowEpochMs = Date.UTC(2026, 0, 1, 0, 0, 0);

describe("auth runtime", () => {
  beforeEach(() => {
    nowEpochMs = Date.UTC(2026, 0, 1, 0, 0, 0);
    Date.now = () => nowEpochMs;
    delete process.env.AUTH_PROVIDER;
    delete process.env.MOCK_AUTH_SIGNING_SECRET;
  });

  afterEach(() => {
    Date.now = ORIGINAL_DATE_NOW;
    delete process.env.AUTH_PROVIDER;
    delete process.env.MOCK_AUTH_SIGNING_SECRET;
  });

  it("issues valid mock sessions for employee/vendor/admin", async () => {
    const roles: AuthRole[] = ["employee", "vendor", "admin"];

    for (const role of roles) {
      const runtime = createAuthRuntime();
      const cookies = new MemoryCookies();

      const context = await runtime.issueDevSession(
        createRequestEvent("/auth/mock", cookies),
        role
      );

      assert.equal(context.actor?.role, role);
      assert.equal(context.provider, "mock");
      assert.equal(validateActorScope(context.actor!), null);
      assert.ok(cookies.get(MOCK_AUTH_SESSION_COOKIE_NAME));
    }
  });

  it("refreshes a session when refresh threshold is reached", async () => {
    const runtime = createAuthRuntime();
    const cookies = new MemoryCookies();

    const initial = await runtime.authenticate(
      createRequestEvent("/employee?mockRole=employee", cookies)
    );

    assert.equal(initial.actor?.role, "employee");
    assert.ok(initial.session);

    const firstSession = initial.session;
    nowEpochMs = firstSession.refreshAfterEpochMs + 1;

    const refreshed = await runtime.authenticate(createRequestEvent("/employee", cookies));

    assert.equal(refreshed.actor?.role, "employee");
    assert.ok(refreshed.session);
    assert.notEqual(refreshed.session.sessionId, firstSession.sessionId);
    assert.ok(refreshed.session.issuedAtEpochMs > firstSession.issuedAtEpochMs);
  });

  it("rejects and clears expired sessions", async () => {
    const runtime = createAuthRuntime();
    const cookies = new MemoryCookies();

    const issued = await runtime.authenticate(createRequestEvent("/vendor?mockRole=vendor", cookies));
    assert.ok(issued.session);

    nowEpochMs = issued.session.expiresAtEpochMs + 1;

    const afterExpiry = await runtime.authenticate(createRequestEvent("/vendor", cookies));

    assert.equal(afterExpiry.actor, null);
    assert.equal(afterExpiry.session, null);
    assert.equal(cookies.get(MOCK_AUTH_SESSION_COOKIE_NAME), undefined);
  });

  it("rejects and clears tampered session cookies", async () => {
    const runtime = createAuthRuntime();
    const cookies = new MemoryCookies();

    const issued = await runtime.authenticate(createRequestEvent("/admin?mockRole=admin", cookies));
    assert.ok(issued.session);

    const token = cookies.get(MOCK_AUTH_SESSION_COOKIE_NAME);
    assert.ok(token);
    cookies.set(MOCK_AUTH_SESSION_COOKIE_NAME, `${token}.tampered`);

    const afterTamper = await runtime.authenticate(createRequestEvent("/admin", cookies));

    assert.equal(afterTamper.actor, null);
    assert.equal(afterTamper.session, null);
    assert.equal(cookies.get(MOCK_AUTH_SESSION_COOKIE_NAME), undefined);
  });
});
