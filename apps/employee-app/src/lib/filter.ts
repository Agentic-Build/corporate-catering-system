// Pure client-side meal filtering & sorting, applied to a flat MenuItem
// list. Used by both the home grid and the vendor-detail screen so the
// filter sheet behaves identically in both places.

import type { MenuItem } from "./api";
import type { MealFilters } from "./components/FilterSheet.svelte";

export function applyFilters(items: MenuItem[], f: MealFilters): MenuItem[] {
  const q = f.search.trim().toLowerCase();
  const min = f.priceMin === "" ? null : Number(f.priceMin);
  const max = f.priceMax === "" ? null : Number(f.priceMax);

  let out = items.filter((m) => {
    if (q && !m.name.toLowerCase().includes(q) && !m.description.toLowerCase().includes(q)) {
      return false;
    }
    if (min != null && m.price_minor < min) return false;
    if (max != null && m.price_minor > max) return false;
    if (f.showAvail && m.sold_out) return false;
    if (f.tags.length > 0) {
      const itemTags = [...(m.tags ?? []), ...(m.badges ?? [])];
      if (!f.tags.some((t) => itemTags.includes(t) || m.name.includes(t))) return false;
    }
    return true;
  });

  if (f.sortBy === "name") out = [...out].sort((a, b) => a.name.localeCompare(b.name));
  else if (f.sortBy === "price_asc")
    out = [...out].sort((a, b) => a.price_minor - b.price_minor);
  else if (f.sortBy === "price_desc")
    out = [...out].sort((a, b) => b.price_minor - a.price_minor);

  return out;
}
