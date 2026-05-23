import { describe, it, expect } from "vitest";
import { buildPickupQR, parsePickupQR } from "./index";

describe("pickup QR payload", () => {
  it("builds the tbite pickup URL", () => {
    expect(buildPickupQR("abc-123")).toBe("tbite://pickup?order=abc-123");
  });
  it("round-trips", () => {
    expect(parsePickupQR(buildPickupQR("o1"))).toEqual({ orderId: "o1" });
  });
  it("parses a valid payload", () => {
    expect(parsePickupQR("tbite://pickup?order=xyz")).toEqual({ orderId: "xyz" });
  });
  it("returns null when order param missing", () => {
    expect(parsePickupQR("tbite://pickup")).toBeNull();
  });
  it("returns null for wrong scheme/host", () => {
    expect(parsePickupQR("https://evil?order=x")).toBeNull();
    expect(parsePickupQR("tbite://other?order=x")).toBeNull();
  });
  it("returns null for garbage / empty", () => {
    expect(parsePickupQR("garbage")).toBeNull();
    expect(parsePickupQR("")).toBeNull();
  });
});
