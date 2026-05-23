<script lang="ts" module>
  // T-Bite stroke icon set — ported 1:1 from reference_src/ui.jsx (I.*)
  // and ui_kits/tbite/components.jsx (I.*). 24×24 viewBox, stroke-only.
  export type IconName =
    | "cart"
    | "qr"
    | "plus"
    | "minus"
    | "chevron"
    | "filter"
    | "search"
    | "close"
    | "download"
    | "check"
    | "alert"
    | "doc"
    | "heart"
    | "home"
    | "bell"
    | "tag"
    | "wallet"
    | "card"
    | "cog"
    | "user"
    | "pin";
</script>

<script lang="ts">
  interface Props {
    name: IconName;
    class?: string;
  }
  let { name, class: className = "" }: Props = $props();

  // Stroke width — 2.2 for plus/minus (heavier), 2 for check, 1.8 otherwise.
  const strokeWidth = $derived(
    name === "plus" || name === "minus" ? "2.2" : name === "check" ? "2" : "1.8",
  );
</script>

<svg
  viewBox="0 0 24 24"
  fill="none"
  stroke="currentColor"
  stroke-width={strokeWidth}
  stroke-linecap="round"
  stroke-linejoin="round"
  class={className}
  aria-hidden="true"
>
  {#if name === "cart"}
    <path d="M3 4h2l2.4 12.3a2 2 0 0 0 2 1.7h8.2a2 2 0 0 0 2-1.6L21 8H6" />
    <circle cx="10" cy="20" r="1.4" />
    <circle cx="17" cy="20" r="1.4" />
  {:else if name === "qr"}
    <rect x="3" y="3" width="7" height="7" rx="1" />
    <rect x="14" y="3" width="7" height="7" rx="1" />
    <rect x="3" y="14" width="7" height="7" rx="1" />
    <path d="M14 14h3v3h-3zM20 14v3M14 20h3M20 20h1" />
  {:else if name === "plus"}
    <path d="M12 5v14M5 12h14" />
  {:else if name === "minus"}
    <path d="M5 12h14" />
  {:else if name === "chevron"}
    <path d="M6 9l6 6 6-6" />
  {:else if name === "filter"}
    <path d="M4 5h16M7 12h10M10 19h4" />
  {:else if name === "search"}
    <circle cx="11" cy="11" r="6" />
    <path d="M20 20l-3.2-3.2" />
  {:else if name === "close"}
    <path d="M6 6l12 12M18 6L6 18" />
  {:else if name === "download"}
    <path d="M12 4v11" />
    <path d="M7 11l5 5 5-5" />
    <path d="M5 20h14" />
  {:else if name === "check"}
    <path d="M5 12l5 5 9-11" />
  {:else if name === "alert"}
    <path d="M12 4l9 16H3z" />
    <path d="M12 10v5M12 18v.01" />
  {:else if name === "doc"}
    <path d="M7 3h7l5 5v13H7z" />
    <path d="M14 3v6h5" />
  {:else if name === "heart"}
    <path d="M12 20s-7-4.5-7-10a4 4 0 0 1 7-2.6A4 4 0 0 1 19 10c0 5.5-7 10-7 10z" />
  {:else if name === "home"}
    <path d="M4 11l8-7 8 7v9a1 1 0 0 1-1 1h-4v-6h-6v6H5a1 1 0 0 1-1-1z" />
  {:else if name === "bell"}
    <path d="M6 17V10a6 6 0 0 1 12 0v7l2 2H4z" />
    <path d="M10 21a2 2 0 0 0 4 0" />
  {:else if name === "tag"}
    <path d="M3 12V4h8l10 10-8 8z" />
    <circle cx="7" cy="8" r="1.2" />
  {:else if name === "wallet"}
    <path d="M3 7a2 2 0 0 1 2-2h13l3 3v11a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z" />
    <circle cx="17" cy="13" r="1.2" />
  {:else if name === "card"}
    <rect x="3" y="6" width="18" height="13" rx="2" />
    <path d="M3 10h18" />
  {:else if name === "cog"}
    <circle cx="12" cy="12" r="3" />
    <path
      d="M19.4 15a1.7 1.7 0 0 0 .3 1.9l.1.1a2 2 0 1 1-2.8 2.8l-.1-.1a1.7 1.7 0 0 0-1.9-.3 1.7 1.7 0 0 0-1 1.6V21a2 2 0 1 1-4 0v-.1a1.7 1.7 0 0 0-1-1.6 1.7 1.7 0 0 0-1.9.3l-.1.1a2 2 0 1 1-2.8-2.8l.1-.1a1.7 1.7 0 0 0 .3-1.9 1.7 1.7 0 0 0-1.6-1H3a2 2 0 1 1 0-4h.1a1.7 1.7 0 0 0 1.6-1 1.7 1.7 0 0 0-.3-1.9l-.1-.1a2 2 0 1 1 2.8-2.8l.1.1a1.7 1.7 0 0 0 1.9.3 1.7 1.7 0 0 0 1-1.6V3a2 2 0 1 1 4 0v.1a1.7 1.7 0 0 0 1 1.6 1.7 1.7 0 0 0 1.9-.3l.1-.1a2 2 0 1 1 2.8 2.8l-.1.1a1.7 1.7 0 0 0-.3 1.9 1.7 1.7 0 0 0 1.6 1H21a2 2 0 1 1 0 4h-.1a1.7 1.7 0 0 0-1.5 1z"
    />
  {:else if name === "user"}
    <circle cx="12" cy="8" r="3.4" />
    <path d="M5 20a7 7 0 0 1 14 0" />
  {:else if name === "pin"}
    <path d="M12 21s-7-7.5-7-13a7 7 0 0 1 14 0c0 5.5-7 13-7 13z" />
    <circle cx="12" cy="8" r="2.2" />
  {/if}
</svg>
