# @tbite/api-client

Typed HTTP client for the T-Bite API, generated from the OpenAPI contract.

- `createApiClient(baseUrl, accessToken?)` — an [`openapi-fetch`](https://github.com/openapi-ts/openapi-fetch) client typed against the API. Query arrays are serialised comma-delimited (`explode:false`) to match how Huma parses them.
- `paths`, `components`, `operations`, `ApiClient` — generated types for request/response bodies, query params, and schemas. Use these instead of `any` so backend contract changes surface as compile errors.

```ts
import { createApiClient } from "@tbite/api-client";

const api = createApiClient(API_BASE_URL, accessToken);
const { data, error } = await api.GET("/api/employee/menu", {
  params: { query: { plant, day } },
});
```

## Regenerating

`src/schema.d.ts` is generated — do not edit it by hand. After changing the Go API:

```sh
make contract-sync   # go run ./services/api/cmd/contract-export && pnpm --filter @tbite/api-client generate
```

CI's `contract-drift` job fails if the committed artifacts diverge from the Go source.
