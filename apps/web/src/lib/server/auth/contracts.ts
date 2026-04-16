import type { RequestEvent } from "@sveltejs/kit";

export const AUTH_ROLES = ["employee", "vendor", "admin"] as const;

export type AuthRole = (typeof AUTH_ROLES)[number];

export interface AuthActor {
  id: string;
  role: AuthRole;
  displayName: string;
}

export interface AuthSession {
  sessionId: string;
  provider: "mock";
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
