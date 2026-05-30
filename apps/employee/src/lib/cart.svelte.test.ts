import { describe, it, expect, beforeEach } from "vitest";
import { cart } from "./cart.svelte";

const META = { name: "Bento", vendor: "v1", price: 100 };

beforeEach(() => {
  cart.clear();
});

describe("cart", () => {
  it("starts empty", () => {
    expect(cart.count).toBe(0);
    expect(cart.total).toBe(0);
    expect(cart.qty("x")).toBe(0);
  });

  it("add inserts a new line then increments an existing one", () => {
    cart.add("a", META);
    expect(cart.qty("a")).toBe(1);
    cart.add("a", META);
    expect(cart.qty("a")).toBe(2);
    expect(cart.count).toBe(2);
    expect(cart.total).toBe(200);
  });

  it("add stores optional image meta", () => {
    cart.add("a", { ...META, image: "img.png" });
    expect(cart.items.a.image).toBe("img.png");
  });

  it("inc on a missing line is a no-op", () => {
    cart.inc("missing");
    expect(cart.qty("missing")).toBe(0);
  });

  it("inc bumps an existing line", () => {
    cart.add("a", META);
    cart.inc("a");
    expect(cart.qty("a")).toBe(2);
  });

  it("dec on a missing line is a no-op", () => {
    cart.dec("missing");
    expect(cart.qty("missing")).toBe(0);
  });

  it("dec removes the line when it reaches zero", () => {
    cart.add("a", META);
    cart.dec("a");
    expect(cart.qty("a")).toBe(0);
    expect(cart.items.a).toBeUndefined();
  });

  it("dec decrements when above one", () => {
    cart.add("a", META);
    cart.inc("a");
    cart.dec("a");
    expect(cart.qty("a")).toBe(1);
  });

  it("remove deletes a line", () => {
    cart.add("a", META);
    cart.remove("a");
    expect(cart.items.a).toBeUndefined();
  });

  it("clear empties the cart", () => {
    cart.add("a", META);
    cart.add("b", META);
    cart.clear();
    expect(cart.count).toBe(0);
    expect(cart.items).toEqual({});
  });
});
