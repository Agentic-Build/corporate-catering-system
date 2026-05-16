<script lang="ts">
  import "../app.css";
  import { TBiteLogo, Button, Icon } from "@tbite/ui";
  let { data, children } = $props();

  // No vendor-name field on the merchant `Me` payload — the operator's
  // display name is the closest real data, and stands in for the shop.
  const vendorName = $derived(data.user?.display_name ?? "");
  const avatarText = $derived(vendorName ? vendorName.slice(0, 2) : "");
</script>

<div class="min-h-screen bg-tb-slate-50">
  <header class="sticky top-0 z-30 border-b border-tb-slate-200 bg-white">
    <div class="mx-auto flex max-w-[1400px] flex-wrap items-center gap-3 px-4 py-3 md:px-8">
      <TBiteLogo />
      {#if data.user}
        <span
          class="ml-2 rounded-full bg-tb-slate-100 px-3 py-1 text-xs font-bold text-tb-slate-700"
        >
          商家後台 · {vendorName}
        </span>
        <div class="ml-auto flex items-center gap-2">
          <a href="/orders">
            <Button variant="secondary" size="sm">
              <Icon name="bell" class="h-4 w-4" />通知
            </Button>
          </a>
          <a href="/menus">
            <Button variant="secondary" size="sm">
              <Icon name="cog" class="h-4 w-4" />設定
            </Button>
          </a>
          <form method="POST" action="/auth/logout">
            <Button variant="ghost" size="sm" type="submit">登出</Button>
          </form>
          <div
            class="ml-1 grid h-9 w-9 place-items-center rounded-full bg-gradient-to-br from-tb-amber-400 to-tb-red-600 text-xs font-bold text-white shadow-tb-sm"
          >
            {avatarText}
          </div>
        </div>
      {:else}
        <a
          href="/login"
          class="ml-auto text-sm font-semibold text-tb-red-600 hover:text-tb-red-700"
        >
          登入
        </a>
      {/if}
    </div>
  </header>

  <main class="mx-auto max-w-[1400px] px-4 py-6 md:px-8">{@render children()}</main>
</div>
