<script lang="ts" module>
  // Stable per-tag swatch + glyph. The reference TbCategoryStrip uses a fixed
  // category list; here categories are data-driven (distinct day_menu tags),
  // so known tags get a hand-tuned palette and any other tag falls back to a
  // deterministic palette/glyph picked from its name.
  const KNOWN: Record<string, { palette: string; glyph: string }> = {
    hot: { palette: "from-tb-rose-100 to-tb-rose-200", glyph: "▮" },
    healthy: { palette: "from-tb-emerald-100 to-tb-emerald-200", glyph: "◐" },
    veggie: { palette: "from-tb-lime-100 to-tb-emerald-100", glyph: "◌" },
    noodle: { palette: "from-tb-amber-100 to-tb-orange-200", glyph: "≋" },
    drink: { palette: "from-tb-cyan-100 to-tb-sky-200", glyph: "○" },
    japan: { palette: "from-tb-pink-100 to-tb-rose-200", glyph: "◆" },
    korean: { palette: "from-tb-stone-200 to-tb-amber-200", glyph: "▽" },
    western: { palette: "from-tb-yellow-100 to-tb-amber-200", glyph: "◇" },
    sweet: { palette: "from-tb-pink-100 to-tb-rose-100", glyph: "✿" },
  };
  const FALLBACK_PALETTES = [
    "from-tb-sky-100 to-tb-cyan-200",
    "from-tb-amber-100 to-tb-orange-200",
    "from-tb-emerald-100 to-tb-lime-200",
    "from-tb-pink-100 to-tb-rose-200",
    "from-tb-slate-100 to-tb-slate-200",
  ];
  const FALLBACK_GLYPHS = ["●", "◆", "▲", "■", "★"];

  function hash(s: string): number {
    let h = 0;
    for (let i = 0; i < s.length; i++) h = (h * 31 + s.charCodeAt(i)) | 0;
    return Math.abs(h);
  }
  export function tagStyle(id: string): { palette: string; glyph: string } {
    if (id === "all") return { palette: "from-tb-slate-100 to-tb-slate-200", glyph: "★" };
    if (KNOWN[id]) return KNOWN[id];
    const h = hash(id);
    return {
      palette: FALLBACK_PALETTES[h % FALLBACK_PALETTES.length],
      glyph: FALLBACK_GLYPHS[h % FALLBACK_GLYPHS.length],
    };
  }
</script>

<script lang="ts">
  // Category filter strip — ported from EmployeeView.jsx TbCategoryStrip.
  // `categories` is built by the home page from distinct day_menu tags.
  interface Category {
    id: string;
    label: string;
  }
  interface Props {
    categories: Category[];
    active: string;
    onChange: (id: string) => void;
  }
  let { categories, active, onChange }: Props = $props();
</script>

<div class="no-scrollbar -mx-1 flex gap-2 overflow-x-auto px-1 pb-2">
  {#each categories as c (c.id)}
    {@const on = active === c.id}
    {@const s = tagStyle(c.id)}
    <button
      type="button"
      onclick={() => onChange(c.id)}
      class="group flex min-w-[84px] flex-shrink-0 flex-col items-center gap-1.5 rounded-tb-xl px-2 py-2 transition hover:bg-tb-slate-50"
    >
      <div
        class="grid h-16 w-16 place-items-center rounded-full bg-gradient-to-br {s.palette} ring-1 ring-inset ring-black/5 transition
          {on ? 'ring-2 ring-tb-red-500 ring-offset-2 ring-offset-white' : ''}"
      >
        <span class="font-jetbrains-mono text-2xl text-tb-slate-700/80">{s.glyph}</span>
      </div>
      <span class="text-xs font-bold {on ? 'text-tb-red-700' : 'text-tb-slate-700'}">{c.label}</span
      >
    </button>
  {/each}
</div>
