import { describe, it, expect } from "vitest";
import { buildDays, taipeiISO, type PlantOption } from "./plants";

describe("plants re-exports", () => {
  it("re-exports web-shared date helpers", () => {
    expect(typeof buildDays).toBe("function");
    expect(typeof taipeiISO).toBe("function");
    expect(typeof taipeiISO()).toBe("string");
  });
  it("PlantOption shape is usable", () => {
    const p: PlantOption = { id: "tn-a", label: "Plant A" };
    expect(p.id).toBe("tn-a");
  });
});
