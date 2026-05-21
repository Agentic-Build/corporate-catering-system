// Runtime config for the SPA. In a static build there is no server, so the
// API base URL is baked in at build time via Vite env. `PUBLIC_API_BASE_URL`
// is read from `.env`; it defaults to the local dev API.
import { PUBLIC_API_BASE_URL } from "$env/static/public";

export const API_BASE_URL = PUBLIC_API_BASE_URL || "http://localhost:8080";

// Custom URL scheme the Tauri deep-link plugin registers; the OIDC callback
// (backend B4) redirects here with `?token=...` after a mobile login.
export const DEEP_LINK_SCHEME = "tbite";
