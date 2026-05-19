// Client-side auth/session store. The Bearer token is the single source of
// truth for "logged in". On native it is persisted by the secure-storage
// layer (see auth.ts); in a plain browser it falls back to localStorage so
// `pnpm dev`/`pnpm build` previews work without the Tauri runtime.

const TOKEN_KEY = "tbite.token";

export interface SessionUser {
  display_name: string;
  plant: string;
}

class Session {
  token = $state<string | null>(null);
  user = $state<SessionUser | null>(null);
  ready = $state(false);

  get isAuthed(): boolean {
    return this.token != null;
  }

  /** Restore a previously stored token (browser fallback path). */
  hydrate(): void {
    if (typeof localStorage !== "undefined") {
      this.token = localStorage.getItem(TOKEN_KEY);
    }
    this.ready = true;
  }

  setToken(token: string): void {
    this.token = token;
    if (typeof localStorage !== "undefined") {
      localStorage.setItem(TOKEN_KEY, token);
    }
  }

  setUser(user: SessionUser): void {
    this.user = user;
  }

  clear(): void {
    this.token = null;
    this.user = null;
    if (typeof localStorage !== "undefined") {
      localStorage.removeItem(TOKEN_KEY);
    }
  }
}

export const session = new Session();
