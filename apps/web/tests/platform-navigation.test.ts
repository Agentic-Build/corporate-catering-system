import assert from "node:assert/strict";
import { describe, it } from "node:test";

import { resolveRoleGuard } from "../src/lib/server/auth/guards";
import { buildRoleAwareNavigation } from "../src/lib/platform/navigation";

describe("platform navigation", () => {
  it("locks portals outside the authenticated role", () => {
    const navigation = buildRoleAwareNavigation("employee", "/employee/orders");

    assert.equal(navigation.sectionPortal, "employee");
    assert.equal(navigation.activeSectionId, "orders");
    assert.equal(
      navigation.portalLinks.find((link) => link.role === "vendor")?.locked,
      true
    );
    assert.equal(
      navigation.portalLinks.find((link) => link.role === "admin")?.locked,
      true
    );
  });

  it("resolves admin anomalies as the active primary section", () => {
    const navigation = buildRoleAwareNavigation("admin", "/admin/anomalies");

    assert.equal(navigation.activeSectionId, "anomalies");
    assert.equal(
      navigation.primary.find((item) => item.id === "anomalies")?.active,
      true
    );
  });

  it("keeps deep admin routes active under their parent section", () => {
    const navigation = buildRoleAwareNavigation("admin", "/admin/settlement/close");

    assert.equal(navigation.activeSectionId, "settlement");
    assert.equal(
      navigation.primary.find((item) => item.id === "settlement")?.active,
      true
    );
  });

  it("activates vendor compliance section for sub-routes", () => {
    const navigation = buildRoleAwareNavigation("vendor", "/vendor/compliance/upload");

    assert.equal(navigation.activeSectionId, "compliance");
    assert.equal(
      navigation.primary.find((item) => item.id === "compliance")?.active,
      true
    );
  });

  it("activates employee pickup deep route under the orders section", () => {
    const navigation = buildRoleAwareNavigation("employee", "/employee/orders/abc/pickup");

    assert.equal(navigation.activeSectionId, "orders");
    assert.equal(
      navigation.primary.find((item) => item.id === "orders")?.active,
      true
    );
  });

  it("rejects legacy /portal and /console guard prefixes", () => {
    assert.equal(resolveRoleGuard("/portal/admin"), null);
    assert.equal(resolveRoleGuard("/console/vendor"), null);
    assert.notEqual(resolveRoleGuard("/employee"), null);
  });
});
