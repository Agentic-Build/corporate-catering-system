<script lang="ts">
  import Icon from "./Icon.svelte";

  interface Props {
    provider: string;
    href: string;
    children?: import("svelte").Snippet;
  }
  let { provider, href, children }: Props = $props();
</script>

<a
  {href}
  class="inline-flex w-full items-center justify-center gap-3 rounded-lg border border-tb-slate-300 bg-white px-4 py-2.5 text-sm font-semibold text-tb-slate-800 transition hover:border-tb-slate-500 hover:text-tb-slate-950 focus:outline-none focus:ring-2 focus:ring-tb-slate-300"
  data-provider={provider}
>
  <Icon name="cog" class="h-4 w-4" />
  <span>{@render children?.()}</span>
</a>
