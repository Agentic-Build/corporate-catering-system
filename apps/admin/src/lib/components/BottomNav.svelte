<script lang="ts">
  // Mobile bottom navigation; hidden at md+ where the top tab bar takes over.
  import { Icon, type IconName } from "@tbite/ui";
  import { page } from "$app/stores";

  type Tab = { href: string; label: string; icon: IconName };
  const topRow: Tab[] = [
    { href: "/", label: "治理總覽", icon: "home" },
    { href: "/vendors", label: "商家", icon: "doc" },
    { href: "/payroll", label: "薪資", icon: "wallet" },
    { href: "/vendor-settlements", label: "結算", icon: "card" },
  ];
  const bottomRow: Tab[] = [
    { href: "/complaints", label: "客訴", icon: "bell" },
    { href: "/disputes", label: "申訴", icon: "alert" },
    { href: "/anomalies", label: "告警", icon: "alert" },
    { href: "/audit", label: "稽核", icon: "download" },
  ];

  const pathname = $derived($page.url.pathname);
  function isActive(href: string): boolean {
    return href === "/" ? pathname === "/" : pathname === href || pathname.startsWith(href + "/");
  }
</script>

<nav
  class="fixed inset-x-0 bottom-0 z-40 border-t border-tb-slate-200 bg-white/95 px-2 pt-1 backdrop-blur md:hidden"
  style="padding-bottom: max(env(safe-area-inset-bottom), 8px)"
  aria-label="主導覽"
>
  <div class="mx-auto grid max-w-md grid-cols-4 gap-1">
    {#each topRow as tab (tab.href)}
      {@const on = isActive(tab.href)}
      <a
        href={tab.href}
        aria-current={on ? "page" : undefined}
        class="flex flex-col items-center gap-0.5 rounded-lg px-2 py-1.5 transition {on
          ? 'bg-tb-red-50 text-tb-red-600'
          : 'text-tb-slate-500'}"
      >
        <Icon name={tab.icon} class="h-5 w-5" />
        <span class="text-[10px] font-bold leading-none">{tab.label}</span>
      </a>
    {/each}
  </div>
  <div class="mx-auto grid max-w-md grid-cols-4 gap-1 pt-1">
    {#each bottomRow as tab (tab.href)}
      {@const on = isActive(tab.href)}
      <a
        href={tab.href}
        aria-current={on ? "page" : undefined}
        class="flex flex-col items-center gap-0.5 rounded-lg px-2 py-1.5 transition {on
          ? 'bg-tb-red-50 text-tb-red-600'
          : 'text-tb-slate-500'}"
      >
        <Icon name={tab.icon} class="h-5 w-5" />
        <span class="text-[10px] font-bold leading-none">{tab.label}</span>
      </a>
    {/each}
  </div>
</nav>
