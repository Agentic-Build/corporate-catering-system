// Static SPA: disable SSR entirely. Routing is fully client-side inside the
// Tauri webview (or a browser); the static adapter's `fallback: index.html`
// serves every path, so prerendering is off (dynamic [id] routes have no
// crawlable static paths).
export const ssr = false;
export const prerender = false;
export const trailingSlash = "never";
