<script lang="ts">
  /**
   * Lightweight inline-SVG icon set. Stroke-based, designed to inherit currentColor.
   * Name set matches the NavNode.icon identifiers declared in platform/navigation.ts.
   */
  interface Props {
    name: string;
    class?: string;
    size?: number;
  }

  let { name, class: className = "", size = 18 }: Props = $props();
</script>

{#snippet stroke(path: string)}
  <svg
    xmlns="http://www.w3.org/2000/svg"
    viewBox="0 0 24 24"
    width={size}
    height={size}
    fill="none"
    stroke="currentColor"
    stroke-width="1.75"
    stroke-linecap="round"
    stroke-linejoin="round"
    aria-hidden="true"
    class={className}
  >
    <!-- eslint-disable-next-line -->
    {@html path}
  </svg>
{/snippet}

{#if name === "home"}
  {@render stroke(`<path d="M3 11.5 12 4l9 7.5" /><path d="M5 10v10h14V10" />`)}
{:else if name === "search"}
  {@render stroke(`<circle cx="11" cy="11" r="7" /><path d="m20 20-3.5-3.5" />`)}
{:else if name === "bag"}
  {@render stroke(`<path d="M6 8h12l-1 12H7z" /><path d="M9 8V6a3 3 0 0 1 6 0v2" />`)}
{:else if name === "wallet"}
  {@render stroke(`<path d="M3 7h18v12H3z" /><path d="M3 7V5a1 1 0 0 1 1-1h14v3" /><circle cx="17" cy="13" r="1.2" />`)}
{:else if name === "today"}
  {@render stroke(`<rect x="3" y="4" width="18" height="17" rx="2" /><path d="M3 9h18M8 3v4M16 3v4" /><circle cx="12" cy="14" r="2" />`)}
{:else if name === "menu"}
  {@render stroke(`<path d="M4 6h16M4 12h16M4 18h10" />`)}
{:else if name === "calendar"}
  {@render stroke(`<rect x="3" y="4" width="18" height="17" rx="2" /><path d="M3 9h18M8 3v4M16 3v4" />`)}
{:else if name === "print"}
  {@render stroke(`<path d="M6 9V3h12v6" /><rect x="4" y="9" width="16" height="8" rx="1" /><path d="M6 17h12v4H6z" />`)}
{:else if name === "clipboard"}
  {@render stroke(`<rect x="6" y="4" width="12" height="17" rx="2" /><path d="M9 4h6v3H9z" /><path d="M9 12h6M9 16h6" />`)}
{:else if name === "shield"}
  {@render stroke(`<path d="M12 3 4 6v6c0 5 3.5 8 8 9 4.5-1 8-4 8-9V6z" />`)}
{:else if name === "chart"}
  {@render stroke(`<path d="M4 20V6M10 20v-8M16 20V10M22 20H2" />`)}
{:else if name === "inbox"}
  {@render stroke(`<path d="M3 13h4l2 3h6l2-3h4" /><path d="M3 13 6 5h12l3 8v6H3z" />`)}
{:else if name === "building"}
  {@render stroke(`<rect x="4" y="3" width="16" height="18" rx="1" /><path d="M9 8h2M13 8h2M9 12h2M13 12h2M9 16h2M13 16h2" />`)}
{:else if name === "cash"}
  {@render stroke(`<rect x="3" y="6" width="18" height="12" rx="2" /><circle cx="12" cy="12" r="2.5" /><path d="M6 9v6M18 9v6" />`)}
{:else if name === "alert"}
  {@render stroke(`<path d="M12 3 2 20h20z" /><path d="M12 10v5M12 17.5v.5" />`)}
{:else if name === "eye"}
  {@render stroke(`<path d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7S2 12 2 12z" /><circle cx="12" cy="12" r="3" />`)}
{:else}
  <!-- Fallback: simple dot -->
  {@render stroke(`<circle cx="12" cy="12" r="3" />`)}
{/if}
