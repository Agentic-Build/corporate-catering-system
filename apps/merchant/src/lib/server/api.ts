import { createApiClient } from "@tbite/api-client";
import { API_BASE_URL } from "$lib/server/env";

export function apiFor(token: string | undefined) {
  return createApiClient(API_BASE_URL, token);
}
