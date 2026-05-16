// Rune-based shared cart store. The cart is purely client-side state — the
// backend has no cart table; checkout fires a one-shot `?/placeOrder` action.
// Imported as a singleton so the header badge, floating bar, drawer and the
// home grid all read/write the same instance.

export interface CartLine {
  qty: number;
  name: string;
  vendor: string;
  price: number; // minor units, per single unit
  image?: string;
}

class Cart {
  items = $state<Record<string, CartLine>>({});

  count = $derived(Object.values(this.items).reduce((s, l) => s + l.qty, 0));
  total = $derived(Object.values(this.items).reduce((s, l) => s + l.qty * l.price, 0));

  /** Quantity of a single item (0 when absent). */
  qty(id: string): number {
    return this.items[id]?.qty ?? 0;
  }

  /** Add one unit, creating the line from `meta` if it does not exist yet. */
  add(id: string, meta: Omit<CartLine, "qty">): void {
    const cur = this.items[id];
    this.items = {
      ...this.items,
      [id]: cur ? { ...cur, qty: cur.qty + 1 } : { ...meta, qty: 1 },
    };
  }

  /** Increment an existing line by one. */
  inc(id: string): void {
    const cur = this.items[id];
    if (!cur) return;
    this.items = { ...this.items, [id]: { ...cur, qty: cur.qty + 1 } };
  }

  /** Decrement a line by one; removes it when it reaches zero. */
  dec(id: string): void {
    const cur = this.items[id];
    if (!cur) return;
    if (cur.qty <= 1) {
      this.remove(id);
      return;
    }
    this.items = { ...this.items, [id]: { ...cur, qty: cur.qty - 1 } };
  }

  remove(id: string): void {
    const next = { ...this.items };
    delete next[id];
    this.items = next;
  }

  clear(): void {
    this.items = {};
  }
}

export const cart = new Cart();
