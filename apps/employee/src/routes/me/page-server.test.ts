import { describe, it, expect } from "vitest";
import { load } from "./+page.server";

function loadEvent(user: unknown) {
  return { locals: { user }, url: new URL("http://h/me") } as never;
}

describe("me load", () => {
  it("redirects to login when unauthenticated", () => {
    expect(() => load(loadEvent(null))).toThrowError(
      expect.objectContaining({ status: 303, location: "/login?return_to=%2Fme" }),
    );
  });
  it("returns empty object when authenticated", () => {
    expect(load(loadEvent({ id: "u1" }))).toEqual({});
  });
});
