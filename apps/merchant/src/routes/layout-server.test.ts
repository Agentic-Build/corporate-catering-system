import { describe, it, expect } from "vitest";
import { load } from "./+layout.server";

describe("layout load", () => {
  it("exposes the current user from locals", () => {
    const user = { id: "u1", role: "vendor_operator" };
    expect(load({ locals: { user } } as never)).toEqual({ user });
  });

  it("passes through an undefined user", () => {
    expect(load({ locals: {} } as never)).toEqual({ user: undefined });
  });
});
