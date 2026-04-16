<script lang="ts">
  import { getLocalizedErrorCopy } from "$lib/platform/error";
  import { zhTW } from "$lib/i18n/zh-tw";

  let { status }: { status: number } = $props();

  const localized = $derived(getLocalizedErrorCopy(status));
</script>

<section class="mx-auto grid max-w-2xl gap-4 rounded-2xl border border-rose-200 bg-rose-50/80 p-6 text-slate-900">
  <p class="text-xs font-semibold tracking-[0.14em] text-rose-700">{zhTW.error.statusCodePrefix} {status}</p>
  <h2 class="text-2xl font-bold">{localized.title}</h2>
  <p class="text-base">{localized.statusText}</p>
  <p class="text-sm text-slate-700">{localized.description}</p>
  <a
    class="inline-flex w-fit rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm font-semibold text-slate-700 transition hover:border-slate-500 hover:text-slate-950"
    href="/"
  >
    {zhTW.error.backHome}
  </a>
</section>
