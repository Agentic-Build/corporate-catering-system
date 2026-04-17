import type { AuthActor, AuthRequestContext } from "$lib/server/auth/contracts";
import { LOCALE_CODE } from "$lib/i18n/zh-tw";
import { buildApiBearerToken } from "$lib/server/auth/api-bearer";

import {
  buildRoleAwareNavigation,
  resolvePortalFromPath,
  type RoleAwareNavigation
} from "./navigation";
import { idleState, loadingState, type AsyncState } from "./async-state";
import type { ExperienceMode } from "./presentation";

interface ShellAuthSnapshot {
  provider: string;
  refreshAfterEpochMs: number | null;
  expiresAtEpochMs: number | null;
  apiBearerToken: string | null;
}

export interface ShellBootstrapState {
  message: string;
}

export interface AppShellData {
  locale: typeof LOCALE_CODE;
  actor: AuthActor | null;
  auth: ShellAuthSnapshot;
  navigation: RoleAwareNavigation;
  activePortal: ReturnType<typeof resolvePortalFromPath>;
  experienceMode: ExperienceMode;
  bootstrapState: AsyncState<ShellBootstrapState>;
}

export function buildAppShellData(args: {
  actor: AuthActor | null;
  auth: AuthRequestContext;
  pathname: string;
}): AppShellData {
  const { actor, auth, pathname } = args;
  const navigation = buildRoleAwareNavigation(actor?.role ?? null, pathname);
  const activePortal = resolvePortalFromPath(pathname);
  const experiencePortal = navigation.rolePortal ?? activePortal;

  return {
    locale: LOCALE_CODE,
    actor,
    auth: {
      provider: auth.provider,
      refreshAfterEpochMs: auth.session?.refreshAfterEpochMs ?? null,
      expiresAtEpochMs: auth.session?.expiresAtEpochMs ?? null,
      apiBearerToken: buildApiBearerToken(auth)
    },
    navigation,
    activePortal,
    experienceMode: experiencePortal === "employee" ? "mobile-first" : "desktop-first",
    bootstrapState: actor === null ? idleState() : loadingState()
  };
}
