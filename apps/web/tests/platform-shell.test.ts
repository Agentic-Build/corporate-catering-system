import assert from "node:assert/strict";
import { describe, it } from "node:test";

import type { AuthActor, AuthRequestContext, AuthRole } from "../src/lib/server/auth/contracts";
import { buildAppShellData } from "../src/lib/platform/shell";

describe("platform shell data", () => {
  configureJwtEnv();

  it("uses mobile-first mode for employee actors and starts bootstrap probe in loading state", () => {
    const actor = createActor("employee");
    const authContext = createAuthContext(actor);

    const shell = buildAppShellData({
      actor,
      auth: authContext,
      pathname: "/employee/orders"
    });

    assert.equal(shell.experienceMode, "mobile-first");
    assert.equal(shell.bootstrapState.status, "loading");
    assert.equal(shell.navigation.sectionPortal, "employee");
    assert.equal(shell.navigation.activeSectionId, "orders");
  });

  it("uses desktop-first mode for vendor/admin actors", () => {
    const vendorShell = buildAppShellData({
      actor: createActor("vendor"),
      auth: createAuthContext(createActor("vendor")),
      pathname: "/vendor/menu"
    });
    const adminShell = buildAppShellData({
      actor: createActor("admin"),
      auth: createAuthContext(createActor("admin")),
      pathname: "/admin/anomalies"
    });

    assert.equal(vendorShell.experienceMode, "desktop-first");
    assert.equal(adminShell.experienceMode, "desktop-first");
  });

  it("returns idle bootstrap and unlocked nav when unauthenticated", () => {
    const shell = buildAppShellData({
      actor: null,
      auth: {
        actor: null,
        provider: "mock",
        session: null
      },
      pathname: "/"
    });

    assert.equal(shell.bootstrapState.status, "idle");
    assert.equal(shell.navigation.primary.length, 0);
    assert.equal(shell.navigation.portalLinks.every((portalLink) => portalLink.locked === false), true);
  });

  it("embeds vendor scope in API bearer claims for vendor sessions", () => {
    const vendorActor = createActor("vendor");
    const shell = buildAppShellData({
      actor: vendorActor,
      auth: createAuthContext(vendorActor),
      pathname: "/vendor/menu"
    });
    const claims = decodeJwtClaims(shell.auth.apiBearerToken);

    assert.equal(claims.role, "VENDOR_OPERATOR");
    assert.deepEqual(claims.vendorIds, ["ven-test"]);
    assert.deepEqual(claims.plantIds, ["plant-a"]);
  });

  it("preserves scoped admin plant boundaries in API bearer claims", () => {
    const scopedAdminActor: AuthActor = {
      id: "adm-scoped",
      role: "admin",
      displayName: "Scoped Admin",
      scope: {
        plantIds: ["plant-a", "plant-b"],
        vendorIds: [],
        permissions: ["admin:portal"]
      }
    };
    const shell = buildAppShellData({
      actor: scopedAdminActor,
      auth: createAuthContext(scopedAdminActor),
      pathname: "/admin/settlements"
    });
    const claims = decodeJwtClaims(shell.auth.apiBearerToken);

    assert.equal(claims.role, "COMMITTEE_ADMIN");
    assert.equal(claims.allPlants, false);
    assert.deepEqual(claims.plantIds, ["plant-a", "plant-b"]);
  });

  it("keeps global admin tokens on allPlants when scope:all is present", () => {
    const globalAdminActor = createActor("admin");
    const shell = buildAppShellData({
      actor: globalAdminActor,
      auth: createAuthContext(globalAdminActor),
      pathname: "/admin/analytics"
    });
    const claims = decodeJwtClaims(shell.auth.apiBearerToken);

    assert.equal(claims.role, "COMMITTEE_ADMIN");
    assert.equal(claims.allPlants, true);
    assert.deepEqual(claims.plantIds, []);
  });
});

function configureJwtEnv() {
  process.env.CORPORATE_SSO_JWT_ISSUER = "https://issuer.catering-corp.test";
  process.env.CORPORATE_SSO_JWT_AUDIENCE = "corporate-catering-http-runtime-corporate";
  process.env.CORPORATE_SSO_JWT_HS256_SECRET_BASE64 = Buffer.from(
    "corporate-sso-test-signing-secret-32"
  ).toString("base64");
  process.env.VENDOR_MFA_JWT_ISSUER = "https://issuer.catering-vendor.test";
  process.env.VENDOR_MFA_JWT_AUDIENCE = "corporate-catering-http-runtime-vendor";
  process.env.VENDOR_MFA_JWT_HS256_SECRET_BASE64 = Buffer.from(
    "vendor-mfa-test-signing-secret-32-bytes"
  ).toString("base64");
}

function createAuthContext(actor: AuthActor): AuthRequestContext {
  const nowEpochMs = Date.now();
  return {
    actor,
    provider: "mock",
    session: {
      sessionId: "session-id",
      provider: "mock",
      actor,
      issuedAtEpochMs: nowEpochMs,
      refreshAfterEpochMs: nowEpochMs + 5 * 60 * 1000,
      expiresAtEpochMs: nowEpochMs + 10 * 60 * 1000
    }
  };
}

function decodeJwtClaims(token: string | null): Record<string, unknown> {
  assert.ok(token, "api bearer token should be present");
  const segments = token.split(".");
  assert.equal(segments.length, 3);
  const payloadSegment = segments[1];
  return JSON.parse(Buffer.from(payloadSegment, "base64url").toString("utf8")) as Record<
    string,
    unknown
  >;
}

function createActor(role: AuthRole): AuthActor {
  if (role === "employee") {
    return {
      id: "emp-test",
      role,
      displayName: "Employee",
      scope: {
        plantIds: ["plant-a"],
        vendorIds: [],
        permissions: ["employee:portal"]
      }
    };
  }

  if (role === "vendor") {
    return {
      id: "ven-test",
      role,
      displayName: "Vendor",
      scope: {
        plantIds: ["plant-a"],
        vendorIds: ["ven-test"],
        permissions: ["vendor:portal"]
      }
    };
  }

  return {
    id: "adm-test",
    role,
    displayName: "Admin",
    scope: {
      plantIds: [],
      vendorIds: [],
      permissions: ["admin:portal", "scope:all"]
    }
  };
}
