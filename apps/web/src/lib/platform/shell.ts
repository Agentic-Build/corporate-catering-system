import type { AuthActor, AuthRequestContext } from "$lib/server/auth/contracts";
import { LOCALE_CODE } from "$lib/i18n/zh-tw";

import {
  buildRoleAwareNavigation,
  resolvePortalFromPath,
  type RoleAwareNavigation
} from "./navigation";
import { idleState, successState, type AsyncState } from "./async-state";

interface ShellAuthSnapshot {
  provider: string;
  refreshAfterEpochMs: number | null;
  expiresAtEpochMs: number | null;
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
  experienceMode: "mobile-first" | "desktop-first";
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
      expiresAtEpochMs: auth.session?.expiresAtEpochMs ?? null
    },
    navigation,
    activePortal,
    experienceMode: experiencePortal === "employee" ? "mobile-first" : "desktop-first",
    bootstrapState:
      actor === null
        ? idleState()
        : successState({
            message: "shared-platform-baseline-ready"
          })
  };
}
