import createClient from "openapi-fetch";
import type { paths } from "./schema";

export function createApiClient(baseUrl: string, accessToken?: string) {
  return createClient<paths>({
    baseUrl,
    headers: accessToken ? { Authorization: `Bearer ${accessToken}` } : {},
  });
}

export type ApiClient = ReturnType<typeof createApiClient>;
export type { paths, components, operations } from "./schema";
