<script lang="ts">
  // Mobile bottom navigation; hidden at lg+ where the Sidebar takes over.
  import { Icon, type IconName } from "@tbite/ui";
  import { page } from "$app/stores";

  interface Props {
    ordersBadge?: number;
  }
  let { ordersBadge = 0 }: Props = $props();

  type Tab = { id: string; href: string; label: string; icon: IconName };
  const tabs: Tab[] = [
    { id: "home", href: "/", label: "首頁", icon: "home" },
    { id: "orders", href: "/orders", label: "訂單", icon: "doc" },
    { id: "scan", href: "/scan", label: "掃描領餐", icon: "qr" },
    { id: "payroll", href: "/payroll", label: "薪資", icon: "wallet" },
    { id: "me", href: "/me", label: "我的", icon: "user" },
  ];

  const pathname = $derived($page.url.pathname);
  function isActive(href: string): boolean {
    return href === "/" ? pathname === "/" : pathname === href || pathname.startsWith(href + "/");
  }
</script>

<nav
  class="fixed inset-x-0 bottom-0 z-40 border-t border-tb-slate-200 bg-white/95 px-2 pt-1 backdrop-blur lg:hidden"
  style="padding-bottom: max(env(safe-area-inset-bottom), 8px)"
  aria-label="主導覽"
>
  <div class="mx-auto flex max-w-md items-stretch justify-around">
    {#each tabs as tab (tab.id)}
      {@const on = isActive(tab.href)}
      <a
        href={tab.href}
        aria-current={on ? "page" : undefined}
        class="relative flex min-w-0 flex-1 flex-col items-center gap-0.5 rounded-full px-2 py-1.5 {on
          ? 'bg-tb-red-50 text-tb-red-600'
          : 'text-tb-slate-500'}"
      >
        <Icon name={tab.icon} class="h-6 w-6" />
        <span class="text-[10px] font-bold leading-none">{tab.label}</span>
        {#if tab.id === "orders" && ordersBadge > 0}
          <span
            class="absolute top-0 left-1/2 grid h-4 min-w-[16px] translate-x-1.5 place-items-center rounded-full bg-tb-rose-600 px-1 text-[10px] font-bold tabular-nums text-white"
            >{ordersBadge}</span
          >
        {/if}
      </a>
    {/each}
  </div>
</nav>
