import assert from "node:assert/strict";
import { describe, it } from "node:test";

import {
  ANOMALY_RELEASE_SIGN_OFF_ISSUE_ID,
  SETTLEMENT_RELEASE_SIGN_OFF_ISSUE_ID,
  isIssueSignOffConfirmed,
  parseBooleanFlag,
  parseEvidenceRefsInput,
  parseIssueChecklist,
  parseOptionalEpochDay,
  parseOptionalMinuteOfDay,
  toTaipeiDateTime
} from "../src/lib/admin/portal";

describe("admin portal helpers", () => {
  it("parses issue checklists and validates required sign-off ids", () => {
    const checklist = parseIssueChecklist("iss-003, ISS-007\nISS-010");
    assert.deepEqual(checklist, ["ISS-003", "ISS-007", "ISS-010"]);
    assert.equal(
      isIssueSignOffConfirmed(checklist, SETTLEMENT_RELEASE_SIGN_OFF_ISSUE_ID),
      true
    );
    assert.equal(
      isIssueSignOffConfirmed(checklist, ANOMALY_RELEASE_SIGN_OFF_ISSUE_ID),
      true
    );
    assert.equal(isIssueSignOffConfirmed(checklist, "ISS-999"), false);
  });

  it("normalizes evidence refs with stable de-duplication", () => {
    const refs = parseEvidenceRefsInput(
      " evidence://a \n evidence://b, evidence://a ,, runbook://ops "
    );
    assert.deepEqual(refs, ["evidence://a", "evidence://b", "runbook://ops"]);
  });

  it("parses optional epoch-day and minute filters with validation", () => {
    assert.equal(parseOptionalEpochDay(""), undefined);
    assert.equal(parseOptionalEpochDay("123"), 123);
    assert.equal(parseOptionalMinuteOfDay("0"), 0);
    assert.equal(parseOptionalMinuteOfDay("1439"), 1439);
    assert.throws(() => parseOptionalEpochDay("-1"), /大於或等於/);
    assert.throws(() => parseOptionalMinuteOfDay("1440"), /小於或等於/);
  });

  it("parses deterministic boolean filters for tri-state controls", () => {
    assert.equal(parseBooleanFlag("ALL"), undefined);
    assert.equal(parseBooleanFlag("true"), true);
    assert.equal(parseBooleanFlag("FALSE"), false);
    assert.throws(() => parseBooleanFlag("maybe"), /僅接受 ALL、TRUE 或 FALSE/);
  });

  it("formats datetime-local values into fixed taipei offset timestamps", () => {
    assert.equal(toTaipeiDateTime("2026-04-17T14:30"), "2026-04-17T14:30:00+08:00");
    assert.throws(() => toTaipeiDateTime(""), /為必填/);
  });
});
