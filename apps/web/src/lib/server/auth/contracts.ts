import type { RequestEvent } from "@sveltejs/kit";

export const AUTH_ROLES = ["employee", "vendor", "admin"] as const;

export type AuthRole = (typeof AUTH_ROLES)[number];

export interface AuthScope {
  plantIds: readonly string[];
  vendorIds: readonly string[];
  permissions: readonly string[];
}

export interface AuthActor {
  id: string;
  role: AuthRole;
  displayName: string;
  scope: AuthScope;
}

export interface AuthSession {
  sessionId: string;
  provider: string;
  actor: AuthActor;
  issuedAtEpochMs: number;
  refreshAfterEpochMs: number;
  expiresAtEpochMs: number;
}

export interface AuthRequestContext {
  actor: AuthActor | null;
  session: AuthSession | null;
  provider: string;
}

export interface AuthProvider {
  readonly id: string;
  readSession(event: RequestEvent): Promise<AuthSession | null>;
  refreshSession(event: RequestEvent, session: AuthSession): Promise<AuthSession | null>;
  clearSession(event: RequestEvent): void;
  issueSessionForRole?(event: RequestEvent, role: AuthRole): Promise<AuthSession>;
}

export interface ScopeValidationIssue {
  code: string;
  message: string;
}

const REQUIRED_ROLE_PERMISSION: Readonly<Record<AuthRole, string>> = {
  employee: "employee:portal",
  vendor: "vendor:portal",
  admin: "admin:portal"
};

export function parseAuthRole(value: string | null | undefined): AuthRole | null {
  if (!value) {
    return null;
  }

  const normalized = value.trim().toLowerCase();
  if (normalized === "employee" || normalized === "vendor" || normalized === "admin") {
    return normalized;
  }

  return null;
}

export function hasPermission(actor: AuthActor, permission: string): boolean {
  if (actor.scope.permissions.includes("scope:all")) {
    return true;
  }

  return actor.scope.permissions.includes(permission);
}

export function validateActorScope(actor: AuthActor): ScopeValidationIssue | null {
  if (!isNonEmptyString(actor.id)) {
    return {
      code: "actor-id-invalid",
      message: "actor.id must be a non-empty string"
    };
  }
  if (!isNonEmptyString(actor.displayName)) {
    return {
      code: "actor-display-name-invalid",
      message: "actor.displayName must be a non-empty string"
    };
  }

  const plantIds = normalizeStringList(actor.scope.plantIds);
  if (!plantIds.ok) {
    return {
      code: "scope-plants-invalid",
      message: plantIds.message
    };
  }

  const vendorIds = normalizeStringList(actor.scope.vendorIds);
  if (!vendorIds.ok) {
    return {
      code: "scope-vendors-invalid",
      message: vendorIds.message
    };
  }

  const permissions = normalizeStringList(actor.scope.permissions);
  if (!permissions.ok) {
    return {
      code: "scope-permissions-invalid",
      message: permissions.message
    };
  }

  const requiredPermission = REQUIRED_ROLE_PERMISSION[actor.role];
  if (!permissions.values.includes(requiredPermission) && !permissions.values.includes("scope:all")) {
    return {
      code: "scope-missing-required-permission",
      message: `role ${actor.role} requires permission ${requiredPermission}`
    };
  }

  if (actor.role === "employee") {
    if (vendorIds.values.length > 0) {
      return {
        code: "scope-role-mismatch-employee-vendor",
        message: "employee role must not carry vendorIds scope"
      };
    }
    if (plantIds.values.length === 0) {
      return {
        code: "scope-role-mismatch-employee-plant",
        message: "employee role requires at least one plantId scope"
      };
    }
  }

  if (actor.role === "vendor") {
    if (vendorIds.values.length === 0) {
      return {
        code: "scope-role-mismatch-vendor-id",
        message: "vendor role requires at least one vendorId scope"
      };
    }
    if (plantIds.values.length === 0) {
      return {
        code: "scope-role-mismatch-vendor-plant",
        message: "vendor role requires at least one plantId scope"
      };
    }
  }

  return null;
}

function normalizeStringList(values: readonly string[]):
  | { ok: true; values: string[] }
  | { ok: false; message: string } {
  const normalized: string[] = [];
  const seen = new Set<string>();

  for (const value of values) {
    if (!isNonEmptyString(value)) {
      return {
        ok: false,
        message: "scope values must be non-empty strings"
      };
    }

    const normalizedValue = value.trim();
    if (!seen.has(normalizedValue)) {
      seen.add(normalizedValue);
      normalized.push(normalizedValue);
    }
  }

  return {
    ok: true,
    values: normalized
  };
}

function isNonEmptyString(value: unknown): value is string {
  return typeof value === "string" && value.trim().length > 0;
}
