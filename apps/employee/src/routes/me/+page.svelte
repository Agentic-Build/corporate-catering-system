<script lang="ts">
  // 我的: profile + quick links (mobile bottom-nav Profile tab).
  import { Icon, type IconName } from "@tbite/ui";

  let { data } = $props();
  const user = $derived(data.user);
  const initial = $derived((user?.display_name ?? "").trim().slice(0, 1) || "你");

  const links: { href: string; label: string; desc: string; icon: IconName }[] = [
    { href: "/menu/favorites", label: "我的常點", desc: "你收藏的菜色", icon: "heart" },
    { href: "/complaints", label: "我的客訴", desc: "餐點問題回報與處理進度", icon: "bell" },
    { href: "/disputes", label: "申訴", desc: "薪資扣款金額有疑問時提出", icon: "alert" },
  ];
</script>

<div class="fade-up mx-auto max-w-xl">
  <section
    class="mb-6 flex items-center gap-4 rounded-tb-2xl border border-tb-slate-200 bg-white p-5 shadow-tb-sm"
  >
    <div
      class="grid h-14 w-14 flex-shrink-0 place-items-center rounded-full bg-gradient-to-br from-tb-red-500 to-tb-rose-700 text-xl font-bold text-white"
    >
      {initial}
    </div>
    <div class="min-w-0">
      <h1 class="truncate text-lg font-black text-tb-slate-900">{user?.display_name ?? "你"}</h1>
      {#if user?.plant}
        <p class="text-sm text-tb-slate-500">領餐廠區 · {user.plant}</p>
      {/if}
    </div>
  </section>

  <section class="mb-6 grid gap-2">
    {#each links as l (l.href)}
      <a
        href={l.href}
        class="flex items-center gap-3 rounded-tb-2xl border border-tb-slate-200 bg-white p-4 shadow-tb-sm transition hover:shadow-tb-md"
      >
        <span
          class="grid h-10 w-10 flex-shrink-0 place-items-center rounded-full bg-tb-slate-100 text-tb-slate-700"
        >
          <Icon name={l.icon} class="h-5 w-5" />
        </span>
        <span class="min-w-0 flex-1">
          <span class="block text-sm font-bold text-tb-slate-900">{l.label}</span>
          <span class="block text-xs text-tb-slate-500">{l.desc}</span>
        </span>
        <Icon name="chevron" class="h-5 w-5 -rotate-90 text-tb-slate-400" />
      </a>
    {/each}
  </section>

  <form method="POST" action="/auth/logout">
    <button
      type="submit"
      class="w-full rounded-tb-2xl border border-tb-rose-200 bg-tb-rose-50 px-4 py-3 text-sm font-bold text-tb-rose-700 transition hover:bg-tb-rose-100"
    >
      登出
    </button>
  </form>
</div>
