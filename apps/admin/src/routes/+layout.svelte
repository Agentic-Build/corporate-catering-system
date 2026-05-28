<script lang="ts">
  import "../app.css";
  import { page } from "$app/stores";
  import { TBiteLogo, Button, Icon, type IconName } from "@tbite/ui";
  import BottomNav from "$lib/components/BottomNav.svelte";
  let { data, children } = $props();

  const navItems: { href: string; label: string; icon: IconName }[] = [
    { href: "/", label: "治理總覽", icon: "home" },
    { href: "/vendors", label: "商家管理", icon: "doc" },
    { href: "/plants", label: "廠區登錄", icon: "tag" },
    { href: "/payroll", label: "月結", icon: "wallet" },
    { href: "/vendor-settlements", label: "商家結算", icon: "card" },
    { href: "/complaints", label: "升級客訴", icon: "bell" },
    { href: "/disputes", label: "申訴", icon: "alert" },
    { href: "/anomalies", label: "告警", icon: "alert" },
    { href: "/audit", label: "稽核紀錄", icon: "download" },
    { href: "/dlq", label: "死信佇列", icon: "alert" },
  ];

  function isActive(href: string, path: string): boolean {
    if (href === "/") return path === "/";
    return path === href || path.startsWith(href + "/");
  }
</script>

<div class="fade-up min-h-screen bg-tb-slate-50">
  <header class="sticky top-0 z-30 border-b border-tb-slate-200 bg-white/95 backdrop-blur">
    <div class="mx-auto flex max-w-[1400px] flex-wrap items-center gap-3 px-4 py-3 md:px-8">
      <TBiteLogo />
      <span class="ml-2 rounded-full bg-tb-slate-100 px-3 py-1 text-xs font-bold text-tb-slate-700"
        >福委會後台 · 管理員</span
      >
      {#if data.user}
        <div class="ml-auto hidden items-center gap-2 md:flex">
          <a href="/audit">
            <Button variant="secondary" size="sm">
              <Icon name="download" class="h-4 w-4" />稽核紀錄
            </Button>
          </a>
          <a href="/vendors">
            <Button variant="primary" size="sm">
              <Icon name="plus" class="h-3.5 w-3.5" />新增邀請
            </Button>
          </a>
          <form method="POST" action="/auth/logout">
            <Button variant="ghost" size="sm" type="submit">登出</Button>
          </form>
          <div
            class="ml-1 grid h-9 w-9 place-items-center rounded-full bg-tb-slate-800 text-xs font-bold text-white shadow-tb-sm"
            title={data.user.display_name}
          >
            福
          </div>
        </div>
        <form method="POST" action="/auth/logout" class="ml-auto md:hidden">
          <Button variant="ghost" size="sm" type="submit">登出</Button>
        </form>
      {:else}
        <a href="/login" class="ml-auto text-sm font-semibold text-tb-red-600 hover:text-tb-red-700"
          >登入</a
        >
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

  <main class="mx-auto grid max-w-[1400px] gap-6 px-4 pb-24 pt-6 md:px-8 md:py-6">
    {@render children()}
  </main>

  {#if data.user}
    <BottomNav />
  {/if}
</div>
