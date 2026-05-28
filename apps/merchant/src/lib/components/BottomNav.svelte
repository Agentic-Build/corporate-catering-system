<script lang="ts">
  // Mobile bottom navigation; hidden at md+ where the top nav takes over.
  import { Icon, type IconName } from "@tbite/ui";
  import { page } from "$app/stores";

  type Tab = { href: string; label: string; icon: IconName };
  const topRow: Tab[] = [
    { href: "/", label: "儀表板", icon: "home" },
    { href: "/orders", label: "看板", icon: "doc" },
    { href: "/menus", label: "菜單", icon: "tag" },
    { href: "/complaints", label: "客訴", icon: "bell" },
  ];
  const bottomRow: Tab[] = [
    { href: "/reconciliation", label: "對帳", icon: "wallet" },
    { href: "/compliance", label: "合規", icon: "check" },
    { href: "/settings", label: "設定", icon: "cog" },
  ];

  const pathname = $derived($page.url.pathname);
  function isActive(href: string): boolean {
    return href === "/" ? pathname === "/" : pathname === href || pathname.startsWith(href + "/");
  }
</script>

<nav
  class="fixed inset-x-0 bottom-0 z-40 border-t border-tb-slate-200 bg-white/95 px-2 pt-2 backdrop-blur md:hidden"
  style="padding-bottom: max(env(safe-area-inset-bottom), 8px)"
  aria-label="主導覽"
>
  <div class="mx-auto grid max-w-md grid-cols-4 gap-1">
    {#each topRow as tab (tab.href)}
      {@const on = isActive(tab.href)}
      <a
        href={tab.href}
        aria-current={on ? "page" : undefined}
        class="flex flex-col items-center gap-0.5 rounded-lg py-1.5 {on
          ? 'bg-tb-red-50 text-tb-red-600'
          : 'text-tb-slate-500'}"
      >
        <Icon name={tab.icon} class="h-6 w-6" />
        <span class="text-[10px] font-bold leading-none">{tab.label}</span>
      </a>
    {/each}
  </div>
  <div class="mx-auto mt-1 grid max-w-md grid-cols-3 gap-1">
    {#each bottomRow as tab (tab.href)}
      {@const on = isActive(tab.href)}
      <a
        href={tab.href}
        aria-current={on ? "page" : undefined}
        class="flex flex-col items-center gap-0.5 rounded-lg py-1.5 {on
          ? 'bg-tb-red-50 text-tb-red-600'
          : 'text-tb-slate-500'}"
      >
        <Icon name={tab.icon} class="h-6 w-6" />
        <span class="text-[10px] font-bold leading-none">{tab.label}</span>
      </a>
    {/each}
  </div>
</nav>
