export const SETTLEMENT_RELEASE_SIGN_OFF_ISSUE_ID = "ISS-003";
export const ANOMALY_RELEASE_SIGN_OFF_ISSUE_ID = "ISS-007";

export function parseOptionalEpochDay(value: string): number | undefined {
  return parseOptionalInteger(value, {
    field: "epochDay",
    minimum: 1
  });
}

export function parseOptionalMinuteOfDay(value: string): number | undefined {
  return parseOptionalInteger(value, {
    field: "minuteOfDay",
    minimum: 0,
    maximum: 1439
  });
}

export function parseEvidenceRefsInput(value: string): string[] {
  const uniqueRefs = new Set<string>();
  const normalizedRefs: string[] = [];
  const candidates = value
    .split(/[\n,]/)
    .map((candidate) => candidate.trim())
    .filter((candidate) => candidate.length > 0);

  for (const candidate of candidates) {
    if (uniqueRefs.has(candidate)) {
      continue;
    }
    uniqueRefs.add(candidate);
    normalizedRefs.push(candidate);
  }

  return normalizedRefs;
}

export function parseOptionalNumber(value: string): number | undefined {
  const trimmed = value.trim();
  if (trimmed.length === 0) {
    return undefined;
  }

  const parsed = Number(trimmed);
  if (!Number.isFinite(parsed)) {
    throw new Error("must be a finite number");
  }
  return parsed;
}

export function parseRequiredNumber(value: string, field: string): number {
  const parsed = parseOptionalNumber(value);
  if (parsed === undefined) {
    throw new Error(`${field} is required`);
  }
  return parsed;
}

export function parseBooleanFlag(value: string): boolean | undefined {
  const normalized = value.trim().toLowerCase();
  if (normalized.length === 0 || normalized === "all") {
    return undefined;
  }
  if (normalized === "true") {
    return true;
  }
  if (normalized === "false") {
    return false;
  }
  throw new Error("flag must be ALL, TRUE, or FALSE");
}

export function isIssueSignOffConfirmed(
  confirmedIssueIds: readonly string[],
  requiredIssueId: string
): boolean {
  const required = normalizeIssueId(requiredIssueId);
  return confirmedIssueIds.some((candidate) => normalizeIssueId(candidate) === required);
}

export function parseIssueChecklist(value: string): string[] {
  return value
    .split(/[\n,]/)
    .map((entry) => normalizeIssueId(entry))
    .filter((entry) => entry.length > 0);
}

export function normalizeIssueId(value: string): string {
  return value.trim().toUpperCase();
}

export function toTaipeiDateTime(dateTimeLocal: string): string {
  const trimmed = dateTimeLocal.trim();
  if (trimmed.length === 0) {
    throw new Error("datetime input is required");
  }

  const candidate = `${trimmed}:00+08:00`;
  if (!isValidIsoDateTime(candidate)) {
    throw new Error("datetime input must follow YYYY-MM-DDTHH:mm");
  }
  return candidate;
}

export function formatTaipeiDateTime(dateTime: string | null | undefined): string {
  if (!dateTime) {
    return "-";
  }
  const epochMs = Date.parse(dateTime);
  if (Number.isNaN(epochMs)) {
    return dateTime;
  }
  return new Date(epochMs).toLocaleString("zh-TW", {
    hour12: false,
    timeZone: "Asia/Taipei"
  });
}

export function todayTaipeiIsoDate(): string {
  return new Date().toLocaleDateString("en-CA", {
    timeZone: "Asia/Taipei"
  });
}

export function addDaysIsoDate(dateIso: string, days: number): string {
  const source = Date.parse(`${dateIso}T00:00:00+08:00`);
  if (Number.isNaN(source)) {
    throw new Error("dateIso must be a valid YYYY-MM-DD date string");
  }
  const target = source + days * 24 * 60 * 60 * 1000;
  return new Date(target).toLocaleDateString("en-CA", {
    timeZone: "Asia/Taipei"
  });
}

function parseOptionalInteger(
  value: string,
  constraints: {
    field: string;
    minimum: number;
    maximum?: number;
  }
): number | undefined {
  const trimmed = value.trim();
  if (trimmed.length === 0) {
    return undefined;
  }

  const parsed = Number(trimmed);
  if (!Number.isInteger(parsed)) {
    throw new Error(`${constraints.field} must be an integer`);
  }
  if (parsed < constraints.minimum) {
    throw new Error(`${constraints.field} must be at least ${constraints.minimum}`);
  }
  if (constraints.maximum !== undefined && parsed > constraints.maximum) {
    throw new Error(`${constraints.field} must be at most ${constraints.maximum}`);
  }

  return parsed;
}

function isValidIsoDateTime(value: string): boolean {
  return !Number.isNaN(Date.parse(value));
}
