import { createHmac, randomUUID, timingSafeEqual } from "node:crypto";

import type { RequestEvent } from "@sveltejs/kit";

import {
  validateActorScope,
  parseAuthRole,
  type AuthProvider,
  type AuthRole,
  type AuthSession
} from "./contracts";

export const MOCK_AUTH_SESSION_COOKIE_NAME = "cc_portal_auth_session";
const SESSION_COOKIE_PATH = "/";
const SESSION_TOKEN_VERSION = "v1";
const DEFAULT_TTL_MS = 30 * 60 * 1000;
const DEFAULT_REFRESH_WINDOW_MS = 10 * 60 * 1000;

interface MockAuthProviderOptions {
  signingSecret: string;
  ttlMs?: number;
  refreshWindowMs?: number;
}

interface MockSessionPayload {
  provider: "mock";
  sessionId: string;
  actor: {
    id: string;
    role: AuthRole;
    displayName: string;
    scope: {
      plantIds: string[];
      vendorIds: string[];
      permissions: string[];
    };
  };
  issuedAtEpochMs: number;
  refreshAfterEpochMs: number;
  expiresAtEpochMs: number;
}

const MOCK_ACTOR_TEMPLATE: Readonly<
  Record<
    AuthRole,
    {
      id: string;
      displayName: string;
      scope: {
        plantIds: readonly string[];
        vendorIds: readonly string[];
        permissions: readonly string[];
      };
    }
  >
> = {
  employee: {
    id: "emp-mock-001",
    displayName: "Mock Employee",
    scope: {
      plantIds: ["plant-tpe-a1"],
      vendorIds: [],
      permissions: ["employee:portal"]
    }
  },
  vendor: {
    id: "ven-mock-001",
    displayName: "Mock Vendor",
    scope: {
      plantIds: ["plant-tpe-a1"],
      vendorIds: ["ven-mock-001"],
      permissions: ["vendor:portal"]
    }
  },
  admin: {
    id: "adm-mock-001",
    displayName: "Mock Admin",
    scope: {
      plantIds: [],
      vendorIds: [],
      permissions: ["admin:portal", "scope:all"]
    }
  }
};

export function createMockAuthProvider(options: MockAuthProviderOptions): AuthProvider {
  const signingSecret = options.signingSecret.trim();
  if (!signingSecret) {
    throw new Error("mock auth signing secret must not be empty");
  }

  const ttlMs = options.ttlMs ?? DEFAULT_TTL_MS;
  const refreshWindowMs = options.refreshWindowMs ?? DEFAULT_REFRESH_WINDOW_MS;
  if (ttlMs <= 0) {
    throw new Error("mock auth session TTL must be greater than zero");
  }
  if (refreshWindowMs <= 0 || refreshWindowMs >= ttlMs) {
    throw new Error("mock auth refresh window must be within (0, ttlMs)");
  }

  return {
    id: "mock",
    async readSession(event: RequestEvent) {
      const token = event.cookies.get(MOCK_AUTH_SESSION_COOKIE_NAME);
      if (!token) {
        return null;
      }

      const session = parseSession(token, signingSecret);
      if (!session) {
        this.clearSession(event);
        return null;
      }

      return session;
    },
    async refreshSession(event: RequestEvent, session: AuthSession) {
      if (session.provider !== "mock") {
        return null;
      }

      const nowEpochMs = Date.now();
      if (session.expiresAtEpochMs <= nowEpochMs) {
        return null;
      }

      const refreshed = buildSessionForActor(session.actor, nowEpochMs, ttlMs, refreshWindowMs);
      persistSession(event, refreshed, signingSecret);
      return refreshed;
    },
    async issueSessionForRole(event: RequestEvent, role: AuthRole) {
      const template = MOCK_ACTOR_TEMPLATE[role];
      const session = buildSessionForActor(
        {
          id: template.id,
          role,
          displayName: template.displayName,
          scope: {
            plantIds: [...template.scope.plantIds],
            vendorIds: [...template.scope.vendorIds],
            permissions: [...template.scope.permissions]
          }
        },
        Date.now(),
        ttlMs,
        refreshWindowMs
      );
      persistSession(event, session, signingSecret);
      return session;
    },
    clearSession(event: RequestEvent) {
      event.cookies.delete(MOCK_AUTH_SESSION_COOKIE_NAME, cookieOptions(event));
    }
  };
}

function buildSessionForActor(
  actor: AuthSession["actor"],
  nowEpochMs: number,
  ttlMs: number,
  refreshWindowMs: number
): AuthSession {
  return {
    sessionId: randomUUID(),
    provider: "mock",
    actor: {
      id: actor.id,
      role: actor.role,
      displayName: actor.displayName,
      scope: {
        plantIds: [...actor.scope.plantIds],
        vendorIds: [...actor.scope.vendorIds],
        permissions: [...actor.scope.permissions]
      }
    },
    issuedAtEpochMs: nowEpochMs,
    refreshAfterEpochMs: nowEpochMs + (ttlMs - refreshWindowMs),
    expiresAtEpochMs: nowEpochMs + ttlMs
  };
}

function persistSession(event: RequestEvent, session: AuthSession, signingSecret: string) {
  const token = serializeSession(session, signingSecret);
  const maxAgeSeconds = maxAgeSecondsUntilExpiry(session.expiresAtEpochMs, Date.now());
  event.cookies.set(MOCK_AUTH_SESSION_COOKIE_NAME, token, cookieOptions(event, maxAgeSeconds));
}

function serializeSession(session: AuthSession, signingSecret: string): string {
  const payload = JSON.stringify(sessionToPayload(session));
  const encodedPayload = Buffer.from(payload).toString("base64url");
  const signature = signPayload(encodedPayload, signingSecret);
  return `${SESSION_TOKEN_VERSION}.${encodedPayload}.${signature}`;
}

function parseSession(token: string, signingSecret: string): AuthSession | null {
  if (!isCookieTokenShapeValid(token)) {
    return null;
  }

  const [version, encodedPayload, encodedSignature] = token.split(".");
  if (version !== SESSION_TOKEN_VERSION) {
    return null;
  }

  const expectedSignature = signPayload(encodedPayload, signingSecret);
  const expectedBuffer = Buffer.from(expectedSignature, "base64url");
  const providedBuffer = Buffer.from(encodedSignature, "base64url");
  if (expectedBuffer.length !== providedBuffer.length) {
    return null;
  }
  if (!timingSafeEqual(expectedBuffer, providedBuffer)) {
    return null;
  }

  try {
    const payloadJson = Buffer.from(encodedPayload, "base64url").toString("utf8");
    const payload = JSON.parse(payloadJson) as MockSessionPayload;
    return payloadToSession(payload);
  } catch {
    return null;
  }
}

function sessionToPayload(session: AuthSession): MockSessionPayload {
  return {
    provider: "mock",
    sessionId: session.sessionId,
    actor: {
      id: session.actor.id,
      role: session.actor.role,
      displayName: session.actor.displayName,
      scope: {
        plantIds: [...session.actor.scope.plantIds],
        vendorIds: [...session.actor.scope.vendorIds],
        permissions: [...session.actor.scope.permissions]
      }
    },
    issuedAtEpochMs: session.issuedAtEpochMs,
    refreshAfterEpochMs: session.refreshAfterEpochMs,
    expiresAtEpochMs: session.expiresAtEpochMs
  };
}

function payloadToSession(payload: MockSessionPayload): AuthSession | null {
  if (payload.provider !== "mock") {
    return null;
  }
  if (typeof payload.sessionId !== "string" || payload.sessionId.length === 0) {
    return null;
  }
  if (!payload.actor || typeof payload.actor !== "object") {
    return null;
  }

  const role = parseAuthRole(payload.actor.role);
  if (!role) {
    return null;
  }
  if (typeof payload.actor.id !== "string" || payload.actor.id.length === 0) {
    return null;
  }
  if (typeof payload.actor.displayName !== "string" || payload.actor.displayName.length === 0) {
    return null;
  }
  if (!payload.actor.scope || typeof payload.actor.scope !== "object") {
    return null;
  }

  if (
    !isFinitePositiveEpoch(payload.issuedAtEpochMs) ||
    !isFinitePositiveEpoch(payload.refreshAfterEpochMs) ||
    !isFinitePositiveEpoch(payload.expiresAtEpochMs)
  ) {
    return null;
  }
  if (payload.issuedAtEpochMs >= payload.refreshAfterEpochMs) {
    return null;
  }
  if (payload.refreshAfterEpochMs >= payload.expiresAtEpochMs) {
    return null;
  }

  const session: AuthSession = {
    sessionId: payload.sessionId,
    provider: "mock",
    actor: {
      id: payload.actor.id,
      role,
      displayName: payload.actor.displayName,
      scope: {
        plantIds: payload.actor.scope.plantIds,
        vendorIds: payload.actor.scope.vendorIds,
        permissions: payload.actor.scope.permissions
      }
    },
    issuedAtEpochMs: payload.issuedAtEpochMs,
    refreshAfterEpochMs: payload.refreshAfterEpochMs,
    expiresAtEpochMs: payload.expiresAtEpochMs
  };

  const issue = validateActorScope(session.actor);
  if (issue) {
    return null;
  }

  return session;
}

function signPayload(encodedPayload: string, signingSecret: string): string {
  return createHmac("sha256", signingSecret).update(encodedPayload).digest("base64url");
}

function cookieOptions(event: RequestEvent, maxAge?: number) {
  const secure = process.env.NODE_ENV === "production" || event.url.protocol === "https:";

  if (maxAge === undefined) {
    return {
      path: SESSION_COOKIE_PATH,
      httpOnly: true,
      sameSite: "lax" as const,
      secure
    };
  }

  return {
    path: SESSION_COOKIE_PATH,
    httpOnly: true,
    sameSite: "lax" as const,
    secure,
    maxAge
  };
}

function maxAgeSecondsUntilExpiry(expiresAtEpochMs: number, nowEpochMs: number): number {
  const remainingMs = expiresAtEpochMs - nowEpochMs;
  if (remainingMs <= 0) {
    return 1;
  }

  return Math.max(1, Math.floor(remainingMs / 1000));
}

function isFinitePositiveEpoch(value: unknown): value is number {
  return typeof value === "number" && Number.isFinite(value) && value > 0;
}

function isCookieTokenShapeValid(token: string): boolean {
  const parts = token.split(".");
  if (parts.length !== 3) {
    return false;
  }

  return parts.every((part) => part.length > 0);
}
