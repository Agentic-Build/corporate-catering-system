import { zhTW } from "$lib/i18n/zh-tw";

const API_BASE_URL_REQUIRED_ERROR_CODE = "api-base-url-missing";
const PLANT_SCOPE_REQUIRED_ERROR_CODE = "plant-scope-required";

export interface ApiFailure {
  status: number | null;
  code: string;
  technicalMessage: string;
  localizedMessage: string;
}

export class ApiConfigurationError extends Error {
  constructor() {
    super("PUBLIC_API_BASE_URL must be configured to the API origin without a trailing /api segment");
    this.name = "ApiConfigurationError";
  }
}

export class PlantScopeError extends Error {
  constructor() {
    super("actor scope must include at least one plantId");
    this.name = "PlantScopeError";
  }
}

export function normalizeApiFailure(error: unknown): ApiFailure {
  if (error instanceof ApiConfigurationError) {
    return {
      status: null,
      code: API_BASE_URL_REQUIRED_ERROR_CODE,
      technicalMessage: error.message,
      localizedMessage: zhTW.api.failure.baseUrlMissing
    };
  }

  if (error instanceof PlantScopeError) {
    return {
      status: null,
      code: PLANT_SCOPE_REQUIRED_ERROR_CODE,
      technicalMessage: error.message,
      localizedMessage: zhTW.api.failure.plantScopeMissing
    };
  }

  if (isApiError(error)) {
    return {
      status: error.status,
      code: `http-${error.status}`,
      technicalMessage: error.message,
      localizedMessage: localizeApiStatus(error.status)
    };
  }

  if (error instanceof TypeError) {
    return {
      status: null,
      code: "network-error",
      technicalMessage: error.message,
      localizedMessage: zhTW.api.failure.network
    };
  }

  return {
    status: null,
    code: "unknown-api-failure",
    technicalMessage: error instanceof Error ? error.message : "unknown api failure",
    localizedMessage: zhTW.api.failure.unknown
  };
}

function localizeApiStatus(status: number): string {
  return (
    zhTW.api.failure.statusText[status as keyof typeof zhTW.api.failure.statusText] ??
    zhTW.api.failure.unknown
  );
}

function isApiError(error: unknown): error is Error & { status: number } {
  if (!error || typeof error !== "object") {
    return false;
  }

  const candidate = error as Partial<Error> & { status?: unknown };
  return (
    candidate.name === "ApiError" &&
    typeof candidate.message === "string" &&
    typeof candidate.status === "number"
  );
}
