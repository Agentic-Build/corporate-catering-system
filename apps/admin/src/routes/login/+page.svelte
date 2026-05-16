<script lang="ts">
  import { Card, ProviderButton, TBiteLogo } from "@tbite/ui";
  let { data } = $props();
  const r = $derived(encodeURIComponent(data.returnTo));
</script>

<div class="mx-auto mt-12 max-w-md">
  <div class="mb-8 flex justify-center"><TBiteLogo size={44} /></div>
  <Card>
    <div class="text-[11px] font-bold uppercase tracking-eyebrow text-tb-red-600">
      合規 · 治理 · 對帳
    </div>
    <h1 class="mt-1 text-2xl font-black tracking-tight text-tb-slate-900">福委會後台登入</h1>
    <p class="mt-1 mb-6 text-sm text-tb-slate-500">僅限 Authentik 管理角色 · 以企業帳號繼續</p>
    <div class="space-y-3">
      {#each data.providers as provider}
        <ProviderButton
          provider={provider.slug}
          href={`/auth/start?provider=${provider.slug}&return_to=${r}`}
        >
          使用 {provider.display_name} 登入
        </ProviderButton>
      {/each}
    </div>
  </Card>
  <p class="mt-6 text-center text-[11px] uppercase tracking-eyebrow-wide text-tb-slate-500">
    T-Bite · Welfare Committee
  </p>
</div>
