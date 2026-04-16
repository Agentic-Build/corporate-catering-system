<script lang="ts">
  import { zhTW } from "$lib/i18n/zh-tw";
  import type { PageData } from "./$types";

  let { data }: { data: PageData } = $props();
</script>

<section class="grid gap-4">
  <header class="rounded-xl border border-emerald-100 bg-emerald-50/70 p-4">
    <h2 class="text-xl font-bold text-slate-950">{zhTW.home.title}</h2>
    <p class="mt-2 text-sm text-slate-700">{zhTW.home.description}</p>
  </header>

  <section class="grid gap-3 rounded-xl border border-slate-200 bg-white p-4 shadow-sm">
    <h3 class="text-sm font-semibold text-slate-800">{zhTW.home.portalPreviewLabel}</h3>
    <div class="grid gap-2 md:grid-cols-3">
      <article class="rounded-lg border border-slate-200 bg-slate-50 p-3">
        <p class="text-sm font-semibold text-slate-900">{zhTW.nav.portals.employee}</p>
        <p class="mt-1 text-sm text-slate-700">{zhTW.portal.employee.lead}</p>
      </article>
      <article class="rounded-lg border border-slate-200 bg-slate-50 p-3">
        <p class="text-sm font-semibold text-slate-900">{zhTW.nav.portals.vendor}</p>
        <p class="mt-1 text-sm text-slate-700">{zhTW.portal.vendor.lead}</p>
      </article>
      <article class="rounded-lg border border-slate-200 bg-slate-50 p-3">
        <p class="text-sm font-semibold text-slate-900">{zhTW.nav.portals.admin}</p>
        <p class="mt-1 text-sm text-slate-700">{zhTW.portal.admin.lead}</p>
      </article>
    </div>
  </section>

  <section class="grid gap-3 rounded-xl border border-slate-200 bg-white p-4 shadow-sm">
    <h3 class="text-sm font-semibold text-slate-800">{zhTW.home.signInActionsLabel}</h3>
    <div class="grid gap-2 md:grid-cols-2">
      <a
        class="rounded-lg border border-cyan-600 px-3 py-2 text-sm font-semibold text-cyan-700 transition hover:bg-cyan-50"
        href="/auth/mock?role=employee&next=/employee"
      >
        {zhTW.home.signIn.employee}
      </a>
      <a
        class="rounded-lg border border-cyan-600 px-3 py-2 text-sm font-semibold text-cyan-700 transition hover:bg-cyan-50"
        href="/auth/mock?role=vendor&next=/vendor"
      >
        {zhTW.home.signIn.vendor}
      </a>
      <a
        class="rounded-lg border border-cyan-600 px-3 py-2 text-sm font-semibold text-cyan-700 transition hover:bg-cyan-50"
        href="/auth/mock?role=admin&next=/admin"
      >
        {zhTW.home.signIn.admin}
      </a>
      <a
        class="rounded-lg border border-slate-300 px-3 py-2 text-sm font-semibold text-slate-700 transition hover:bg-slate-100"
        href="/auth/mock?logout=1&next=/"
      >
        {zhTW.home.clearSession}
      </a>
    </div>
    {#if data.actor}
      <a
        class="inline-flex w-fit rounded-lg border border-emerald-600 px-3 py-2 text-sm font-semibold text-emerald-700 transition hover:bg-emerald-50"
        href={`/${data.actor.role}`}
      >
        {zhTW.home.continueToPortal}
      </a>
    {/if}
  </section>
</section>
