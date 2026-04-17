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
    assert.equal(shell.navigation.sectionLinks.length, 0);
    assert.equal(shell.navigation.portalLinks.every((portalLink) => portalLink.locked === false), true);
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
  return {
    actor,
    provider: "mock",
    session: {
      sessionId: "session-id",
      provider: "mock",
      actor,
      issuedAtEpochMs: 1,
      refreshAfterEpochMs: 2,
      expiresAtEpochMs: 3
    }
  };
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
