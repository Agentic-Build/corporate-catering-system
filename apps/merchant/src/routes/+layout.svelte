<script lang="ts">
  import "../app.css";
  import { page } from "$app/stores";
  import { TBiteLogo, Button, Icon, type IconName } from "@tbite/ui";
  import BottomNav from "$lib/components/BottomNav.svelte";
  let { data, children } = $props();

  // No vendor-name field on the merchant `Me` payload — the operator's
  // display name is the closest real data, and stands in for the shop.
  const vendorName = $derived(data.user?.display_name ?? "");
  const avatarText = $derived(vendorName ? vendorName.slice(0, 2) : "");

  const navItems: { href: string; label: string; icon: IconName }[] = [
    { href: "/", label: "備餐儀表板", icon: "home" },
    { href: "/orders", label: "備餐看板", icon: "doc" },
    { href: "/labels", label: "餐點貼紙", icon: "qr" },
    { href: "/menus", label: "菜單管理", icon: "tag" },
    { href: "/complaints", label: "客訴收件匣", icon: "bell" },
    { href: "/reconciliation", label: "對帳", icon: "wallet" },
    { href: "/compliance", label: "合規自查", icon: "check" },
    { href: "/settings", label: "營運設定", icon: "cog" },
  ];

  /** A nav item is active when the path matches it or sits beneath it. */
  function isActive(href: string, path: string): boolean {
    if (href === "/") return path === "/";
    return path === href || path.startsWith(href + "/");
  }
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
    {#if data.user}
      <nav class="mx-auto hidden max-w-[1400px] px-4 md:block md:px-8">
        <div class="flex gap-1 overflow-x-auto">
          {#each navItems as item (item.href)}
            {@const on = isActive(item.href, $page.url.pathname)}
            <a
              href={item.href}
              class="flex items-center gap-1.5 whitespace-nowrap border-b-2 px-3 py-2.5 text-sm font-semibold transition
                {on
                ? 'border-tb-red-600 text-tb-red-700'
                : 'border-transparent text-tb-slate-500 hover:text-tb-slate-900'}"
            >
              <Icon name={item.icon} class="h-4 w-4" />{item.label}
            </a>
          {/each}
        </div>
      </nav>
    {/if}
  </header>

  <main class="mx-auto max-w-[1400px] px-4 pb-24 pt-6 md:px-8 md:py-6">{@render children()}</main>
  {#if data.user}
    <BottomNav />
  {/if}
</div>
