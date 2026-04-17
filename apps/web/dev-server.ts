import { resolve } from "node:path";
import { fileURLToPath } from "node:url";

import type { ProxyOptions } from "vite";

export const DEFAULT_RUNTIME_BIND_ADDR = "127.0.0.1:18080";
export const DEFAULT_PUBLIC_API_BASE_URL = "";

export function resolveWorkspaceRoot(configFileUrl: string): string {
  return resolve(fileURLToPath(new URL(".", configFileUrl)), "../..");
}

export function mergeMissingEnv(
  targetEnv: NodeJS.ProcessEnv,
  loadedEnv: Record<string, string>
): void {
  for (const [name, value] of Object.entries(loadedEnv)) {
    if (targetEnv[name] === undefined) {
      targetEnv[name] = value;
    }
  }
}

export function resolveRuntimeOrigin(bindAddr: string | undefined): string {
  const normalized = bindAddr?.trim();
  if (!normalized) {
    return `http://${DEFAULT_RUNTIME_BIND_ADDR}`;
  }

  if (normalized.startsWith("http://") || normalized.startsWith("https://")) {
    return normalized.replace(/\/+$/, "");
  }

  return `http://${normalized}`;
}

export function resolvePublicApiBaseUrl(publicApiBaseUrl: string | undefined): string {
  const normalized = publicApiBaseUrl?.trim();
  return normalized && normalized.length > 0 ? normalized : DEFAULT_PUBLIC_API_BASE_URL;
}

export function buildDevServerProxy(
  runtimeOrigin: string
): Record<string, string | ProxyOptions> {
  return {
    "/api": {
      target: runtimeOrigin,
      changeOrigin: true
    },
    "/health": {
      target: runtimeOrigin,
      changeOrigin: true
    },
    "/mcp": {
      target: runtimeOrigin,
      changeOrigin: true
    }
  };
}
