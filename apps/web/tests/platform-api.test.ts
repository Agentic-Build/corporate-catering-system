import assert from "node:assert/strict";
import { describe, it } from "node:test";

import { zhTW } from "../src/lib/i18n/zh-tw";
import {
  ApiConfigurationError,
  PlantScopeError,
  normalizeApiFailure
} from "../src/lib/platform/api/failure";

describe("api failure localization", () => {
  it("localizes API configuration failures", () => {
    const failure = normalizeApiFailure(new ApiConfigurationError());

    assert.equal(failure.code, "api-base-url-missing");
    assert.equal(failure.localizedMessage, zhTW.api.failure.baseUrlMissing);
  });

  it("localizes missing plant scope failures", () => {
    const failure = normalizeApiFailure(new PlantScopeError());

    assert.equal(failure.code, "plant-scope-required");
    assert.equal(failure.localizedMessage, zhTW.api.failure.plantScopeMissing);
  });

  it("maps ApiError status codes to zh-TW user-facing copy", () => {
    const apiFailure = normalizeApiFailure({
      name: "ApiError",
      message: "Forbidden",
      status: 403
    } as Error & { status: number });

    assert.equal(apiFailure.code, "http-403");
    assert.equal(apiFailure.localizedMessage, zhTW.api.failure.statusText[403]);
  });
});
