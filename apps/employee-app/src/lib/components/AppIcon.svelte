<script lang="ts" module>
  // Minimal inline-SVG icon set for the mobile app. Kept local (rather than
  // @tbite/ui's Icon) because the bottom-nav glyphs are app-specific.
  export type AppIconName =
    | "home"
    | "orders"
    | "qr"
    | "wallet"
    | "profile"
    | "bell"
    | "back"
    | "plus"
    | "minus"
    | "star"
    | "check"
    | "search";

  const PATHS: Record<AppIconName, { d: string; fill?: boolean }> = {
    home: { d: "M4 11l8-7 8 7v9a1 1 0 0 1-1 1h-4v-5h-6v5H5a1 1 0 0 1-1-1z" },
    orders: { d: "M7 3h7l5 5v13H7zM14 3v6h5M10 12h5M10 16h3" },
    qr: { d: "M3 4h6v6H3zM15 4h6v6h-6zM3 14h6v6H3zM15 14h3v3h-3zM21 14v3M15 20h3M21 20h0" },
    wallet: { d: "M3 7a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2v10a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2zM17 12h.01" },
    profile: { d: "M12 12a4 4 0 1 0 0-8 4 4 0 0 0 0 8zM4 20c0-4 3.6-7 8-7s8 3 8 7" },
    bell: { d: "M6 17V10a6 6 0 0 1 12 0v7l2 2H4zM10 21a2 2 0 0 0 4 0" },
    back: { d: "M15 18l-6-6 6-6" },
    plus: { d: "M12 5v14M5 12h14" },
    minus: { d: "M5 12h14" },
    star: {
      d: "M12 2l3.09 6.26L22 9.27l-5 4.87 1.18 6.88L12 17.77l-6.18 3.25L7 14.14 2 9.27l6.91-1.01z",
      fill: true,
    },
    check: { d: "M5 12l5 5 9-11" },
    search: { d: "M11 18a7 7 0 1 0 0-14 7 7 0 0 0 0 14zM20 20l-3-3" },
  };

  export const ICON_PATHS = PATHS;
</script>

<script lang="ts">
  interface Props {
    name: AppIconName;
    class?: string;
  }
  let { name, class: cls = "h-5 w-5" }: Props = $props();
  const icon = $derived(ICON_PATHS[name]);
</script>

<svg
  viewBox="0 0 24 24"
  class={cls}
  fill={icon.fill ? "currentColor" : "none"}
  stroke={icon.fill ? "none" : "currentColor"}
  stroke-width="2"
  stroke-linecap="round"
  stroke-linejoin="round"
  aria-hidden="true"
>
  <path d={icon.d} />
</svg>
