<script lang="ts">
  import { LocationBar, MealCard, StateTag } from "@tbite/ui";
  import { goto, invalidateAll } from "$app/navigation";
  import { enhance } from "$app/forms";
  import { page } from "$app/stores";
  import ChipCarousel from "$lib/components/ChipCarousel.svelte";
  import ReorderChip from "$lib/components/ReorderChip.svelte";
  import FavoriteChip from "$lib/components/FavoriteChip.svelte";
  import RecommendChip from "$lib/components/RecommendChip.svelte";

  let { data, form } = $props();

  // Typed views over `data.home` payload from /api/employee/home.
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
    badges: string[] | null;
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

  // Optimistic favorites set, kept in sync as the user clicks ⭐.
  let favoritesSet = $state<Set<string>>(new Set());
  // Seed + refresh when server data changes (initial load + invalidateAll()).
  $effect(() => {
    favoritesSet = new Set(data.favoriteIds as string[]);
  });

  // Toast queue (used for reorder partial-mode result + favorite errors).
  let toast = $state<{ tone: "info" | "warning" | "danger"; text: string } | null>(null);
  function showToast(tone: "info" | "warning" | "danger", text: string) {
    toast = { tone, text };
    setTimeout(() => {
      toast = null;
    }, 4500);
  }

  // Surface reorder partial-mode flash via query-params.
  $effect(() => {
    const params = $page.url.searchParams;
    if (params.get("reorder") === "partial") {
      const names = params.get("unavailable") ?? "";
      const cnt = params.get("unavailable_count") ?? "?";
      showToast(
        "warning",
        names ? `${cnt} 項今日無供應：${names}，其餘已加入訂單` : `${cnt} 項今日無供應`,
      );
      // Remove the params from URL so toast doesn't replay on reload.
      const url = new URL(window.location.href);
      url.searchParams.delete("reorder");
      url.searchParams.delete("unavailable");
      url.searchParams.delete("unavailable_count");
      url.searchParams.delete("order_id");
      goto(url.pathname + url.search, { replaceState: true, noScroll: true });
    }
  });

  // Surface form-action errors as toast.
  $effect(() => {
    const f = form as { reorderToast?: string } | null | undefined;
    if (f?.reorderToast) {
      showToast("danger", f.reorderToast);
    }
  });

  // ── cart state ──────────────────────────────────────────────────────────
  let cart = $state<Record<string, number>>({});
  function plantChange(id: string) {
    const url = new URL(window.location.href);
    url.searchParams.set("plant", id);
    goto(url.pathname + url.search);
  }
  function dayChange(id: string) {
    const url = new URL(window.location.href);
    url.searchParams.set("day", id);
    goto(url.pathname + url.search);
  }
  function inc(id: string) {
    cart = { ...cart, [id]: (cart[id] ?? 0) + 1 };
  }
  function dec(id: string) {
    cart = { ...cart, [id]: Math.max(0, (cart[id] ?? 0) - 1) };
  }
  const cartCount = $derived(Object.values(cart).reduce((a, n) => a + n, 0));
  const cartTotal = $derived(
    Object.entries(cart).reduce((sum, [id, qty]) => {
      const item = dayMenu.find((i) => i.id === id);
      return sum + (item ? item.price_minor * qty : 0);
    }, 0),
  );
  const cartEntries = $derived(Object.entries(cart).filter(([, q]) => q > 0));

  // ── Optimistic ⭐ toggle on MealCard ─────────────────────────────────────
  function toggleFavorite(menuItemId: string) {
    const wasFav = favoritesSet.has(menuItemId);
    // optimistic
    const next = new Set(favoritesSet);
    if (wasFav) next.delete(menuItemId);
    else next.add(menuItemId);
    favoritesSet = next;

    // Build a hidden form + submit via fetch so we can revert on failure.
    const fd = new FormData();
    fd.set("menu_item_id", menuItemId);
    const action = wasFav ? "?/removeFavorite" : "?/addFavorite";
    fetch(action, { method: "POST", body: fd })
      .then((r) => {
        if (!r.ok) throw new Error("favorite toggle failed");
        // Refresh server data so favorite_chips row + ids stay in sync.
        return invalidateAll();
      })
      .catch(() => {
        // Revert.
        const reverted = new Set(favoritesSet);
        if (wasFav) reverted.add(menuItemId);
        else reverted.delete(menuItemId);
        favoritesSet = reverted;
        showToast("danger", wasFav ? "取消最愛失敗" : "加入最愛失敗");
      });
  }

  // ── helpers ─────────────────────────────────────────────────────────────
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

  function formatCutoff(iso: string): string {
    if (!iso) return "";
    return iso.slice(0, 16).replace("T", " ");
  }
</script>

<section class="space-y-4 pb-24">
  <header class="flex items-center justify-between">
    <div>
      <h1 class="text-2xl font-black text-tb-slate-900">哈囉，{data.user.display_name} 👋</h1>
      <p class="mt-1 text-sm text-tb-slate-500">
        {hasOrdered ? "今天的午餐已就緒" : "挑選你今天想預訂的餐點"}
      </p>
    </div>
    {#if cartCount > 0}
      <StateTag tone="info">已選 {cartCount} 份</StateTag>
    {/if}
  </header>

  <LocationBar
    plants={data.plants}
    selectedPlant={data.selectedPlant}
    onPlantChange={plantChange}
    days={data.days}
    selectedDay={data.selectedDay}
    onDayChange={dayChange}
  />

  {#if form && "error" in form && form.error && !(form as { reorderToast?: string }).reorderToast}
    <div
      class="rounded-tb-2xl border border-tb-rose-300 bg-tb-rose-50/60 p-4 text-sm text-tb-rose-700"
    >
      送出失敗：{form.error}
    </div>
  {/if}

  {#if data.error}
    <div
      class="rounded-tb-2xl border border-tb-rose-300 bg-tb-rose-50/60 p-4 text-sm text-tb-rose-700"
    >
      載入首頁時發生錯誤：{data.error}
    </div>
  {/if}

  {#if hasOrdered && orderSummary}
    <a
      href={`/orders/${orderSummary.order_id}`}
      class="block rounded-tb-2xl border border-tb-slate-200 bg-white p-4 shadow-tb-sm hover:shadow-tb-md"
    >
      <div class="flex items-center justify-between gap-2">
        <div>
          <p class="text-[10px] uppercase tracking-eyebrow text-tb-slate-500">今天的訂單</p>
          <p class="mt-1 font-jetbrains-mono tabular-nums text-base font-black text-tb-slate-900">
            ${orderSummary.total_price_minor.toLocaleString()}
          </p>
          <p class="mt-1 text-xs text-tb-slate-500">截單 {formatCutoff(orderSummary.cutoff_at)}</p>
        </div>
        <StateTag tone={orderStatusTone[orderSummary.status] ?? "neutral"}>
          {orderStatusLabel[orderSummary.status] ?? orderSummary.status}
        </StateTag>
      </div>
    </a>

    <details class="rounded-tb-2xl border border-tb-slate-200 bg-white p-3 shadow-tb-sm">
      <summary
        class="cursor-pointer select-none text-sm font-semibold text-tb-slate-700 hover:text-tb-slate-900"
        >今天還想加點？</summary
      >
      <div class="mt-3 space-y-4">
        <ChipCarousel
          title="再點一次"
          icon="✋"
          moreHref="/menu/reorders"
          isEmpty={reorderChips.length === 0}
          emptyHint="還沒有訂單紀錄 — 點完第一份午餐後就會出現"
        >
          {#each reorderChips as c (c.source_order_id)}
            <ReorderChip
              sourceOrderId={c.source_order_id}
              vendorName={c.vendor_name}
              totalPriceMinor={c.total_price_minor}
              freq={c.freq}
              itemsPreview={c.items_preview ?? []}
              availableToday={c.available_today}
              supplyDate={data.home.target_day}
            />
          {/each}
        </ChipCarousel>

        <ChipCarousel
          title="推薦你今天"
          icon="✨"
          moreHref="/menu/recommendations"
          isEmpty={recommendChips.length === 0}
          emptyHint="正在收集你的偏好，先看看同事都點什麼吧"
        >
          {#each recommendChips as c (c.menu_item_id)}
            <RecommendChip
              menuItemId={c.menu_item_id}
              name={c.name}
              unitPrice={c.unit_price}
              reason={c.reason}
            />
          {/each}
        </ChipCarousel>

        <ChipCarousel
          title="我的最愛"
          icon="⭐"
          moreHref="/menu/favorites"
          isEmpty={favoriteChips.length === 0}
          emptyHint="點 ⭐ 收藏喜歡的菜色"
        >
          {#each favoriteChips as c (c.menu_item_id)}
            <FavoriteChip
              menuItemId={c.menu_item_id}
              name={c.name}
              unitPrice={c.unit_price}
              availableToday={c.available_today}
            />
          {/each}
        </ChipCarousel>

        {#if dayMenu.length > 0}
          <div class="border-t border-tb-slate-100 pt-3">
            <h2 class="px-1 pb-2 text-sm font-bold text-tb-slate-900">今日菜單</h2>
            <div class="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
              {#each dayMenu as item (item.id)}
                <MealCard
                  name={item.name}
                  vendor={item.vendor}
                  priceMinor={item.price_minor}
                  remain={item.remain}
                  capacity={item.capacity}
                  pickupWindow={item.pickup_window}
                  badges={item.badges ?? []}
                  image={(item.images ?? [])[0]}
                  qty={cart[item.id] ?? 0}
                  soldOut={item.sold_out}
                  onIncrement={() => inc(item.id)}
                  onDecrement={() => dec(item.id)}
                  isFavorite={favoritesSet.has(item.id)}
                  onToggleFavorite={() => toggleFavorite(item.id)}
                />
              {/each}
            </div>
          </div>
        {/if}
      </div>
    </details>
  {:else}
    <!-- has_ordered = false: chip rows expanded, day menu below. -->
    <ChipCarousel
      title="再點一次"
      icon="✋"
      moreHref="/menu/reorders"
      isEmpty={reorderChips.length === 0}
      emptyHint="還沒有訂單紀錄 — 點完第一份午餐後就會出現"
    >
      {#each reorderChips as c (c.source_order_id)}
        <ReorderChip
          sourceOrderId={c.source_order_id}
          vendorName={c.vendor_name}
          totalPriceMinor={c.total_price_minor}
          freq={c.freq}
          itemsPreview={c.items_preview ?? []}
          availableToday={c.available_today}
          supplyDate={data.home.target_day}
        />
      {/each}
    </ChipCarousel>

    <ChipCarousel
      title="推薦你今天"
      icon="✨"
      moreHref="/menu/recommendations"
      isEmpty={recommendChips.length === 0}
      emptyHint="正在收集你的偏好，先看看同事都點什麼吧"
    >
      {#each recommendChips as c (c.menu_item_id)}
        <RecommendChip
          menuItemId={c.menu_item_id}
          name={c.name}
          unitPrice={c.unit_price}
          reason={c.reason}
        />
      {/each}
    </ChipCarousel>

    <ChipCarousel
      title="我的最愛"
      icon="⭐"
      moreHref="/menu/favorites"
      isEmpty={favoriteChips.length === 0}
      emptyHint="點 ⭐ 收藏喜歡的菜色"
    >
      {#each favoriteChips as c (c.menu_item_id)}
        <FavoriteChip
          menuItemId={c.menu_item_id}
          name={c.name}
          unitPrice={c.unit_price}
          availableToday={c.available_today}
        />
      {/each}
    </ChipCarousel>

    <div class="border-t border-tb-slate-100 pt-3">
      <h2 class="px-1 pb-2 text-sm font-bold text-tb-slate-900">今日菜單</h2>
      {#if dayMenu.length === 0}
        <div
          class="rounded-tb-2xl border border-tb-slate-200 bg-white p-6 text-center text-sm text-tb-slate-500"
        >
          該日該廠區尚無可選餐點。試試切換另一個日期或廠區。
        </div>
      {:else}
        <div class="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
          {#each dayMenu as item (item.id)}
            <MealCard
              name={item.name}
              vendor={item.vendor}
              priceMinor={item.price_minor}
              remain={item.remain}
              capacity={item.capacity}
              pickupWindow={item.pickup_window}
              badges={item.badges ?? []}
              image={(item.images ?? [])[0]}
              qty={cart[item.id] ?? 0}
              soldOut={item.sold_out}
              onIncrement={() => inc(item.id)}
              onDecrement={() => dec(item.id)}
              isFavorite={favoritesSet.has(item.id)}
              onToggleFavorite={() => toggleFavorite(item.id)}
            />
          {/each}
        </div>
      {/if}
    </div>
  {/if}

  {#if cartCount > 0}
    <form
      method="POST"
      action="?/placeOrder"
      use:enhance
      class="fixed bottom-5 left-1/2 z-30 w-[min(28rem,calc(100vw-2rem))] -translate-x-1/2 rounded-full bg-tb-slate-900 px-4 py-3 text-white shadow-tb-md"
    >
      <input type="hidden" name="plant" value={data.selectedPlant} />
      <input type="hidden" name="supply_date" value={data.selectedDay} />
      {#each cartEntries as [id, qty] (id)}
        <input type="hidden" name="item_id" value={id} />
        <input type="hidden" name="qty" value={qty} />
      {/each}
      <button class="flex w-full items-center justify-between gap-3 text-sm font-semibold">
        <span>送出預訂 · 由本月薪資代扣</span>
        <span class="font-jetbrains-mono tabular-nums">
          {cartCount} 份 · ${cartTotal.toLocaleString()}
        </span>
      </button>
    </form>
  {/if}

  {#if toast}
    <div
      role="alert"
      aria-live="polite"
      class="fixed bottom-24 left-1/2 z-40 w-[min(28rem,calc(100vw-2rem))] -translate-x-1/2 rounded-tb-2xl border px-4 py-3 text-sm shadow-tb-md
        {toast.tone === 'danger'
        ? 'border-tb-rose-300 bg-tb-rose-50 text-tb-rose-700'
        : toast.tone === 'warning'
          ? 'border-tb-amber-300 bg-tb-amber-50 text-tb-amber-700'
          : 'border-tb-slate-200 bg-white text-tb-slate-700'}"
    >
      {toast.text}
    </div>
  {/if}
</section>
