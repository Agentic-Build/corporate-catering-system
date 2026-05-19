// Rune-based client-side cart, mirroring apps/employee/src/lib/cart.svelte.ts.
// The backend has no cart table; checkout fires a single placeOrder call.
// A cart is scoped to one vendor — adding from a different vendor replaces it.

export interface CartLine {
  qty: number;
  name: string;
  price: number; // minor units, per single unit
}

class Cart {
  vendorId = $state<string | null>(null);
  vendorName = $state<string>("");
  items = $state<Record<string, CartLine>>({});

  count = $derived(Object.values(this.items).reduce((s, l) => s + l.qty, 0));
  total = $derived(Object.values(this.items).reduce((s, l) => s + l.qty * l.price, 0));

  qty(id: string): number {
    return this.items[id]?.qty ?? 0;
  }

  /** Set the absolute quantity of a line; 0 removes it. */
  set(
    id: string,
    qty: number,
    meta: Omit<CartLine, "qty">,
    vendor: { id: string; name: string },
  ): void {
    // Switching vendors clears the previous cart (one-vendor-per-order rule).
    if (this.vendorId && this.vendorId !== vendor.id) {
      this.items = {};
    }
    this.vendorId = vendor.id;
    this.vendorName = vendor.name;

    if (qty <= 0) {
      this.remove(id);
      return;
    }
    this.items = { ...this.items, [id]: { ...meta, qty } };
  }

  remove(id: string): void {
    const next = { ...this.items };
    delete next[id];
    this.items = next;
    if (Object.keys(next).length === 0) {
      this.vendorId = null;
      this.vendorName = "";
    }
  }

  clear(): void {
    this.items = {};
    this.vendorId = null;
    this.vendorName = "";
  }
}

export const cart = new Cart();
