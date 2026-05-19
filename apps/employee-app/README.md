# @tbite/employee-app

T-Bite employee mobile app — a SvelteKit static SPA wrapped by Tauri 2 for
iOS and Android. Reproduces the `T-Bite App.html` design as a real Svelte 5
app sharing the `@tbite/*` packages with the web apps.

## Architecture

- **Frontend**: SvelteKit 2 + Svelte 5 runes, `@sveltejs/adapter-static`
  (`ssr=false`, SPA fallback to `index.html`). Tailwind 3 via the
  `@tbite/tokens` preset.
- **Native shell**: `src-tauri/` — Tauri 2, targets iOS + Android.
- **Data**: `src/lib/api.ts` wraps `@tbite/api-client` (openapi-fetch) with
  a Bearer token and calls the real `/api/employee/*` endpoints.
- **Auth**: deep-link OIDC flow — see `src/lib/auth.ts`.

## Web SPA build (no native toolchain needed)

```sh
pnpm --filter @tbite/employee-app build      # static SPA → build/
pnpm --filter @tbite/employee-app dev        # dev server on :5180
pnpm --filter @tbite/employee-app check      # svelte-check
```

The API base URL is baked in at build time from `PUBLIC_API_BASE_URL`
(`.env`, see `.env.example`); it defaults to `http://localhost:8080`.

## Native build (M4 / M5 — requires a native toolchain)

Not compiled in CI here. Prerequisites:

- **Rust** toolchain (`rustup`, stable ≥ 1.77).
- **Tauri CLI 2**: `pnpm add -D @tauri-apps/cli` (or `cargo install tauri-cli`).
- **iOS**: macOS + Xcode + iOS targets
  (`rustup target add aarch64-apple-ios aarch64-apple-ios-sim x86_64-apple-ios`).
- **Android**: Android SDK + NDK + JDK 17, `ANDROID_HOME` / `NDK_HOME` set
  (`rustup target add aarch64-linux-android armv7-linux-androideabi i686-linux-android x86_64-linux-android`).

### One-time mobile project init

```sh
pnpm tauri ios init
pnpm tauri android init
```

### Run / build

```sh
pnpm tauri ios dev          # iOS simulator
pnpm tauri android dev      # Android emulator
pnpm tauri ios build
pnpm tauri android build
```

### Native plugins to install during bring-up (M5)

The JS plugin packages are intentionally NOT in `package.json` yet because
they require the Rust/native toolchain. Add them when wiring native:

```sh
pnpm add @tauri-apps/api @tauri-apps/plugin-opener \
         @tauri-apps/plugin-deep-link @tauri-apps/plugin-stronghold
```

Then replace the `TODO(M5)` stubs in `src/lib/auth.ts`:

- `startLogin` → `openUrl` from `@tauri-apps/plugin-opener`.
- `initDeepLinks` → `onOpenUrl` from `@tauri-apps/plugin-deep-link`.
- `storeToken` → `@tauri-apps/plugin-stronghold` (platform keychain).

The Rust side (`src-tauri/Cargo.toml`, `src/lib.rs`) already declares the
matching crates and registers the plugins.

## Deep-link auth flow

1. App opens the system browser at
   `{API}/auth/{provider}/start?app=employee-app`.
2. After OIDC the backend (B4) redirects to `tbite://auth?token=...`.
3. The OS routes that URL into the app; the deep-link handler calls
   `completeLogin(url)` which stores the token (stronghold on device).
4. The auth gate in `+layout.svelte` routes to the home screen.

The `tbite://` scheme is declared in `src-tauri/tauri.conf.json` under
`plugins.deep-link`.

## Status

| Screen | State |
| --- | --- |
| Login | done — deep-link flow (native plugin calls stubbed) |
| Home | done — `/api/employee/home`, vendor-grouped |
| VendorDetail | done — `/api/employee/menu`, filter sheet, cart |
| CartSheet | done — `placeOrder` to `/api/employee/orders` |
| Orders + detail | done — `/api/employee/orders` |
| TOTP | done — `/api/employee/orders/{id}/pickup-code` |
| Payroll | done — `/api/employee/payroll/current` |
| EntryDetailSheet | done — rating / complaint endpoints |
| Profile / Favorites | done — favorites are a client-side store |
| NotifModal | static sample list (no notifications endpoint) |

Remaining for native (M4/M5): install native plugins, `tauri ios/android
init`, real QR rendering, app icons/splash, signing, CI.
