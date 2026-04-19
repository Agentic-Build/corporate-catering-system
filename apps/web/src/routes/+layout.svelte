<script lang="ts">
  import "../app.css";

  import { browser } from "$app/environment";
  import { navigating, page } from "$app/state";
  import { onMount, type Snippet } from "svelte";

  import { zhTW } from "$lib/i18n/zh-tw";
  import { probeApiAccess } from "$lib/platform/api";
  import { idleState, loadingState } from "$lib/platform/async-state";
  import type { PortalRole } from "$lib/platform/navigation";
  import { Icon, ToastRail, toasts } from "$lib/components/ui";

  import type { LayoutData } from "./$types";

  let { data, children }: { data: LayoutData; children: Snippet } = $props();

  let isOnline = $state(true);
  let bootstrapState = $state(idleState<{ message: string }, string>());
  let bootstrapProbeInFlight = false;
  let mobileNavOpen = $state(false);

  const experienceMode = $derived(data.experienceMode);
  const isMobileFirst = $derived(experienceMode === "mobile-first");
  const isAuthPage = $derived(page.url.pathname === "/" || page.url.pathname.startsWith("/auth"));

  onMount(() => {
    isOnline = navigator.onLine;
    const onlineHandler = () => (isOnline = true);
    const offlineHandler = () => (isOnline = false);
    window.addEventListener("online", onlineHandler);
    window.addEventListener("offline", offlineHandler);
    return () => {
      window.removeEventListener("online", onlineHandler);
      window.removeEventListener("offline", offlineHandler);
    };
  });

  $effect(() => {
    const actor = data.actor;
    const shouldProbe = browser && actor !== null && data.bootstrapState.status === "loading";
    bootstrapState = data.bootstrapState;
    if (shouldProbe) {
      void refreshBootstrapState(actor, data.auth.apiBearerToken);
    }
  });

  $effect(() => {
    // close mobile nav on route change
    void page.url.pathname;
    mobileNavOpen = false;
  });

  let lastFlashKey = $state<string | null>(null);
  $effect(() => {
    if (!browser) return;
    const flash = page.url.searchParams.get("flash");
    if (!flash) return;
    const key = `${page.url.pathname}?${flash}:${page.url.searchParams.get("attempted") ?? ""}`;
    if (key === lastFlashKey) return;
    lastFlashKey = key;

    if (flash === "cross-role") {
      const attempted = page.url.searchParams.get("attempted");
      const hint = attempted ? `（你嘗試前往：${decodeURIComponent(attempted)}）` : "";
      toasts.error(`此頁面不在你的角色範圍內，已為你返回本人入口${hint}`);
    } else if (flash === "auth-required") {
      toasts.info("請先登入才能進入該頁面");
    }

    // Clean the flash params from the URL without reloading.
    const clean = new URL(page.url);
    clean.searchParams.delete("flash");
    clean.searchParams.delete("attempted");
    clean.searchParams.delete("next");
    history.replaceState(history.state, "", clean.toString());
  });

  function portalLabel(role: PortalRole): string {
    return zhTW.nav.portals[role];
  }

  function resolveLabelKey(labelKey: string): string {
    const parts = labelKey.split(".");
    let node: unknown = zhTW;
    for (const part of parts) {
      if (node && typeof node === "object" && part in (node as Record<string, unknown>)) {
        node = (node as Record<string, unknown>)[part];
      } else {
        return labelKey;
      }
    }
    return typeof node === "string" ? node : labelKey;
  }

  async function refreshBootstrapState(
    actor: NonNullable<LayoutData["actor"]>,
    bearerToken: string | null
  ) {
    if (bootstrapProbeInFlight) return;
    bootstrapProbeInFlight = true;
    bootstrapState = loadingState();
    bootstrapState = await probeApiAccess(actor, bearerToken);
    bootstrapProbeInFlight = false;
  }
</script>

<svelte:head>
  <title>{zhTW.app.name}</title>
  <link rel="icon" href="/favicon.svg" type="image/svg+xml" />
</svelte:head>

{#if navigating.to}
  <div class="fixed inset-x-0 top-0 z-50 h-0.5 animate-pulse bg-gradient-to-r from-cyan-500 via-emerald-500 to-amber-500"></div>
{/if}

<div class="min-h-screen bg-slate-50 text-slate-900">
  {#if !isOnline}
    <aside class="bg-amber-100/90 px-4 py-2 text-sm text-amber-950 shadow-sm">
      <strong>{zhTW.shell.offlineTitle}</strong>
      <span class="ml-2">{zhTW.shell.offlineDescription}</span>
    </aside>
  {/if}

  {#if isAuthPage || !data.navigation.sectionPortal}
    <!-- Home / auth layout: no side nav, centered content -->
    <div class="mx-auto w-full max-w-4xl px-4 py-6 md:px-8">
      <header class="mb-6 flex items-center justify-between">
        <div class="grid gap-0.5">
          <h1 class="text-xl font-bold text-slate-950">{zhTW.app.name}</h1>
          <p class="text-xs text-slate-500">{zhTW.app.subtitle}</p>
        </div>
        {#if data.actor}
          <a
            class="inline-flex items-center rounded-lg border border-slate-300 px-3 py-1.5 text-sm font-medium text-slate-700 transition hover:border-slate-500 hover:text-slate-950"
            href="/auth/mock?logout=1&next=/"
          >
            {zhTW.shell.signOut}
          </a>
        {/if}
      </header>
      <main>{@render children()}</main>
    </div>
  {:else}
    <!-- Portal layout: desktop = side nav + content, mobile = top bar + content + bottom tab -->
    <div class="flex min-h-screen flex-col md:flex-row">
      <!-- Desktop side nav -->
      <aside class="hidden md:flex md:w-64 md:flex-col md:border-r md:border-slate-200 md:bg-white">
        <div class="border-b border-slate-200 p-4">
          <p class="text-[11px] font-semibold uppercase tracking-[0.12em] text-slate-500">
            {zhTW.app.name}
          </p>
          <p class="mt-1 text-sm font-bold text-slate-900">
            {portalLabel(data.navigation.sectionPortal)}
          </p>
          {#if data.actor}
            <p class="mt-0.5 text-xs text-slate-500">
              {data.actor.displayName}
            </p>
          {/if}
        </div>
        <nav aria-label={zhTW.shell.navLabel} class="flex-1 overflow-y-auto p-3">
          <ul class="grid gap-1">
            {#each data.navigation.primary as item}
              <li>
                <a
                  class={`grid grid-cols-[20px_1fr] items-start gap-2 rounded-lg px-3 py-2 text-sm transition ${item.active ? "bg-cyan-50 font-semibold text-cyan-800" : "text-slate-700 hover:bg-slate-100"}`}
                  href={item.href}
                >
                  <span class="mt-0.5">
                    {#if item.icon}<Icon name={item.icon} />{/if}
                  </span>
                  <span class="grid gap-0.5">
                    <span>{resolveLabelKey(item.labelKey)}</span>
                    {#if item.descriptionKey}
                      <span class="text-[11px] font-normal text-slate-500">{resolveLabelKey(item.descriptionKey)}</span>
                    {/if}
                  </span>
                </a>
              </li>
            {/each}
          </ul>
        </nav>
        <div class="border-t border-slate-200 p-3">
          <div class="mb-2 grid gap-1">
            {#each data.navigation.portalLinks as link}
              {#if !link.locked}
                <a
                  class={`rounded-md px-2 py-1 text-xs font-medium transition ${link.active ? "text-cyan-700" : "text-slate-500 hover:text-slate-900"}`}
                  href={link.href}
                >
                  {portalLabel(link.role)}
                </a>
              {/if}
            {/each}
          </div>
          <a
            class="block rounded-md border border-slate-200 px-2 py-1.5 text-center text-xs text-slate-600 hover:border-slate-400"
            href="/auth/mock?logout=1&next=/"
          >
            {zhTW.shell.signOut}
          </a>
        </div>
      </aside>

      <!-- Mobile top bar -->
      <header class="sticky top-0 z-30 border-b border-slate-200 bg-white/90 px-4 py-3 backdrop-blur md:hidden">
        <div class="flex items-center justify-between gap-3">
          <div class="grid">
            <p class="text-[10px] font-semibold uppercase tracking-[0.12em] text-slate-500">
              {portalLabel(data.navigation.sectionPortal)}
            </p>
            <p class="text-sm font-bold text-slate-900">{zhTW.app.name}</p>
          </div>
          <button
            type="button"
            class="rounded-lg border border-slate-300 px-2.5 py-1 text-xs font-medium text-slate-700"
            aria-expanded={mobileNavOpen}
            onclick={() => (mobileNavOpen = !mobileNavOpen)}
          >
            {mobileNavOpen ? zhTW.shell.closeMenu : zhTW.shell.openMenu}
          </button>
        </div>
        {#if mobileNavOpen}
          <nav aria-label={zhTW.shell.navLabel} class="mt-3 grid gap-1">
            {#each data.navigation.primary as item}
              <a
                class={`flex items-center gap-2 rounded-lg px-3 py-2 text-sm ${item.active ? "bg-cyan-50 font-semibold text-cyan-800" : "text-slate-700 hover:bg-slate-100"}`}
                href={item.href}
              >
                {#if item.icon}<Icon name={item.icon} />{/if}
                {resolveLabelKey(item.labelKey)}
              </a>
            {/each}
            <a
              class="mt-2 rounded-md border border-slate-200 px-3 py-2 text-center text-xs text-slate-600"
              href="/auth/mock?logout=1&next=/"
            >
              {zhTW.shell.signOut}
            </a>
          </nav>
        {/if}
      </header>

      <!-- Content area -->
      <main class="flex-1 pb-20 md:pb-8">
        <div class={`mx-auto w-full ${isMobileFirst ? "max-w-3xl" : "max-w-7xl"} px-4 py-5 md:px-8`}>
          {#if bootstrapState.status === "error"}
            <div class="mb-4 rounded-lg border border-rose-200 bg-rose-50 px-3 py-2 text-sm text-rose-900">
              {bootstrapState.error}
            </div>
          {/if}
          {@render children()}
        </div>
      </main>

      <!-- Mobile bottom tab bar (all roles: shows up to 5 primary sections) -->
      {#if data.navigation.primary.length > 0}
        {@const bottomItems = data.navigation.primary.slice(0, 5)}
        <nav aria-label={zhTW.shell.navLabel} class="fixed inset-x-0 bottom-0 z-30 border-t border-slate-200 bg-white/95 backdrop-blur md:hidden">
          <ul class={`grid`} style={`grid-template-columns: repeat(${bottomItems.length}, minmax(0, 1fr))`}>
            {#each bottomItems as item}
              <li>
                <a
                  class={`grid place-items-center gap-0.5 py-2 text-[11px] ${item.active ? "font-semibold text-cyan-700" : "text-slate-600"}`}
                  href={item.href}
                >
                  {#if item.icon}<Icon name={item.icon} size={18} />{/if}
                  <span>{resolveLabelKey(item.labelKey)}</span>
                </a>
              </li>
            {/each}
          </ul>
        </nav>
      {/if}
    </div>
  {/if}

  <ToastRail />
</div>
