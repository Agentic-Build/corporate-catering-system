<script lang="ts">
  import { Card, ProviderButton, TBiteLogo } from "@tbite/ui";
  let { data } = $props();
</script>

<div class="mx-auto mt-16 max-w-md">
  <div class="mb-8 flex justify-center"><TBiteLogo size={40} /></div>
  <Card>
    <h1 class="mb-1 text-xl font-black text-tb-slate-900">商家入駐</h1>
    <p class="mb-6 text-sm text-tb-slate-500">邀請碼：<span class="font-jetbrains-mono">{data.invite}</span></p>
    <p class="mb-6 text-sm text-tb-slate-500">請以 Google 或 GitHub 帳號完成註冊綁定。</p>
    <div class="space-y-3">
      <ProviderButton provider="google" href="/auth/start?provider=google&return_to=/">
        使用 Google 繼續
      </ProviderButton>
      <ProviderButton provider="github" href="/auth/start?provider=github&return_to=/">
        使用 GitHub 繼續
      </ProviderButton>
    </div>
  </Card>
</div>
