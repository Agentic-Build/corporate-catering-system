// Client-side only; checkout fires a one-shot `?/placeOrder` action — there is
// no backend cart table.

interface CartLine {
  qty: number;
  name: string;
  vendor: string;
  price: number;
  image?: string;
}

class Cart {
  items = $state<Record<string, CartLine>>({});

  count = $derived(Object.values(this.items).reduce((s, l) => s + l.qty, 0));
  total = $derived(Object.values(this.items).reduce((s, l) => s + l.qty * l.price, 0));

  qty(id: string): number {
    return this.items[id]?.qty ?? 0;
  }

  add(id: string, meta: Omit<CartLine, "qty">): void {
    const cur = this.items[id];
    this.items = {
      ...this.items,
      [id]: cur ? { ...cur, qty: cur.qty + 1 } : { ...meta, qty: 1 },
    };
  }

  inc(id: string): void {
    const cur = this.items[id];
    if (!cur) return;
    this.items = { ...this.items, [id]: { ...cur, qty: cur.qty + 1 } };
  }

  // Removes the line when qty drops to 0.
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
