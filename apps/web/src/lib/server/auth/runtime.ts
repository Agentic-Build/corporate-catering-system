import type { RequestEvent } from "@sveltejs/kit";

import {
  parseAuthRole,
  type AuthProvider,
  type AuthRequestContext,
  type AuthRole
} from "./contracts";
import { resolveRoleGuard, type RoleGuard } from "./guards";
import { createMockAuthProvider } from "./mock-provider";

const DEV_ROLE_HINT_HEADER = "x-mock-role";
const DEV_ROLE_HINT_QUERY_PARAMS = ["mockRole", "mock_role"] as const;

const DEFAULT_DEV_SIGNING_SECRET = "clar-002-dev-only-mock-auth-session-secret";

export interface AuthRuntime {
  authenticate(event: RequestEvent): Promise<AuthRequestContext>;
  issueMockSession(event: RequestEvent, role: AuthRole): Promise<AuthRequestContext>;
  clearSession(event: RequestEvent): void;
  resolveRoleGuard(pathname: string): RoleGuard | null;
  isDevMode(): boolean;
  canIssueMockSessions(): boolean;
}

export function createAuthRuntime(): AuthRuntime {
  const provider = createProviderFromEnv();
  const devMode = isDevRuntime();
  const mockSessionsEnabled = Boolean(provider.issueSessionForRole);

  return {
    async authenticate(event: RequestEvent): Promise<AuthRequestContext> {
      let session = await provider.readSession(event);
      const nowEpochMs = Date.now();

      if (session && session.expiresAtEpochMs <= nowEpochMs) {
        provider.clearSession(event);
        session = null;
      }

      if (devMode && provider.issueSessionForRole) {
        const overrideRole = readDevRoleOverride(event);
        if (overrideRole) {
          session = await provider.issueSessionForRole(event, overrideRole);
        }
      }

      if (session && nowEpochMs >= session.refreshAfterEpochMs) {
        const refreshed = await provider.refreshSession(event, session);
        if (!refreshed) {
          provider.clearSession(event);
          session = null;
        } else {
          session = refreshed;
        }
      }

      return {
        actor: session?.actor ?? null,
        session,
        provider: provider.id
      };
    },
    async issueMockSession(event: RequestEvent, role: AuthRole): Promise<AuthRequestContext> {
      if (!provider.issueSessionForRole) {
        throw new Error(`auth provider ${provider.id} does not support mock role sessions`);
      }

      const session = await provider.issueSessionForRole(event, role);

      return {
        actor: session.actor,
        session,
        provider: provider.id
      };
    },
    clearSession(event: RequestEvent) {
      provider.clearSession(event);
    },
    resolveRoleGuard,
    isDevMode() {
      return devMode;
    },
    canIssueMockSessions() {
      return mockSessionsEnabled;
    }
  };
}

function createProviderFromEnv(): AuthProvider {
  const providerId = (process.env.AUTH_PROVIDER ?? "mock").trim().toLowerCase();
  if (providerId !== "mock") {
    throw new Error(
      `CLAR-002 violation: auth provider \"${providerId}\" is forbidden in this phase; only \"mock\" is allowed`
    );
  }

  return createMockAuthProvider({
    signingSecret: resolveMockAuthSigningSecret()
  });
}

function resolveMockAuthSigningSecret(): string {
  const configured = process.env.MOCK_AUTH_SIGNING_SECRET?.trim();
  if (configured) {
    return configured;
  }

  if (isDevRuntime()) {
    return DEFAULT_DEV_SIGNING_SECRET;
  }

  throw new Error("MOCK_AUTH_SIGNING_SECRET must be configured when NODE_ENV=production");
}

function readDevRoleOverride(event: RequestEvent): AuthRole | null {
  const headerRole = parseAuthRole(event.request.headers.get(DEV_ROLE_HINT_HEADER));
  if (headerRole) {
    return headerRole;
  }

  for (const key of DEV_ROLE_HINT_QUERY_PARAMS) {
    const queryRole = parseAuthRole(event.url.searchParams.get(key));
    if (queryRole) {
      return queryRole;
    }
  }

  return null;
}

function isDevRuntime(): boolean {
  return process.env.NODE_ENV !== "production";
}
