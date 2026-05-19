// Deep-link auth flow for the Tauri mobile app.
//
// Flow (per design doc Part B' — Auth):
//  1. App opens the system browser at the API's OIDC start endpoint,
//     passing `app=employee-app` so the backend (B4) knows to redirect
//     the callback to our custom scheme instead of the web landing page.
//  2. After OIDC the backend redirects to `tbite://auth?token=...`.
//  3. The OS routes that URL back into the app; the deep-link plugin's
//     handler calls `completeLogin(url)` below.
//  4. The token is stored securely and the session store is updated.
//
// The Tauri plugin calls (`@tauri-apps/plugin-opener`,
// `@tauri-apps/plugin-deep-link`, `@tauri-apps/plugin-stronghold`) are
// intentionally NOT imported here: those packages are only installable in
// the native build (M5). This module is a documented stub that compiles in
// a plain web build and is wired to the real plugins during native bring-up.

import { API_BASE_URL } from "./config";
import { session } from "./session.svelte";

/** True when running inside the Tauri webview (vs. a plain browser). */
export function isTauri(): boolean {
  return typeof window !== "undefined" && "__TAURI_INTERNALS__" in window;
}

/**
 * Begin login. `POST`s to the OIDC start endpoint to obtain the provider
 * `auth_url`, then opens that URL in the system browser.
 *
 * The start endpoint is a POST that takes a JSON body `{ app, return_to }`
 * and returns `{ auth_url, state }` — `app: "employee-app"` tells the backend
 * (B4) to redirect the callback to our `tbite://` deep link.
 *
 * Native: replace `window.open` with `@tauri-apps/plugin-opener`'s `openUrl`.
 */
export async function startLogin(provider: string): Promise<void> {
  const res = await fetch(`${API_BASE_URL}/auth/${encodeURIComponent(provider)}/start`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ app: "employee-app", return_to: "/" }),
  });
  if (!res.ok) throw new Error("登入啟動失敗");
  const { auth_url } = (await res.json()) as { auth_url: string };
  if (isTauri()) {
    // TODO(M5): const { openUrl } = await import("@tauri-apps/plugin-opener");
    //           await openUrl(auth_url);
    window.open(auth_url, "_blank");
  } else {
    window.location.href = auth_url;
  }
}

/**
 * Handle the `tbite://auth?token=...` deep link. Registered as the
 * deep-link plugin's `onOpenUrl` handler in native builds.
 */
export async function completeLogin(deepLinkUrl: string): Promise<boolean> {
  let token: string | null = null;
  try {
    token = new URL(deepLinkUrl).searchParams.get("token");
  } catch {
    return false;
  }
  if (!token) return false;

  await storeToken(token);
  session.setToken(token);
  return true;
}

/**
 * Persist the token. Native builds must use the platform keychain via
 * `tauri-plugin-stronghold` — NOT localStorage. Until that plugin is wired
 * in M5, `session.setToken` already mirrors to localStorage as a fallback.
 */
async function storeToken(_token: string): Promise<void> {
  // TODO(M5): persist via @tauri-apps/plugin-stronghold / platform keychain.
}

/**
 * Register the deep-link handler. Called once from the root layout on a
 * native build. No-op in the browser.
 */
export async function initDeepLinks(): Promise<void> {
  if (!isTauri()) return;
  // TODO(M5):
  //   const { onOpenUrl } = await import("@tauri-apps/plugin-deep-link");
  //   await onOpenUrl((urls) => { for (const u of urls) completeLogin(u); });
}

/** Log out: clear local session and notify the backend. */
export async function logout(): Promise<void> {
  const token = session.token;
  session.clear();
  if (token) {
    try {
      await fetch(`${API_BASE_URL}/auth/logout`, {
        method: "POST",
        headers: { Authorization: `Bearer ${token}` },
      });
    } catch {
      // best-effort; local session is already cleared.
    }
  }
}
