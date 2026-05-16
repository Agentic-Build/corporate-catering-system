<script lang="ts">
  import { Card, ProviderButton, TBiteLogo } from "@tbite/ui";
  let { data } = $props();
</script>

<div class="mx-auto mt-16 max-w-md">
  <div class="mb-8 flex justify-center"><TBiteLogo size={40} /></div>
  <Card>
    <div class="text-[11px] font-bold uppercase tracking-eyebrow text-tb-red-600">
      Vendor Onboarding · 商家入駐
    </div>
    <h1 class="mt-1 text-2xl font-black tracking-tight text-tb-slate-900">商家入駐</h1>
    <p class="mt-2 text-sm text-tb-slate-500">
      邀請碼：<span class="font-jetbrains-mono text-tb-slate-800">{data.invite}</span>
    </p>
    <p class="mb-6 mt-1 text-sm text-tb-slate-500">
      請以 Google 或 GitHub 帳號完成註冊綁定。
    </p>
    <div class="space-y-3">
      <ProviderButton provider="google" href="/auth/start?provider=google&return_to=/">
        使用 Google 繼續
      </ProviderButton>
      <ProviderButton provider="github" href="/auth/start?provider=github&return_to=/">
        使用 GitHub 繼續
      </ProviderButton>
    </div>
  </Card>
  <p
    class="mt-6 text-center text-xs uppercase tracking-eyebrow-wide text-tb-slate-500"
  >
    T-Bite · Merchant
  </p>
</div>
