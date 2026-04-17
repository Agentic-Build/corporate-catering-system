import assert from "node:assert/strict";
import { describe, it } from "node:test";

import {
  countdownToEpoch,
  isEmployeeOrderEditable,
  isPickupEligible,
  isResolvedDisputeStatus,
  pickupQrSecondsRemaining,
  taipeiDateMinuteToEpochMs
} from "../src/lib/employee/portal";

describe("employee portal helpers", () => {
  it("marks mutable lifecycle states for update and pickup", () => {
    assert.equal(isEmployeeOrderEditable("PENDING"), true);
    assert.equal(isEmployeeOrderEditable("MODIFIED"), true);
    assert.equal(isEmployeeOrderEditable("FULFILLED"), false);

    assert.equal(isPickupEligible("PENDING"), true);
    assert.equal(isPickupEligible("MODIFIED"), true);
    assert.equal(isPickupEligible("CANCELLED"), false);
  });

  it("classifies resolved payroll dispute statuses", () => {
    assert.equal(isResolvedDisputeStatus("RESOLVED_REFUND_APPROVED"), true);
    assert.equal(isResolvedDisputeStatus("RESOLVED_REJECTED"), true);
    assert.equal(isResolvedDisputeStatus("OPEN"), false);
  });

  it("converts Taipei date + minute-of-day into epoch millis", () => {
    const epochMs = taipeiDateMinuteToEpochMs("2026-04-17", 17 * 60);
    assert.equal(
      new Date(epochMs).toISOString(),
      "2026-04-17T09:00:00.000Z"
    );
  });

  it("renders countdown labels and expired state", () => {
    const now = Date.parse("2026-04-17T08:00:00.000Z");
    const future = now + 90_000;
    const active = countdownToEpoch(now, future);
    assert.equal(active.expired, false);
    assert.equal(active.secondsRemaining, 90);
    assert.match(active.label, /^倒數 /);

    const expired = countdownToEpoch(now, now - 1_000);
    assert.equal(expired.expired, true);
    assert.equal(expired.secondsRemaining, 0);
    assert.equal(expired.label, "已截止");
  });

  it("computes pickup QR remaining seconds from expiresAt epoch", () => {
    const now = Date.parse("2026-04-17T08:00:00.000Z");
    const remaining = pickupQrSecondsRemaining(now, {
      expiresAtEpochSecond: Math.floor(now / 1000) + 17
    });
    assert.equal(remaining, 17);
  });
});
