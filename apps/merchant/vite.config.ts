import { sveltekit } from "@sveltejs/kit/vite";
import { defineConfig } from "vite";

export default defineConfig({
  plugins: [sveltekit()],
  server: { port: 5174, strictPort: true },
  // adapter-node externalises third-party deps by default, but the runner
  // image only ships build/ + package.json (no production node_modules), so
  // any externalised package fails at runtime with ERR_MODULE_NOT_FOUND.
  // openapi-fetch reaches the SSR bundle via @tbite/api-client (server load
  // functions) and qrcode is a top-level import in a route component; bundle
  // both in so the build is self-contained.
  ssr: { noExternal: ["openapi-fetch", "qrcode"] },
});
