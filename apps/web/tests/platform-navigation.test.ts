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
      navigation.portalLinks.find((portalLink) => portalLink.role === "vendor")?.locked,
      true
    );
    assert.equal(
      navigation.portalLinks.find((portalLink) => portalLink.role === "admin")?.locked,
      true
    );
  });

  it("marks section links as active inside wildcard role routes", () => {
    const navigation = buildRoleAwareNavigation("admin", "/admin/anomalies");

    assert.equal(navigation.activeSectionId, "anomalies");
    assert.equal(
      navigation.sectionLinks.find((sectionLink) => sectionLink.id === "anomalies")?.active,
      true
    );
  });

  it("rejects legacy /portal and /console guard prefixes", () => {
    assert.equal(resolveRoleGuard("/portal/admin"), null);
    assert.equal(resolveRoleGuard("/console/vendor"), null);
    assert.notEqual(resolveRoleGuard("/employee"), null);
  });
});
