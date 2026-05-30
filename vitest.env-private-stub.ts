// Test-only stand-in for SvelteKit's `$env/dynamic/private` virtual module,
// which Vite cannot resolve without the full sveltekit() plugin. Backed by
// process.env so tests can set values before importing server modules.
export const env: Record<string, string | undefined> = process.env;
