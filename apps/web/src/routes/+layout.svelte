<script lang="ts">
  import "../app.css";

  import { browser } from "$app/environment";
  import { navigating } from "$app/state";
  import { onMount, type Snippet } from "svelte";

  import { zhTW } from "$lib/i18n/zh-tw";
  import { probeApiAccess } from "$lib/platform/api";
  import { idleState, loadingState } from "$lib/platform/async-state";
  import type { PortalRole } from "$lib/platform/navigation";
  import { resolveLayoutPresentation } from "$lib/platform/presentation";

  import type { LayoutData } from "./$types";

  let { data, children }: { data: LayoutData; children: Snippet } = $props();

  let isOnline = $state(true);
  let bootstrapState = $state(idleState<{ message: string }, string>());
  let bootstrapProbeInFlight = false;

  const presentation = $derived(resolveLayoutPresentation(data.experienceMode));

  onMount(() => {
    isOnline = navigator.onLine;

    const handleOnline = () => {
      isOnline = true;
    };

    const handleOffline = () => {
      isOnline = false;
    };

    window.addEventListener("online", handleOnline);
    window.addEventListener("offline", handleOffline);

    return () => {
      window.removeEventListener("online", handleOnline);
      window.removeEventListener("offline", handleOffline);
    };
  });

  $effect(() => {
    const actor = data.actor;
    const shouldProbe = browser && actor !== null && data.bootstrapState.status === "loading";

    bootstrapState = data.bootstrapState;

    if (shouldProbe) {
      void refreshBootstrapState(actor);
    }
  });

  function portalLabel(role: PortalRole): string {
    return zhTW.nav.portals[role];
  }

  function sectionLabel(role: PortalRole, sectionId: string): string {
    const sections = zhTW.nav.sections[role] as Record<string, string>;
    return sections[sectionId] ?? sectionId;
  }

  function sectionDescription(role: PortalRole, sectionId: string): string {
    const descriptions = zhTW.portal[role].sectionDescriptions as Record<string, string>;
    return descriptions[sectionId] ?? "";
  }

  function formatEpoch(epochMs: number | null): string {
    if (epochMs === null) {
      return "-";
    }

    return new Date(epochMs).toLocaleString("zh-TW", {
      hour12: false,
      timeZone: "Asia/Taipei"
    });
  }

  async function refreshBootstrapState(actor: NonNullable<LayoutData["actor"]>) {
    if (bootstrapProbeInFlight) {
      return;
    }

    bootstrapProbeInFlight = true;
    bootstrapState = loadingState();
    bootstrapState = await probeApiAccess(actor);
    bootstrapProbeInFlight = false;
  }
</script>

<svelte:head>
  <title>{zhTW.app.name}</title>
</svelte:head>

{#if navigating.to}
  <div class="fixed inset-x-0 top-0 z-50 h-1 bg-gradient-to-r from-cyan-500 via-emerald-500 to-amber-500 animate-pulse"></div>
{/if}

<div class="min-h-screen bg-[radial-gradient(circle_at_top,_#dff7f7,_#f8fafc_45%,_#fef7ed)] text-slate-900">
  {#if !isOnline}
    <aside class="bg-amber-100/90 px-4 py-3 text-sm text-amber-950 shadow-sm">
      <strong>{zhTW.shell.offlineTitle}</strong>
      <span class="ml-2">{zhTW.shell.offlineDescription}</span>
    </aside>
  {/if}

  <div class={presentation.shellContainerClass}>
    <header class="mb-4 grid gap-3 rounded-2xl border border-slate-200 bg-white/95 p-4 shadow-sm md:p-5">
      <div class="flex flex-wrap items-center justify-between gap-3">
        <div>
          <p class="text-xs font-semibold tracking-[0.18em] text-cyan-700">{data.locale}</p>
          <h1 class="text-2xl font-bold text-slate-950">
            {zhTW.app.name}
          </h1>
          <p class="text-sm text-slate-600">{zhTW.app.subtitle}</p>
        </div>

        {#if data.actor}
          <a
            class="inline-flex items-center rounded-lg border border-slate-300 px-3 py-2 text-sm font-medium text-slate-700 transition hover:border-slate-500 hover:text-slate-950"
            href="/auth/mock?logout=1&next=/"
          >
            {zhTW.shell.signOut}
          </a>
        {/if}
      </div>

      <dl class="grid gap-2 text-sm text-slate-700 md:grid-cols-2 xl:grid-cols-4">
        <div>
          <dt class="text-slate-500">{zhTW.shell.actorLabel}</dt>
          <dd class="font-medium">{data.actor ? `${data.actor.displayName} (${data.actor.id})` : zhTW.shell.notSignedIn}</dd>
        </div>
        <div>
          <dt class="text-slate-500">{zhTW.shell.providerLabel}</dt>
          <dd class="font-medium">{data.auth.provider}</dd>
        </div>
        <div>
          <dt class="text-slate-500">{zhTW.shell.refreshAfterLabel}</dt>
          <dd class="font-medium">{formatEpoch(data.auth.refreshAfterEpochMs)}</dd>
        </div>
        <div>
          <dt class="text-slate-500">{zhTW.shell.expiresAtLabel}</dt>
          <dd class="font-medium">{formatEpoch(data.auth.expiresAtEpochMs)}</dd>
        </div>
      </dl>
    </header>

    <nav aria-label={zhTW.shell.navLabel} class={presentation.navPanelClass}>
      <div class="grid gap-2">
        <p class="text-xs font-semibold tracking-[0.12em] text-slate-500">{zhTW.nav.portalLinksLabel}</p>
        <div class="flex flex-wrap gap-2">
          {#each data.navigation.portalLinks as portalLink}
            {#if portalLink.locked}
              <span
                class="inline-flex cursor-not-allowed items-center rounded-full border border-slate-200 bg-slate-100 px-3 py-2 text-xs font-semibold text-slate-400"
                title={zhTW.nav.lockedHint}
              >
                {portalLabel(portalLink.role)}
              </span>
            {:else}
              <a
                class={`inline-flex items-center rounded-full border px-3 py-2 text-xs font-semibold transition ${portalLink.active ? "border-cyan-700 bg-cyan-700 text-white" : "border-slate-300 bg-white text-slate-700 hover:border-slate-500 hover:text-slate-950"}`}
                href={portalLink.href}
              >
                {portalLabel(portalLink.role)}
              </a>
            {/if}
          {/each}
        </div>
      </div>

      {#if data.navigation.sectionPortal}
        <div class="grid gap-2">
          <p class="text-xs font-semibold tracking-[0.12em] text-slate-500">{zhTW.nav.sectionLinksLabel}</p>
          <div
            class={`grid gap-2 ${presentation.sectionGridClass}`}
          >
            {#each data.navigation.sectionLinks as sectionLink}
              <a
                class={`grid gap-1 rounded-xl border px-3 py-3 transition ${sectionLink.active ? "border-emerald-500 bg-emerald-50" : "border-slate-200 bg-white hover:border-slate-400"}`}
                href={sectionLink.href}
              >
                <span class="text-sm font-semibold text-slate-900">
                  {sectionLabel(data.navigation.sectionPortal, sectionLink.id)}
                </span>
                <span class="text-xs text-slate-600">
                  {sectionDescription(data.navigation.sectionPortal, sectionLink.id)}
                </span>
              </a>
            {/each}
          </div>
        </div>
      {/if}
    </nav>

    <section class="mb-4 mt-4 rounded-2xl border border-slate-200 bg-white/85 p-4 text-sm text-slate-700 shadow-sm">
      {#if bootstrapState.status === "idle"}
        {zhTW.asyncState.idle}
      {:else if bootstrapState.status === "loading"}
        {zhTW.asyncState.loading}
      {:else if bootstrapState.status === "success"}
        {bootstrapState.data.message}
      {:else}
        {bootstrapState.error}
      {/if}
    </section>

    <main class="rounded-2xl border border-slate-200 bg-white/92 p-4 shadow-sm md:p-6">
      {@render children()}
    </main>
  </div>
</div>
