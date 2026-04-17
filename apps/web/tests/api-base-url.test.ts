import assert from "node:assert/strict";
import { describe, it } from "node:test";

import { resolveConfiguredApiBaseUrl } from "../src/lib/platform/api/base-url";

describe("api base url normalization", () => {
  it("accepts an empty string for same-origin dev proxy traffic", () => {
    assert.equal(resolveConfiguredApiBaseUrl(""), "");
  });

  it("rejects missing or whitespace-padded values", () => {
    assert.equal(resolveConfiguredApiBaseUrl(undefined), null);
    assert.equal(resolveConfiguredApiBaseUrl(" /api "), null);
  });

  it("rejects values that already include the generated client api prefix", () => {
    assert.equal(resolveConfiguredApiBaseUrl("http://127.0.0.1:18080/api"), null);
    assert.equal(resolveConfiguredApiBaseUrl("https://example.com/api/"), null);
  });
});
