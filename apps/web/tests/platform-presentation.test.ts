import assert from "node:assert/strict";
import { describe, it } from "node:test";

import { resolveLayoutPresentation } from "../src/lib/platform/presentation";

describe("responsive presentation baseline", () => {
  it("keeps employee experience mobile-first", () => {
    const presentation = resolveLayoutPresentation("mobile-first");

    assert.equal(presentation.shellContainerClass.includes("max-w-md"), true);
    assert.equal(presentation.sectionGridClass, "grid-cols-1");
  });

  it("keeps vendor/admin experience desktop-first with tablet grid", () => {
    const presentation = resolveLayoutPresentation("desktop-first");

    assert.equal(presentation.shellContainerClass.includes("max-w-7xl"), true);
    assert.equal(presentation.navPanelClass.includes("md:grid-cols-[1fr,2fr]"), true);
    assert.equal(presentation.sectionGridClass, "grid-cols-1 md:grid-cols-3");
  });
});
