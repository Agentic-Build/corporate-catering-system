<script lang="ts">
  import { zhTW } from "$lib/i18n/zh-tw";
  import type { PortalRole } from "$lib/platform/navigation";

  interface Props {
    role: PortalRole;
    sectionId: string;
    actorDisplayName: string;
    actorId: string;
    provider: string;
    experienceMode: "mobile-first" | "desktop-first";
  }

  let { role, sectionId, actorDisplayName, actorId, provider, experienceMode }: Props = $props();

  const sectionTitle = $derived.by(() => {
    const sections = zhTW.nav.sections[role] as Record<string, string>;
    return sections[sectionId] ?? sectionId;
  });

  const sectionDescription = $derived.by(() => {
    const descriptions = zhTW.portal[role].sectionDescriptions as Record<string, string>;
    return descriptions[sectionId] ?? "";
  });

  const experienceLabel = $derived(
    experienceMode === "mobile-first"
      ? zhTW.portalSurface.experience.mobileFirst
      : zhTW.portalSurface.experience.desktopFirst
  );
</script>

<section class="grid gap-4">
  <header class="rounded-xl border border-cyan-100 bg-cyan-50/70 p-4">
    <p class="text-xs font-semibold tracking-[0.14em] text-cyan-800">{zhTW.nav.portals[role]}</p>
    <h2 class="mt-1 text-xl font-bold text-slate-950">{sectionTitle}</h2>
    <p class="mt-2 text-sm text-slate-700">{sectionDescription}</p>
  </header>

  <div class={`grid gap-3 ${experienceMode === "mobile-first" ? "grid-cols-1" : "grid-cols-1 md:grid-cols-3"}`}>
    <article class="rounded-xl border border-slate-200 bg-white p-4 shadow-sm">
      <h3 class="text-sm font-semibold text-slate-800">{zhTW.portalSurface.actorSummaryLabel}</h3>
      <p class="mt-2 text-sm text-slate-700">{actorDisplayName} ({actorId})</p>
    </article>

    <article class="rounded-xl border border-slate-200 bg-white p-4 shadow-sm">
      <h3 class="text-sm font-semibold text-slate-800">{zhTW.portalSurface.providerSummaryLabel}</h3>
      <p class="mt-2 text-sm text-slate-700">{provider}</p>
    </article>

    <article class="rounded-xl border border-slate-200 bg-white p-4 shadow-sm">
      <h3 class="text-sm font-semibold text-slate-800">{zhTW.portalSurface.experienceLabel}</h3>
      <p class="mt-2 text-sm text-slate-700">{experienceLabel}</p>
    </article>
  </div>

  <p class="text-sm text-slate-600">{zhTW.portalSurface.platformReady}</p>
</section>
