import createClient from "openapi-fetch";
import type { paths } from "./schema";

export function createApiClient(baseUrl: string, accessToken?: string) {
  return createClient<paths>({
    baseUrl,
    headers: accessToken ? { Authorization: `Bearer ${accessToken}` } : {},
    // The OpenAPI contract declares array query params with explode:false
    // (comma-delimited, e.g. tags=a,b) and Huma parses them that way. Match it
    // here; openapi-fetch otherwise defaults to explode:true (tags=a&tags=b),
    // which Huma reads as only the first value.
    querySerializer: { array: { style: "form", explode: false } },
  });
}

export type ApiClient = ReturnType<typeof createApiClient>;
export type { paths, components, operations } from "./schema";
