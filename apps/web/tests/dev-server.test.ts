import assert from "node:assert/strict";
import { describe, it } from "node:test";

import {
  buildDevServerProxy,
  mergeMissingEnv,
  resolvePublicApiBaseUrl,
  resolveRuntimeOrigin
} from "../dev-server";

describe("dev server helpers", () => {
  it("preserves explicit env values when loading workspace env files", () => {
    const targetEnv: NodeJS.ProcessEnv = {
      PRELAUNCH_BIND_ADDR: "127.0.0.1:19090"
    };

    mergeMissingEnv(targetEnv, {
      PRELAUNCH_BIND_ADDR: "127.0.0.1:18080",
      CORPORATE_SSO_JWT_ISSUER: "https://local.catering.dev"
    });

    assert.equal(targetEnv.PRELAUNCH_BIND_ADDR, "127.0.0.1:19090");
    assert.equal(targetEnv.CORPORATE_SSO_JWT_ISSUER, "https://local.catering.dev");
  });

  it("defaults browser API traffic to a same-origin proxy during local dev", () => {
    assert.equal(resolvePublicApiBaseUrl(undefined), "");
    assert.equal(resolvePublicApiBaseUrl(" "), "");
  });

  it("derives the runtime origin from bind addresses and full URLs", () => {
    assert.equal(resolveRuntimeOrigin(undefined), "http://127.0.0.1:18080");
    assert.equal(resolveRuntimeOrigin("127.0.0.1:19090"), "http://127.0.0.1:19090");
    assert.equal(resolveRuntimeOrigin("https://runtime.local/"), "https://runtime.local");
  });

  it("proxies runtime endpoints through vite", () => {
    const proxy = buildDevServerProxy("http://127.0.0.1:18080");

    assert.deepEqual(Object.keys(proxy), ["/api", "/health", "/mcp"]);
    assert.equal((proxy["/api"] as { target: string }).target, "http://127.0.0.1:18080");
  });
});
