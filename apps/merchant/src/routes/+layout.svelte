<script lang="ts">
  import "../app.css";
  import { TBiteLogo } from "@tbite/ui";
  let { data, children } = $props();
</script>

<header
  class="sticky top-0 z-40 flex items-center justify-between bg-white/95 px-6 py-4 shadow-tb-sm backdrop-blur"
>
  <TBiteLogo />
  {#if data.user}
    <nav class="flex items-center gap-3 text-sm">
      <a href="/orders" class="text-tb-slate-700 hover:text-tb-slate-950">備餐看板</a>
      <a href="/menus" class="text-tb-slate-700 hover:text-tb-slate-950">菜單管理</a>
      <a href="/supply" class="text-tb-slate-700 hover:text-tb-slate-950">份數設定</a>
    </nav>
    <div class="flex items-center gap-3 text-sm">
      <span class="text-tb-slate-500">{data.user.display_name}</span>
      <form method="POST" action="/auth/logout">
        <button class="text-tb-red-600 hover:text-tb-red-700" type="submit">登出</button>
      </form>
    </div>
  {:else}
    <a href="/login" class="text-sm font-semibold text-tb-red-600 hover:text-tb-red-700">登入</a>
  {/if}
</header>
<main class="mx-auto max-w-[1400px] px-4 py-8 md:px-8">{@render children()}</main>
