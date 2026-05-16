<script lang="ts">
  // Left navigation rail — ported from EmployeeView.jsx TbSidebar, with the
  // reference's no-backend items (薪資代扣/通知中心/設定) dropped. Items map
  // 1:1 to real routes; 領餐碼 opens the global TOTP modal instead of routing.
  import { Icon, type IconName } from "@tbite/ui";
  import { page } from "$app/stores";

  interface Props {
    onOpenTotp: () => void;
    ordersBadge?: number;
  }
  let { onOpenTotp, ordersBadge = 0 }: Props = $props();

  type NavItem = { id: string; label: string; icon: IconName; href?: string };
  const nav: NavItem[] = [
    { id: "home", label: "今日首頁", icon: "home", href: "/" },
    { id: "orders", label: "我的訂單", icon: "doc", href: "/orders" },
    { id: "totp", label: "領餐碼", icon: "qr" },
    { id: "favorites", label: "我的常點", icon: "heart", href: "/menu/favorites" },
    { id: "disputes", label: "申訴", icon: "alert", href: "/disputes" },
  ];

  const pathname = $derived($page.url.pathname);
  function isActive(href: string): boolean {
    if (href === "/") return pathname === "/";
    return pathname === href || pathname.startsWith(href + "/");
  }
</script>

<aside class="hidden lg:block lg:w-60 lg:flex-shrink-0">
  <div class="sticky top-[100px] grid gap-1 pr-2">
    {#each nav as n (n.id)}
      {@const on = n.href ? isActive(n.href) : false}
      {#if n.href}
        <a
          href={n.href}
          class="group flex items-center gap-3 rounded-tb-xl px-3.5 py-2.5 text-left text-sm font-semibold transition
            {on ? 'bg-tb-red-50 text-tb-red-700' : 'text-tb-slate-700 hover:bg-tb-slate-100'}"
        >
          <Icon
            name={n.icon}
            class="h-5 w-5 {on
              ? 'text-tb-red-600'
              : 'text-tb-slate-500 group-hover:text-tb-slate-900'}"
          />
          <span class="flex-1">{n.label}</span>
          {#if n.id === "orders" && ordersBadge > 0}
            <span
              class="grid h-5 min-w-[20px] place-items-center rounded-full bg-tb-rose-600 px-1.5 text-[10px] font-bold tabular-nums text-white"
              >{ordersBadge}</span
            >
          {/if}
        </a>
      {:else}
        <button
          type="button"
          onclick={onOpenTotp}
          class="group flex items-center gap-3 rounded-tb-xl px-3.5 py-2.5 text-left text-sm font-semibold text-tb-slate-700 transition hover:bg-tb-slate-100"
        >
          <Icon name={n.icon} class="h-5 w-5 text-tb-slate-500 group-hover:text-tb-slate-900" />
          <span class="flex-1">{n.label}</span>
        </button>
      {/if}
    {/each}

    <div class="my-2 h-px bg-tb-slate-100"></div>

    <div
      class="rounded-tb-2xl border border-tb-amber-200 bg-gradient-to-br from-tb-amber-50 to-tb-rose-50 p-4"
    >
      <div class="text-[10px] font-bold uppercase tracking-eyebrow text-tb-amber-800">Pro Tip</div>
      <div class="mt-1 text-sm font-bold leading-snug text-tb-slate-900">
        前一日 17:00 前還可改單
      </div>
      <div class="mt-1 text-[11px] leading-relaxed text-tb-slate-600">
        預訂後仍可至「我的訂單」修改份數、取餐點或取消。
      </div>
    </div>
  </div>
</aside>
