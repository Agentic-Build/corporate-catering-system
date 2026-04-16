import type { RequestEvent } from "@sveltejs/kit";

export class MemoryCookies {
  private readonly values = new Map<string, string>();

  get(name: string): string | undefined {
    return this.values.get(name);
  }

  set(name: string, value: string): void {
    this.values.set(name, value);
  }

  delete(name: string): void {
    this.values.delete(name);
  }
}

export function createRequestEvent(
  path: string,
  cookies: MemoryCookies,
  init?: {
    headers?: HeadersInit;
  }
): RequestEvent {
  const url = new URL(path, "http://localhost");
  const request = new Request(url, {
    headers: init?.headers
  });

  return {
    url,
    request,
    cookies,
    locals: {}
  } as unknown as RequestEvent;
}
