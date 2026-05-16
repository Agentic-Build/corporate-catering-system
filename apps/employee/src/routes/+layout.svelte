<script lang="ts">
  // Employee app shell — ported from EmployeeView.jsx without the role
  // switcher: sticky header (top-0), 240px sidebar + main body, with the
  // 領餐碼 modal and cart drawer mounted globally.
  import "../app.css";
  import { TBiteLogo, LocationBar, SearchInput, Button, Icon } from "@tbite/ui";
  import { goto } from "$app/navigation";
  import { page } from "$app/stores";
  import Sidebar from "$lib/components/Sidebar.svelte";
  import CartDrawer from "$lib/components/CartDrawer.svelte";
  import FloatingCartBar from "$lib/components/FloatingCartBar.svelte";
  import TotpModal from "$lib/components/TotpModal.svelte";
  import { cart } from "$lib/cart.svelte";
  import { PLANTS, buildDays } from "$lib/plants";

  let { data, children } = $props();

  // ── plant / day — driven by URL search params, user.plant as fallback ──
  const today = new Date().toISOString().slice(0, 10);
  const selectedPlant = $derived(
    $page.url.searchParams.get("plant") ?? data.user?.plant ?? PLANTS[0].id,
  );
  const selectedDay = $derived($page.url.searchParams.get("day") ?? today);
  const days = $derived(buildDays(new Date(), selectedDay));

  function setParam(key: string, value: string) {
    const url = new URL($page.url);
    url.searchParams.set(key, value);
    goto(url.pathname + url.search);
  }

  // ── header search — only routes the home grid; navigate home then filter ──
  let search = $state("");
  function onSearch(v: string) {
    search = v;
    const url = new URL($page.url);
    url.pathname = "/";
    if (v) url.searchParams.set("q", v);
    else url.searchParams.delete("q");
    goto(url.pathname + url.search, { keepFocus: true });
  }
  // Keep the header box in sync with the active query param.
  $effect(() => {
    search = $page.url.searchParams.get("q") ?? "";
  });

  // ── global overlays ──
  let cartOpen = $state(false);
  let totpOpen = $state(false);

  // cart-bump animation on the header cart button when the count changes
  let bump = $state(false);
  let prevCount = $state(0);
  $effect(() => {
    const c = cart.count;
    if (c > prevCount) {
      bump = true;
      setTimeout(() => (bump = false), 340);
    }
    prevCount = c;
  });

  const initial = $derived((data.user?.display_name ?? "").trim().slice(0, 1) || "你");
</script>

{#if data.user}
  <div class="min-h-screen bg-white">
    <header class="sticky top-0 z-40 border-b border-tb-slate-200 bg-white/95 backdrop-blur">
      <div class="mx-auto flex max-w-[1400px] flex-wrap items-center gap-3 px-4 py-3 md:px-8">
        <TBiteLogo />
        <div class="ml-2 hidden md:block">
          <LocationBar
            plants={PLANTS}
            {selectedPlant}
            onPlantChange={(id) => setParam("plant", id)}
            {days}
            {selectedDay}
            onDayChange={(id) => setParam("day", id)}
          />
        </div>
        <div class="ml-auto hidden max-w-md flex-1 md:block">
          <SearchInput value={search} onInput={onSearch} placeholder="搜尋餐廳或餐點…" />
        </div>
        <div class="ml-auto flex items-center gap-2 md:ml-0">
          <Button variant="secondary" size="sm" onclick={() => (totpOpen = true)}>
            <Icon name="qr" class="h-4 w-4 text-tb-red-600" />
            <span class="hidden sm:inline">領餐碼</span>
          </Button>
          <button
            type="button"
            onclick={() => (cartOpen = true)}
            class="relative grid h-10 w-10 place-items-center rounded-full bg-tb-slate-100 text-tb-slate-800 transition hover:bg-tb-slate-200 {bump
              ? 'cart-bump'
              : ''}"
            aria-label="購物車"
          >
            <Icon name="cart" class="h-5 w-5" />
            {#if cart.count > 0}
              <span
                class="absolute -right-1 -top-1 grid h-5 min-w-[20px] place-items-center rounded-full bg-tb-red-600 px-1 text-[10px] font-extrabold tabular-nums text-white ring-2 ring-white"
                >{cart.count}</span
              >
            {/if}
          </button>
          <form method="POST" action="/auth/logout">
            <button
              type="submit"
              class="ml-1 grid h-10 w-10 place-items-center rounded-full bg-gradient-to-br from-tb-red-500 to-tb-rose-700 text-sm font-bold text-white shadow-tb-sm"
              title="{data.user.display_name} · 登出"
              aria-label="登出"
            >
              {initial}
            </button>
          </form>
        </div>
      </div>
      <div class="border-t border-tb-slate-100 md:hidden">
        <div class="mx-auto max-w-[1400px] px-4 py-2">
          <LocationBar
            plants={PLANTS}
            {selectedPlant}
            onPlantChange={(id) => setParam("plant", id)}
            {days}
            {selectedDay}
            onDayChange={(id) => setParam("day", id)}
          />
        </div>
      </div>
    </header>

    <div class="mx-auto flex max-w-[1400px] gap-6 px-4 py-6 md:px-8">
      <Sidebar onOpenTotp={() => (totpOpen = true)} ordersBadge={data.activeOrders} />
      <main class="min-w-0 flex-1 pb-32">{@render children()}</main>
    </div>
  </div>

  <FloatingCartBar onOpen={() => (cartOpen = true)} />
  <CartDrawer
    open={cartOpen}
    onClose={() => (cartOpen = false)}
    plant={selectedPlant}
    supplyDate={selectedDay}
  />
  <TotpModal open={totpOpen} onClose={() => (totpOpen = false)} orders={data.readyOrders} />
{:else}
  <main class="min-h-screen bg-white">{@render children()}</main>
{/if}
