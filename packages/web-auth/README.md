# @tbite/web-auth

Shared authentication primitives for the three SvelteKit apps (employee / merchant / admin).

It provides a SvelteKit `Handle` that reads the `tbite_sid` cookie, verifies it against the API's `GET /me`, and populates `event.locals.user` (a `SessionUser | null`) plus `event.locals.apiToken`. Apps wire it up in their `src/hooks.server.ts`:

```ts
import { sequence } from "@sveltejs/kit/hooks";
import { createAuthHandle } from "@tbite/web-auth/server";
import { env } from "$env/dynamic/private";

export const handle = sequence(
  createAuthHandle({
    apiBaseUrl: env.API_BASE_URL ?? "http://localhost:8080",
    cookieSecure: env.NODE_ENV === "production",
    cookieDomain: env.COOKIE_DOMAIN || undefined,
  }),
);
```

Each app extends `App.Locals` in `src/app.d.ts` to expose `user` and `apiToken`. Route-level guards (e.g. via `+layout.server.ts`) consume `locals.user`.
