<script lang="ts">
  interface Props {
    provider: "google" | "github";
    href: string;
    children?: import("svelte").Snippet;
  }
  let { provider, href, children }: Props = $props();
</script>

<a
  {href}
  class="inline-flex w-full items-center justify-center gap-3 rounded-lg border border-tb-slate-300 bg-white px-4 py-2.5 text-sm font-semibold text-tb-slate-800 transition hover:border-tb-slate-500 hover:text-tb-slate-950 focus:outline-none focus:ring-2 focus:ring-tb-slate-300"
>
  {#if provider === "google"}
    <svg viewBox="0 0 24 24" class="h-4 w-4" aria-hidden="true">
      <path fill="#EA4335" d="M12 10.2v3.9h5.5c-.2 1.3-1.5 3.7-5.5 3.7-3.3 0-6-2.7-6-6s2.7-6 6-6c1.9 0 3.1.8 3.8 1.5l2.6-2.5C16.7 3.2 14.6 2.3 12 2.3 6.7 2.3 2.5 6.6 2.5 11.9s4.2 9.6 9.5 9.6c5.5 0 9.1-3.9 9.1-9.4 0-.6-.1-1.1-.2-1.9H12z"/>
    </svg>
  {:else}
    <svg viewBox="0 0 24 24" class="h-4 w-4" aria-hidden="true">
      <path fill="#181717" d="M12 2C6.48 2 2 6.58 2 12.26c0 4.49 2.87 8.29 6.85 9.65.5.09.68-.22.68-.49v-1.7c-2.78.62-3.37-1.37-3.37-1.37-.46-1.18-1.11-1.5-1.11-1.5-.91-.63.07-.62.07-.62 1 .07 1.53 1.05 1.53 1.05.89 1.56 2.34 1.11 2.91.85.09-.66.35-1.11.63-1.37-2.22-.26-4.55-1.14-4.55-5.06 0-1.12.39-2.03 1.03-2.75-.1-.26-.45-1.3.1-2.71 0 0 .84-.27 2.75 1.05A9.4 9.4 0 0112 6.84a9.4 9.4 0 012.5.34c1.91-1.32 2.75-1.05 2.75-1.05.55 1.41.2 2.45.1 2.71.64.72 1.03 1.63 1.03 2.75 0 3.93-2.34 4.8-4.57 5.06.36.32.68.94.68 1.9v2.81c0 .27.18.59.69.49C19.13 20.55 22 16.75 22 12.26 22 6.58 17.52 2 12 2z"/>
    </svg>
  {/if}
  <span>{@render children?.()}</span>
</a>
