export function resolveConfiguredApiBaseUrl(publicApiBaseUrl: string | undefined): string | null {
  if (publicApiBaseUrl === undefined) {
    return null;
  }

  const baseUrl = publicApiBaseUrl.trim();
  if (publicApiBaseUrl !== baseUrl) {
    return null;
  }

  if (baseUrl.endsWith("/api") || baseUrl.endsWith("/api/")) {
    return null;
  }

  return baseUrl;
}
