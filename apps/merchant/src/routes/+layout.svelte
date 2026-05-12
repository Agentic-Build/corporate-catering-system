<script lang="ts">
  import "../app.css";
  import { TBiteLogo } from "@tbite/ui";
  let { children } = $props();
</script>

<header
  class="sticky top-0 z-40 bg-white/95 px-6 py-4 shadow-tb-sm backdrop-blur"
>
  <TBiteLogo />
</header>
<main class="mx-auto max-w-[1400px] px-4 py-8 md:px-8">{@render children()}</main>
