<script lang="ts">
  // 員工登入
  import { Card, ProviderButton, TBiteLogo } from "@tbite/ui";
  let { data } = $props();
  const returnTo = $derived(encodeURIComponent(data.returnTo));
</script>

<div class="grid min-h-screen place-items-center bg-tb-slate-50 px-4 py-12">
  <div class="w-full max-w-md fade-up">
    <div class="mb-8 flex justify-center"><TBiteLogo size={44} /></div>
    <Card>
      <div class="text-[11px] font-bold uppercase tracking-eyebrow text-tb-red-600">
        Employee · 員工登入
      </div>
      <h1 class="mt-1 text-3xl font-black tracking-tight text-tb-slate-900">歡迎回來</h1>
      <p class="mt-1 text-sm text-tb-slate-500">使用公司 SSO 帳號登入即可開始預訂午餐。</p>
      <div class="mt-6 space-y-3">
        {#each data.providers as provider}
          <ProviderButton
            provider={provider.slug}
            href={`/auth/start?provider=${provider.slug}&return_to=${returnTo}`}
          >
            使用 {provider.display_name} 登入
          </ProviderButton>
        {/each}
      </div>
    </Card>
    <p class="mt-6 text-center text-[11px] uppercase tracking-eyebrow-wide text-tb-slate-500">
      T-Bite · Corporate Catering
    </p>
  </div>
</div>
