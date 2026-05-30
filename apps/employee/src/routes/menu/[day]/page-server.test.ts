import { describe, it, expect } from "vitest";
import { load } from "./+page.server";

describe("menu/[day] load", () => {
  it("redirects to home with the encoded day query", () => {
    expect(() => load({ params: { day: "2026 01/02" } } as never)).toThrowError(
      expect.objectContaining({ status: 303, location: "/?day=2026%2001%2F02" }),
    );
  });
});
