<script lang="ts">
  import { Card, ProviderButton, TBiteLogo } from "@tbite/ui";
  let { data } = $props();
  const r = encodeURIComponent(data.returnTo);
</script>

<div class="mx-auto mt-16 max-w-md">
  <div class="mb-8 flex justify-center"><TBiteLogo size={40} /></div>
  <Card>
    <h1 class="mb-1 text-xl font-black text-tb-slate-900">福委會登入</h1>
    <p class="mb-6 text-sm text-tb-slate-500">僅限白名單成員</p>
    <div class="space-y-3">
      <ProviderButton provider="google" href={`/auth/start?provider=google&return_to=${r}`}>
        使用 Google 繼續
      </ProviderButton>
      <ProviderButton provider="github" href={`/auth/start?provider=github&return_to=${r}`}>
        使用 GitHub 繼續
      </ProviderButton>
    </div>
  </Card>
  <p class="mt-6 text-center text-xs uppercase tracking-eyebrow-wide text-tb-slate-500">
    Corporate Catering · Welfare
  </p>
</div>
