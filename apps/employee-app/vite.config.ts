import { sveltekit } from "@sveltejs/kit/vite";
import { defineConfig } from "vite";

// Vite config tuned for the Tauri webview. The dev server runs on a fixed
// port so `tauri.conf.json -> build.devUrl` can point at it deterministically.
export default defineConfig({
  plugins: [sveltekit()],
  clearScreen: false,
  server: { port: 5180, strictPort: true },
});
