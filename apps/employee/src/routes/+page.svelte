<script lang="ts">
  import { LocationBar, MealCard, StateTag } from "@tbite/ui";
  import { goto } from "$app/navigation";
  let { data, form } = $props();

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
      const item = data.items.find((i: any) => i.id === id);
      return sum + (item ? item.price_minor * qty : 0);
    }, 0),
  );
  const cartEntries = $derived(Object.entries(cart).filter(([, q]) => q > 0));
</script>

<section class="space-y-4 pb-24">
  <header class="flex items-center justify-between">
    <div>
      <h1 class="text-2xl font-black text-tb-slate-900">哈囉，{data.user.display_name} 👋</h1>
      <p class="mt-1 text-sm text-tb-slate-500">挑選你今天想預訂的餐點</p>
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

  {#if form?.error}
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
      載入菜單時發生錯誤：{data.error}
    </div>
  {:else if data.items.length === 0}
    <div
      class="rounded-tb-2xl border border-tb-slate-200 bg-white p-6 text-center text-sm text-tb-slate-500"
    >
      該日該廠區尚無可選餐點。試試切換另一個日期或廠區。
    </div>
  {:else}
    <div class="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
      {#each data.items as item (item.id)}
        <MealCard
          name={item.name}
          vendor={item.vendor}
          priceMinor={item.price_minor}
          remain={item.remain}
          capacity={item.capacity}
          pickupWindow={item.pickup_window}
          badges={item.badges}
          image={(item.images ?? [])[0]}
          qty={cart[item.id] ?? 0}
          soldOut={item.sold_out}
          onIncrement={() => inc(item.id)}
          onDecrement={() => dec(item.id)}
        />
      {/each}
    </div>
  {/if}

  {#if cartCount > 0}
    <form
      method="POST"
      action="?/placeOrder"
      class="fixed bottom-5 left-1/2 z-30 w-[min(28rem,calc(100vw-2rem))] -translate-x-1/2 rounded-full bg-tb-slate-900 px-4 py-3 text-white shadow-tb-md"
    >
      <input type="hidden" name="plant" value={data.selectedPlant} />
      <input type="hidden" name="supply_date" value={data.selectedDay} />
      {#each cartEntries as [id, qty]}
        <input type="hidden" name="item_id" value={id} />
        <input type="hidden" name="qty" value={qty} />
      {/each}
      <button class="flex w-full items-center justify-between gap-3 text-sm font-semibold">
        <span>送出預訂 · 由本月薪資代扣</span>
        <span class="font-jetbrains-mono tabular-nums"
          >{cartCount} 份 · ${cartTotal.toLocaleString()}</span
        >
      </button>
    </form>
  {/if}
</section>
