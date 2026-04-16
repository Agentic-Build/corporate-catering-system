import { env } from "$env/dynamic/public";

import {
  AdminService,
  EmployeeService,
  OpenAPI,
  VendorService
} from "../../../../../../contract/generated/ts-client";
import { ApiError } from "../../../../../../contract/generated/ts-client/core/ApiError";

const DEFAULT_API_BASE_URL = "http://127.0.0.1:18080";

OpenAPI.BASE = env.PUBLIC_API_BASE_URL?.trim() || DEFAULT_API_BASE_URL;
OpenAPI.WITH_CREDENTIALS = true;
OpenAPI.CREDENTIALS = "include";
OpenAPI.HEADERS = {
  "Accept-Language": "zh-TW"
};

export const apiClient = {
  admin: AdminService,
  employee: EmployeeService,
  vendor: VendorService
} as const;

export interface ApiFailure {
  status: number;
  message: string;
}

export function normalizeApiFailure(error: unknown): ApiFailure {
  if (error instanceof ApiError) {
    return {
      status: error.status,
      message: error.message
    };
  }

  return {
    status: 500,
    message: "unknown api failure"
  };
}
