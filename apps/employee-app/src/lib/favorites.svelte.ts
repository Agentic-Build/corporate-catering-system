// Client-side favorites store. The backend favorites endpoints are scoped
// to menu items per day; for the vendor-favourite UX in the mockup we keep
// a lightweight client set of vendor ids. Persisted to localStorage so the
// FavoritesScreen survives reloads.
//
// TODO: wire to API — map vendor favourites onto /api/employee/favorites
// once a vendor-level favourite endpoint exists.

const KEY = "tbite.favorites";

class Favorites {
  ids = $state<Set<string>>(new Set());

  hydrate(): void {
    if (typeof localStorage === "undefined") return;
    try {
      const raw = localStorage.getItem(KEY);
      if (raw) this.ids = new Set(JSON.parse(raw) as string[]);
    } catch {
      this.ids = new Set();
    }
  }

  has(id: string): boolean {
    return this.ids.has(id);
  }

  toggle(id: string): void {
    const next = new Set(this.ids);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    this.ids = next;
    if (typeof localStorage !== "undefined") {
      localStorage.setItem(KEY, JSON.stringify([...next]));
    }
  }
}

export const favorites = new Favorites();
