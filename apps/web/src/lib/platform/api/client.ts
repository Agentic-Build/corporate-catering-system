import { env } from "$env/dynamic/public";
import { zhTW } from "$lib/i18n/zh-tw";
import { errorState, successState, type AsyncState } from "$lib/platform/async-state";
import type { AuthActor } from "$lib/server/auth/contracts";
import {
  ApiConfigurationError,
  PlantScopeError,
  normalizeApiFailure,
  type ApiFailure
} from "./failure";

import {
  AdminService,
  EmployeeService,
  OpenAPI,
  VendorService
} from "../../../../../../contract/generated/ts-client";

let configuredBaseUrl: string | null = null;

export const apiClient = {
  admin: AdminService,
  employee: EmployeeService,
  vendor: VendorService
} as const;

export function ensureApiClientConfigured(): void {
  if (configuredBaseUrl !== null) {
    return;
  }

  const baseUrl = env.PUBLIC_API_BASE_URL?.trim();
  if (!baseUrl) {
    throw new ApiConfigurationError();
  }

  configuredBaseUrl = baseUrl;
  OpenAPI.BASE = baseUrl;
  OpenAPI.WITH_CREDENTIALS = true;
  OpenAPI.CREDENTIALS = "include";
  OpenAPI.HEADERS = {
    "Accept-Language": "zh-TW"
  };
}

export async function probeApiAccess(
  actor: AuthActor
): Promise<AsyncState<{ message: string }, string>> {
  try {
    ensureApiClientConfigured();

    if (actor.role === "employee") {
      const plantId = requirePlantId(actor);
      await apiClient.employee.listEmployeeOrders(plantId, undefined, undefined, 1, 1);
    } else if (actor.role === "vendor") {
      const plantId = requirePlantId(actor);
      await apiClient.vendor.listVendorOrders(plantId, undefined, undefined, 1, 1);
    } else {
      await apiClient.admin.listAdminVendors(1, 1);
    }

    return successState({
      message: zhTW.api.probe.success
    });
  } catch (error) {
    return errorState(normalizeApiFailure(error).localizedMessage);
  }
}

function requirePlantId(actor: AuthActor): string {
  const plantId = actor.scope.plantIds[0];
  if (!plantId) {
    throw new PlantScopeError();
  }

  return plantId;
}
