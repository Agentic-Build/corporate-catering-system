<script lang="ts">
  // Employee home: greeting, category strip, featured rows, full menu grid.
  import { MealCard, StateTag, WeekCalendar } from "@tbite/ui";
  import { invalidate, invalidateAll, goto } from "$app/navigation";
  import { page } from "$app/stores";
  import { onMount } from "svelte";
  import FeaturedRow from "$lib/components/FeaturedRow.svelte";
  import MenuFilterBar from "$lib/components/MenuFilterBar.svelte";
  import MenuViewToggle from "$lib/components/MenuViewToggle.svelte";
  import { cart } from "$lib/cart.svelte";

  let { data, form } = $props();

  const selectedDay = $derived($page.url.searchParams.get("day") ?? data.home?.target_day ?? "");
  const weekDays = $derived.by(() => {
    const wk = ["日", "一", "二", "三", "四", "五", "六"];
    const today = new Date();
    return Array.from({ length: 7 }, (_, i) => {
      const d = new Date(today);
      d.setDate(today.getDate() + i);
      const id = `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(
        d.getDate(),
      ).padStart(2, "0")}`;
      return { id, weekday: wk[d.getDay()] ?? "", dom: String(d.getDate()), isToday: i === 0 };
    });
  });
  function pickDay(id: string) {
    const u = new URL($page.url);
    u.searchParams.set("day", id);
    goto(u.pathname + u.search, { keepFocus: true, noScroll: true });
  }

  // SSE: refetch only the menu/home fragment (app:home) on stock changes.
  onMount(() => {
    const es = new EventSource("/menu/events");
    es.onmessage = (e) => {
      let kind = "";
      try {
        kind = (JSON.parse(e.data)?.kind as string) ?? "";
      } catch {
        // unparseable payload still signals activity
      }
      if (kind && kind !== "ping") invalidate("app:home");
    };
    return () => es.close();
  });

  type ReorderC = {
    source_order_id: string;
    vendor_name: string;
    total_price_minor: number;
    freq: number;
    items_preview: string[] | null;
    available_today: boolean;
    vendor_id: string;
  };
  type FavoriteC = {
    menu_item_id: string;
    name: string;
    unit_price: number;
    vendor_id: string;
    available_today: boolean;
  };
  type RecommendC = {
    menu_item_id: string;
    name: string;
    unit_price: number;
    vendor_id: string;
    reason: string;
    score: number;
  };
  type DayMenuItem = {
    id: string;
    name: string;
    vendor: string;
    vendor_id: string;
    price_minor: number;
    remain: number;
    capacity: number;
    pickup_window: string;
    tags: string[] | null;
    images?: string[] | null;
    sold_out: boolean;
  };
  type OrderSummary = {
    order_id: string;
    vendor_id: string;
    status: string;
    cutoff_at: string;
    total_price_minor: number;
  };

  const reorderChips = $derived((data.home.reorder_chips as ReorderC[]) ?? []);
  const favoriteChips = $derived((data.home.favorite_chips as FavoriteC[]) ?? []);
  const recommendChips = $derived((data.home.recommend_chips as RecommendC[]) ?? []);
  const dayMenu = $derived((data.home.day_menu as DayMenuItem[]) ?? []);
  const hasOrdered = $derived(data.home.has_ordered);
  const orderSummary = $derived(data.home.order_summary as OrderSummary | undefined);

  const eyebrow = $derived(
    (() => {
      const [y = 0, m = 1, d = 1] = data.home.target_day.split("-").map(Number);
      const wk = ["日", "一", "二", "三", "四", "五", "六"];
      const date = new Date(y, m - 1, d);
      return `${y} / ${String(m).padStart(2, "0")} / ${String(d).padStart(2, "0")} · 週${wk[date.getDay()]}`;
    })(),
  );
  // Cutoff is only known when the user already has an order today.
  const cutoffText = $derived(
    (() => {
      if (!orderSummary?.cutoff_at) return null;
      const diff = new Date(orderSummary.cutoff_at).getTime() - Date.now();
      if (diff <= 0) return null;
      const h = Math.floor(diff / 3_600_000);
      const min = Math.floor((diff % 3_600_000) / 60_000);
      return h > 0 ? `${h} 小時 ${min} 分` : `${min} 分`;
    })(),
  );

  const query = $derived($page.url.searchParams.get("q")?.trim() ?? "");

  // F3: when filter bar params are present, server returns filtered/sorted grid;
  // otherwise client only narrows day_menu by the header search box.
  const serverFiltered = $derived(Boolean(data.filterActive));
  const filteredMenu = $derived(
    serverFiltered
      ? ((data.filteredMenu as DayMenuItem[]) ?? [])
      : dayMenu.filter((m) => {
          if (query && !m.name.includes(query) && !m.vendor.includes(query)) return false;
          return true;
        }),
  );

  // A4: 全部餐點 view toggle (meal/vendor), persisted in ?view= + localStorage.
  let menuView = $state<"meal" | "vendor">("meal");
  onMount(() => {
    const fromUrl = $page.url.searchParams.get("view");
    const stored = localStorage.getItem("tb:menuView");
    const initial = fromUrl ?? stored;
    if (initial === "vendor" || initial === "meal") menuView = initial;
  });
  function setMenuView(next: "meal" | "vendor") {
    menuView = next;
    localStorage.setItem("tb:menuView", next);
    const u = new URL($page.url);
    u.searchParams.set("view", next);
    goto(u.pathname + u.search, { keepFocus: true, noScroll: true, replaceState: true });
  }
  const vendorGroups = $derived.by(() => {
    const groups = new Map<string, { vendorId: string; vendor: string; items: DayMenuItem[] }>();
    for (const m of filteredMenu) {
      let g = groups.get(m.vendor_id);
      if (!g) {
        g = { vendorId: m.vendor_id, vendor: m.vendor, items: [] };
        groups.set(m.vendor_id, g);
      }
      g.items.push(m);
    }
    return [...groups.values()];
  });

  // Enrich chips with day_menu data when on sale; else render as unavailable.
  type RecCard = { key: string } & RecommendC & { menu?: DayMenuItem };
  type FavCard = { key: string } & FavoriteC & { menu?: DayMenuItem };
  const recommendCards = $derived(
    recommendChips.map<RecCard>((c) => ({
      key: c.menu_item_id,
      ...c,
      menu: dayMenu.find((m) => m.id === c.menu_item_id),
    })),
  );
  const favoriteCards = $derived(
    favoriteChips.map<FavCard>((c) => ({
      key: c.menu_item_id,
      ...c,
      menu: dayMenu.find((m) => m.id === c.menu_item_id),
    })),
  );
  type ReorderCard = { key: string } & ReorderC;
  const reorderCards = $derived(
    reorderChips.map<ReorderCard>((c) => ({ key: c.source_order_id, ...c })),
  );

  let favoritesSet = $state<Set<string>>(new Set());
  $effect(() => {
    favoritesSet = new Set(data.favoriteIds as string[]);
  });

  let toast = $state<{ tone: "info" | "warning" | "danger"; text: string } | null>(null);
  let toastTimer: ReturnType<typeof setTimeout> | null = null;
  function showToast(tone: "info" | "warning" | "danger", text: string) {
    toast = { tone, text };
    if (toastTimer) clearTimeout(toastTimer);
    toastTimer = setTimeout(() => {
      toast = null;
      toastTimer = null;
    }, 4500);
  }
  $effect(() => () => {
    if (toastTimer) clearTimeout(toastTimer);
  });

  // Surface reorder partial-mode flash via query-params (do NOT clear `q`).
  $effect(() => {
    const params = $page.url.searchParams;
    if (params.get("reorder") === "partial") {
      const names = params.get("unavailable") ?? "";
      const cnt = params.get("unavailable_count") ?? "?";
      showToast(
        "warning",
        names ? `${cnt} 項今日無供應：${names}，其餘已加入訂單` : `${cnt} 項今日無供應`,
      );
      const url = new URL(window.location.href);
      for (const k of ["reorder", "unavailable", "unavailable_count", "order_id"]) {
        url.searchParams.delete(k);
      }
      history.replaceState(history.state, "", url.pathname + url.search);
    }
  });

  $effect(() => {
    const f = form as { reorderToast?: string } | null | undefined;
    if (f?.reorderToast) showToast("danger", f.reorderToast);
  });

  function addMenuItem(m: DayMenuItem) {
    cart.add(m.id, {
      name: m.name,
      vendor: m.vendor,
      price: m.price_minor,
      image: (m.images ?? [])[0],
    });
    showToast("info", `已加入：${m.name}`);
  }

  function toggleFavorite(menuItemId: string) {
    const wasFav = favoritesSet.has(menuItemId);
    const next = new Set(favoritesSet);
    if (wasFav) next.delete(menuItemId);
    else next.add(menuItemId);
    favoritesSet = next;

    const fd = new FormData();
    fd.set("menu_item_id", menuItemId);
    fetch(wasFav ? "?/removeFavorite" : "?/addFavorite", { method: "POST", body: fd })
      .then((r) => {
        if (!r.ok) throw new Error("favorite toggle failed");
        return invalidateAll();
      })
      .catch(() => {
        const reverted = new Set(favoritesSet);
        if (wasFav) reverted.add(menuItemId);
        else reverted.delete(menuItemId);
        favoritesSet = reverted;
        showToast("danger", wasFav ? "取消最愛失敗" : "加入最愛失敗");
      });
  }

  const orderStatusLabel: Record<string, string> = {
    draft: "草稿",
    placed: "已預訂",
    cutoff: "已截單",
    cancelled: "已取消",
    ready: "備餐完成",
    picked_up: "已領取",
    no_show: "未領取",
    refunded: "已退款",
  };
  const orderStatusTone: Record<string, "info" | "neutral" | "warning" | "danger" | "success"> = {
    draft: "neutral",
    placed: "info",
    cutoff: "warning",
    cancelled: "neutral",
    ready: "success",
    picked_up: "success",
    no_show: "danger",
    refunded: "warning",
  };

  const plantLabel = $derived(
    (data.plants as { id: string; label: string }[]).find((p) => p.id === data.selectedPlant)
      ?.label ?? data.selectedPlant,
  );
</script>

<div class="fade-up">
  <!-- Greeting -->
  <section class="mb-5">
    <div class="text-[11px] font-bold uppercase tracking-eyebrow text-tb-red-600">{eyebrow}</div>
    <h1 class="mt-1 text-3xl font-black tracking-tight text-tb-slate-900">
      哈囉，{data.user.display_name} 👋
    </h1>
    <p class="mt-1 text-sm text-tb-slate-500">
      {#if cutoffText}
        距離今日截單還有 <span class="font-bold text-tb-amber-700">{cutoffText}</span> · 可預訂未來 7
        天
      {:else if hasOrdered}
        今天的午餐已就緒 · 可預訂未來 7 天
      {:else}
        挑選你今天想預訂的餐點 · 可預訂未來 7 天
      {/if}
    </p>
  </section>

  <!-- Week-view date picker -->
  <section class="mb-5">
    <div class="mb-2 text-[11px] font-bold uppercase tracking-eyebrow text-tb-slate-500">
      選擇取餐日 · 可預訂未來 7 天
    </div>
    <WeekCalendar days={weekDays} {selectedDay} onSelect={pickDay} />
  </section>

  {#if form && "error" in form && form.error && !(form as { reorderToast?: string }).reorderToast}
    <div
      class="mb-4 rounded-tb-2xl border border-tb-rose-300 bg-tb-rose-50/60 p-4 text-sm text-tb-rose-700"
    >
      送出失敗：{form.error}
    </div>
  {/if}

  {#if data.error}
    <div
      class="mb-4 rounded-tb-2xl border border-tb-rose-300 bg-tb-rose-50/60 p-4 text-sm text-tb-rose-700"
    >
      載入首頁時發生錯誤：{data.error}
    </div>
  {/if}

  <!-- Today's order summary, when present -->
  {#if hasOrdered && orderSummary}
    <a
      href={`/orders/${orderSummary.order_id}`}
      class="mb-5 flex items-center justify-between gap-2 rounded-tb-2xl border border-tb-slate-200 bg-white p-4 shadow-tb-sm transition hover:shadow-tb-md"
    >
      <div>
        <p class="text-[10px] uppercase tracking-eyebrow text-tb-slate-500">今天的訂單</p>
        <p class="mt-1 font-jetbrains-mono text-base font-black tabular-nums text-tb-slate-900">
          ${orderSummary.total_price_minor.toLocaleString()}
        </p>
      </div>
      <StateTag tone={orderStatusTone[orderSummary.status] ?? "neutral"}>
        {orderStatusLabel[orderSummary.status] ?? orderSummary.status}
      </StateTag>
    </a>
  {/if}

  <!-- Featured row · 再點一次 -->
  <FeaturedRow
    title="再點一次"
    subtitle="你最近的訂單 · 一鍵重新預訂"
    moreHref="/menu/reorders"
    items={reorderCards}
  >
    {#snippet card(c)}
      <form
        method="POST"
        action="?/reorderPast"
        class={c.available_today ? "" : "pointer-events-none opacity-50"}
      >
        <input type="hidden" name="source_order_id" value={c.source_order_id} />
        <input type="hidden" name="supply_date" value={data.home.target_day} />
        <button
          type="submit"
          disabled={!c.available_today}
          class="flex w-full flex-col gap-2 rounded-tb-2xl border border-tb-slate-200 bg-white p-4 text-left shadow-tb-sm transition hover:-translate-y-0.5 hover:shadow-tb-md disabled:cursor-not-allowed"
        >
          <div class="flex items-center justify-between gap-2">
            <span class="truncate text-sm font-bold text-tb-slate-900">{c.vendor_name}</span>
            {#if c.freq > 1}
              <span
                class="shrink-0 rounded-full bg-tb-slate-100 px-2 py-0.5 text-[10px] font-semibold text-tb-slate-700"
                >× {c.freq}</span
              >
            {/if}
          </div>
          {#if c.items_preview && c.items_preview.length > 0}
            <p class="line-clamp-1 text-xs text-tb-slate-500">
              {c.items_preview.slice(0, 3).join("、")}
            </p>
          {/if}
          <div class="flex items-center justify-between">
            <span class="font-jetbrains-mono text-base font-black tabular-nums text-tb-slate-900">
              ${c.total_price_minor.toLocaleString()}
            </span>
            {#if c.available_today}
              <span class="text-xs font-bold text-tb-red-700">再點一次 →</span>
            {:else}
              <span class="text-[11px] font-semibold text-tb-rose-600">今日無供應</span>
            {/if}
          </div>
        </button>
      </form>
    {/snippet}
    {#snippet empty()}
      <div
        class="rounded-tb-2xl border border-dashed border-tb-slate-200 bg-tb-slate-50 p-6 text-center text-xs text-tb-slate-500"
      >
        還沒有訂單紀錄 — 點完第一份午餐後就會出現
      </div>
    {/snippet}
  </FeaturedRow>

  <!-- Featured row · 推薦你今天 -->
  <FeaturedRow
    title="推薦你今天"
    subtitle="同事熱門 × 你的常用商家"
    moreHref="/menu/recommendations"
    items={recommendCards}
  >
    {#snippet card(c)}
      {#if c.menu}
        <MealCard
          name={c.menu.name}
          vendor={c.menu.vendor}
          priceMinor={c.menu.price_minor}
          remain={c.menu.remain}
          capacity={c.menu.capacity}
          pickupWindow={c.menu.pickup_window}
          badges={[c.reason]}
          image={(c.menu.images ?? [])[0]}
          qty={cart.qty(c.menu.id)}
          soldOut={c.menu.sold_out}
          onIncrement={() => addMenuItem(c.menu!)}
          onDecrement={() => cart.dec(c.menu!.id)}
          isFavorite={favoritesSet.has(c.menu.id)}
          onToggleFavorite={() => toggleFavorite(c.menu!.id)}
        />
      {:else}
        <div
          class="flex h-full flex-col justify-between rounded-tb-2xl border border-tb-slate-200 bg-white p-4 opacity-70 shadow-tb-sm"
        >
          <div>
            <span
              class="rounded-full bg-tb-amber-50 px-2 py-0.5 text-[10px] font-semibold text-tb-amber-700"
              >{c.reason}</span
            >
            <h3 class="mt-2 text-sm font-bold text-tb-slate-900">{c.name}</h3>
          </div>
          <div class="mt-3 flex items-center justify-between">
            <span class="font-jetbrains-mono text-base font-black tabular-nums text-tb-slate-900">
              ${c.unit_price.toLocaleString()}
            </span>
            <span class="text-[11px] font-semibold text-tb-rose-600">今日無供應</span>
          </div>
        </div>
      {/if}
    {/snippet}
    {#snippet empty()}
      <div
        class="rounded-tb-2xl border border-dashed border-tb-slate-200 bg-tb-slate-50 p-6 text-center text-xs text-tb-slate-500"
      >
        正在收集你的偏好，先看看同事都點什麼吧
      </div>
    {/snippet}
  </FeaturedRow>

  <!-- Featured row · 我的最愛 -->
  <FeaturedRow
    title="我的最愛"
    subtitle="你收藏的菜色"
    moreHref="/menu/favorites"
    items={favoriteCards}
  >
    {#snippet card(c)}
      {#if c.menu}
        <MealCard
          name={c.menu.name}
          vendor={c.menu.vendor}
          priceMinor={c.menu.price_minor}
          remain={c.menu.remain}
          capacity={c.menu.capacity}
          pickupWindow={c.menu.pickup_window}
          image={(c.menu.images ?? [])[0]}
          qty={cart.qty(c.menu.id)}
          soldOut={c.menu.sold_out}
          onIncrement={() => addMenuItem(c.menu!)}
          onDecrement={() => cart.dec(c.menu!.id)}
          isFavorite={favoritesSet.has(c.menu.id)}
          onToggleFavorite={() => toggleFavorite(c.menu!.id)}
        />
      {:else}
        <div
          class="flex h-full flex-col justify-between rounded-tb-2xl border border-tb-slate-200 bg-white p-4 opacity-70 shadow-tb-sm"
        >
          <h3 class="text-sm font-bold text-tb-slate-900">{c.name}</h3>
          <div class="mt-3 flex items-center justify-between">
            <span class="font-jetbrains-mono text-base font-black tabular-nums text-tb-slate-900">
              ${c.unit_price.toLocaleString()}
            </span>
            <span class="text-[11px] font-semibold text-tb-rose-600">今日無供應</span>
          </div>
        </div>
      {/if}
    {/snippet}
    {#snippet empty()}
      <div
        class="rounded-tb-2xl border border-dashed border-tb-slate-200 bg-tb-slate-50 p-6 text-center text-xs text-tb-slate-500"
      >
        點 <span aria-hidden="true">⭐</span> 收藏喜歡的菜色
      </div>
    {/snippet}
  </FeaturedRow>

  <!-- Full menu grid -->
  <section>
    <div class="mb-3 flex items-end justify-between gap-2">
      <div>
        <h2 class="text-xl font-extrabold tracking-tight text-tb-slate-900">
          全部餐點 · {filteredMenu.length} 項
        </h2>
        <p class="text-sm text-tb-slate-500">配送至 {plantLabel}</p>
      </div>
      <MenuViewToggle view={menuView} onChange={setMenuView} />
    </div>

    <!-- F3 篩選列 -->
    <MenuFilterBar
      tags={data.tagPool as string[]}
      q={data.menuFilter.q}
      selectedTags={data.menuFilter.tags}
      priceMin={data.menuFilter.priceMin}
      priceMax={data.menuFilter.priceMax}
      inStock={data.menuFilter.inStock}
      sort={data.menuFilter.sort}
    />

    {#if dayMenu.length === 0 && !serverFiltered}
      <div
        class="rounded-tb-2xl border border-tb-slate-200 bg-white p-6 text-center text-sm text-tb-slate-500"
      >
        該日該廠區尚無可選餐點。試試切換另一個日期或廠區。
      </div>
    {:else if filteredMenu.length === 0}
      <div
        class="rounded-tb-2xl border border-dashed border-tb-slate-300 bg-white p-6 text-center text-sm text-tb-slate-500"
      >
        沒有符合條件的餐點。試試其他分類或清除搜尋。
      </div>
    {:else if menuView === "vendor"}
      <div class="space-y-6">
        {#each vendorGroups as group (group.vendorId)}
          <div>
            <div class="mb-3 flex items-baseline gap-2">
              <h3 class="text-base font-extrabold tracking-tight text-tb-slate-900">
                {group.vendor}
              </h3>
              <span class="text-xs text-tb-slate-500">{group.items.length} 項</span>
            </div>
            <div class="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
              {#each group.items as item (item.id)}
                <MealCard
                  name={item.name}
                  vendor={item.vendor}
                  priceMinor={item.price_minor}
                  remain={item.remain}
                  capacity={item.capacity}
                  pickupWindow={item.pickup_window}
                  image={(item.images ?? [])[0]}
                  qty={cart.qty(item.id)}
                  soldOut={item.sold_out}
                  onIncrement={() => addMenuItem(item)}
                  onDecrement={() => cart.dec(item.id)}
                  isFavorite={favoritesSet.has(item.id)}
                  onToggleFavorite={() => toggleFavorite(item.id)}
                />
              {/each}
            </div>
          </div>
        {/each}
      </div>
    {:else}
      <div class="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
        {#each filteredMenu as item (item.id)}
          <MealCard
            name={item.name}
            vendor={item.vendor}
            priceMinor={item.price_minor}
            remain={item.remain}
            capacity={item.capacity}
            pickupWindow={item.pickup_window}
            image={(item.images ?? [])[0]}
            qty={cart.qty(item.id)}
            soldOut={item.sold_out}
            onIncrement={() => addMenuItem(item)}
            onDecrement={() => cart.dec(item.id)}
            isFavorite={favoritesSet.has(item.id)}
            onToggleFavorite={() => toggleFavorite(item.id)}
          />
        {/each}
      </div>
    {/if}
  </section>

  {#if toast}
    <div
      role="alert"
      aria-live="polite"
      class="fixed top-32 left-1/2 z-[90] -translate-x-1/2 rounded-full px-4 py-2 text-sm font-bold shadow-tb-md fade-up lg:top-24
        {toast.tone === 'danger'
        ? 'bg-tb-rose-700 text-white'
        : toast.tone === 'warning'
          ? 'bg-tb-amber-500 text-white'
          : 'bg-tb-slate-900 text-white'}"
    >
      {toast.text}
    </div>
  {/if}
</div>
