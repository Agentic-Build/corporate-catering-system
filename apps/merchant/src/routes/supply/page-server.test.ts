import { describe, it, expect } from "vitest";
import { load } from "./+page.server";

describe("supply load", () => {
  it("permanently redirects to the dashboard", async () => {
    await expect(Promise.resolve().then(() => load({} as never))).rejects.toMatchObject({
      status: 301,
      location: "/",
    });
  });
});
