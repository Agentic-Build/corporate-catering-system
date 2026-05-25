import { sveltekit } from "@sveltejs/kit/vite";
import { defineConfig } from "vite";

export default defineConfig({
  plugins: [sveltekit()],
  server: { port: 5174, strictPort: true },
  // adapter-node externalises third-party deps by default, but the runner
  // image only ships build/ + package.json (no production node_modules), so
  // any externalised package fails at runtime with ERR_MODULE_NOT_FOUND.
  // openapi-fetch reaches the SSR bundle via @tbite/api-client (server load
  // functions); bundle it in so the build is self-contained.
  // (qrcode is CommonJS and is imported lazily in the browser only — see
  // routes/labels/+page.svelte — so it never enters the SSR graph.)
  ssr: { noExternal: ["openapi-fetch"] },
});
