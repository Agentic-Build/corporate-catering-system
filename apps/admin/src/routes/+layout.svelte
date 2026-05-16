<script lang="ts">
  // Ported from ui_kits/tbite/AdminView.jsx header shell — no role switcher,
  // so the sticky header sits at top-0 (not top-[52px]).
  import "../app.css";
  import { TBiteLogo, Button, Icon } from "@tbite/ui";
  let { data, children } = $props();
</script>

<div class="fade-up min-h-screen bg-tb-slate-50">
  <header class="sticky top-0 z-30 border-b border-tb-slate-200 bg-white/95 backdrop-blur">
    <div class="mx-auto flex max-w-[1400px] flex-wrap items-center gap-3 px-4 py-3 md:px-8">
      <TBiteLogo />
      <span class="ml-2 rounded-full bg-tb-slate-100 px-3 py-1 text-xs font-bold text-tb-slate-700"
        >福委會後台 · 管理員</span
      >
      {#if data.user}
        <div class="ml-auto flex items-center gap-2">
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
      {:else}
        <a href="/login" class="ml-auto text-sm font-semibold text-tb-red-600 hover:text-tb-red-700"
          >登入</a
        >
      {/if}
    </div>
  </header>

  <main class="mx-auto grid max-w-[1400px] gap-6 px-4 py-6 md:px-8">
    {@render children()}
  </main>
</div>
