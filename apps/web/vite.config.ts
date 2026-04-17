import tailwindcss from "@tailwindcss/vite";
import { sveltekit } from "@sveltejs/kit/vite";
import { defineConfig, loadEnv } from "vite";

import {
  buildDevServerProxy,
  mergeMissingEnv,
  resolvePublicApiBaseUrl,
  resolveRuntimeOrigin,
  resolveWorkspaceRoot
} from "./dev-server";

export default defineConfig(({ command, mode }) => {
  const workspaceRoot = resolveWorkspaceRoot(import.meta.url);
  mergeMissingEnv(process.env, loadEnv(mode, workspaceRoot, ""));

  const config = {
    envDir: workspaceRoot,
    plugins: [tailwindcss(), sveltekit()]
  };

  if (command !== "serve") {
    return config;
  }

  process.env.PUBLIC_API_BASE_URL = resolvePublicApiBaseUrl(process.env.PUBLIC_API_BASE_URL);

  return {
    ...config,
    server: {
      proxy: buildDevServerProxy(resolveRuntimeOrigin(process.env.PRELAUNCH_BIND_ADDR))
    }
  };
});
