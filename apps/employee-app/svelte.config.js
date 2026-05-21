import adapter from "@sveltejs/adapter-static";
import { vitePreprocess } from "@sveltejs/vite-plugin-svelte";

// Static SPA: no SSR, every route falls back to index.html so the Tauri
// webview (and a plain browser) can client-route freely.
export default {
  preprocess: vitePreprocess(),
  kit: {
    adapter: adapter({ fallback: "index.html", precompress: false }),
  },
};
