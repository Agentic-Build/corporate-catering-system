<script lang="ts">
  // Fixed 5-tab bottom navigation. Sits above the home-indicator safe area.
  import { page } from "$app/stores";
  import AppIcon, { type AppIconName } from "./AppIcon.svelte";

  const TABS: { href: string; label: string; icon: AppIconName }[] = [
    { href: "/", label: "首頁", icon: "home" },
    { href: "/orders", label: "訂單", icon: "orders" },
    { href: "/scan", label: "掃描領餐", icon: "qr" },
    { href: "/payroll", label: "薪資", icon: "wallet" },
    { href: "/profile", label: "個人", icon: "profile" },
  ];

  function isActive(href: string, path: string): boolean {
    return href === "/" ? path === "/" : path.startsWith(href);
  }
</script>

<nav
  class="flex-shrink-0 border-t border-tb-slate-200 bg-white px-2 pt-1"
  style="padding-bottom: max(env(safe-area-inset-bottom), 8px)"
>
  <div class="flex items-center justify-around">
    {#each TABS as tab (tab.href)}
      {@const on = isActive(tab.href, $page.url.pathname)}
      <a
        href={tab.href}
        class="flex min-w-0 flex-col items-center gap-0.5 px-3 py-1.5 {on
          ? 'text-tb-red-600'
          : 'text-tb-slate-400'}"
      >
        <AppIcon name={tab.icon} class="h-6 w-6" />
        <span class="text-[10px] font-bold">{tab.label}</span>
      </a>
    {/each}
  </div>
</nav>
