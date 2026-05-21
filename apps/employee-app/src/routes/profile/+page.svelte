<script lang="ts">
  // ProfileScreen — user header, menu rows, logout.
  import { goto } from "$app/navigation";
  import { logout } from "$lib/auth";
  import { PLANTS } from "$lib/sample";
  import { session } from "$lib/session.svelte";

  const name = $derived(session.user?.display_name ?? "員工");
  const plantLabel = $derived(
    PLANTS.find((p) => p.id === (session.user?.plant ?? ""))?.label ?? "—",
  );

  const ROWS: { label: string; glyph: string; cls: string; href?: string }[] = [
    { label: "我的常點", glyph: "♥", cls: "text-tb-rose-600 bg-tb-rose-50", href: "/favorites" },
    { label: "評分與客訴", glyph: "★", cls: "text-tb-amber-600 bg-tb-amber-50", href: "/payroll" },
    { label: "訂單紀錄", glyph: "📋", cls: "text-tb-sky-600 bg-tb-sky-50", href: "/orders" },
    { label: "聯絡福委會", glyph: "💬", cls: "text-tb-emerald-600 bg-tb-emerald-50" },
  ];

  async function onLogout() {
    await logout();
    goto("/login");
  }
</script>

<div class="flex h-full flex-col bg-tb-slate-50">
  <div class="flex-shrink-0 bg-white px-4 pb-5" style="padding-top: max(env(safe-area-inset-top), 1rem)">
    <div class="flex items-center gap-4">
      <div
        class="grid h-16 w-16 place-items-center rounded-full bg-gradient-to-br from-tb-red-500 to-tb-rose-700 text-2xl font-black text-white shadow-lg"
      >
        {name.slice(0, 1)}
      </div>
      <div>
        <div class="text-lg font-black text-tb-slate-900">{name}</div>
        <div class="text-xs text-tb-slate-500">{plantLabel} 廠區</div>
        <div
          class="mt-1 inline-flex items-center gap-1 rounded-full bg-tb-emerald-50 px-2.5 py-0.5 text-[11px] font-bold text-tb-emerald-700"
        >
          <span class="h-1.5 w-1.5 rounded-full bg-tb-emerald-500"></span> 帳號正常
        </div>
      </div>
    </div>
  </div>

  <div class="no-scroll flex-1 overflow-y-auto px-4 py-3">
    <div
      class="divide-y divide-tb-slate-100 overflow-hidden rounded-3xl bg-white shadow-sm ring-1 ring-tb-slate-200/70"
    >
      {#each ROWS as row (row.label)}
        <button
          type="button"
          onclick={() => row.href && goto(row.href)}
          class="flex w-full items-center gap-3 px-4 py-3.5 transition active:bg-tb-slate-100"
        >
          <span class="grid h-9 w-9 place-items-center rounded-xl text-base {row.cls}">
            {row.glyph}
          </span>
          <span class="flex-1 text-left text-sm font-semibold text-tb-slate-800">{row.label}</span>
          <span class="text-lg text-tb-slate-300">›</span>
        </button>
      {/each}
    </div>
    <button
      type="button"
      onclick={onLogout}
      class="mt-4 w-full rounded-2xl border border-tb-rose-200 bg-tb-rose-50 py-3.5 text-sm font-bold text-tb-rose-700"
    >
      登出
    </button>
  </div>
</div>
