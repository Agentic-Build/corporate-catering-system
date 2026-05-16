<script lang="ts">
  import { Card, ProviderButton, TBiteLogo } from "@tbite/ui";
  let { data } = $props();
  const returnTo = $derived(encodeURIComponent(data.returnTo));
</script>

<div class="mx-auto mt-16 max-w-md">
  <div class="mb-8 flex justify-center"><TBiteLogo size={40} /></div>
  <Card>
    <div class="text-[11px] font-bold uppercase tracking-eyebrow text-tb-red-600">
      Merchant Console · 商家後台
    </div>
    <h1 class="mt-1 text-2xl font-black tracking-tight text-tb-slate-900">商家登入</h1>
    <p class="mb-6 mt-1 text-sm text-tb-slate-500">使用商家 Email 登入後台</p>
    <div class="space-y-3">
      <ProviderButton provider="google" href={`/auth/start?provider=google&return_to=${returnTo}`}>
        使用 Google 繼續
      </ProviderButton>
      <ProviderButton provider="github" href={`/auth/start?provider=github&return_to=${returnTo}`}>
        使用 GitHub 繼續
      </ProviderButton>
    </div>
  </Card>
  <p class="mt-6 text-center text-xs uppercase tracking-eyebrow-wide text-tb-slate-500">
    T-Bite · Merchant
  </p>
</div>
