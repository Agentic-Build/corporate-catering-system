<script lang="ts">
  // OrdersScreen — 預訂中 / 歷史 tabs, order cards. "ready" orders get a
  // "show pickup code" CTA that jumps to the TOTP screen for that order.
  import { onMount } from "svelte";
  import { goto } from "$app/navigation";
  import { listOrders, type Order } from "$lib/api";
  import { money, plateColor } from "$lib/sample";
  import { session } from "$lib/session.svelte";
  import Plate from "$lib/components/Plate.svelte";
  import StatusPill from "$lib/components/StatusPill.svelte";

  let tab = $state<"active" | "done">("active");
  let orders = $state<Order[]>([]);
  let loading = $state(true);
  let error = $state<string | null>(null);

  const ACTIVE = new Set(["ready", "submitted", "placed", "prepping", "preparing"]);

  async function load() {
    loading = true;
    error = null;
    try {
      orders = await listOrders();
    } catch (e) {
      error = e instanceof Error ? e.message : "載入失敗";
      orders = [];
    } finally {
      loading = false;
    }
  }

  onMount(load);

  const active = $derived(orders.filter((o) => ACTIVE.has(o.status)));
  const done = $derived(orders.filter((o) => !ACTIVE.has(o.status)));
  const list = $derived(tab === "active" ? active : done);

  function summary(o: Order): string {
    const items = o.items ?? [];
    const count = items.reduce((s, it) => s + it.qty, 0);
    return `${count} 份餐點`;
  }
  function dateLabel(o: Order): string {
    const iso = o.supply_date ?? o.placed_at;
    return iso ? iso.slice(5, 10) : "";
  }
</script>

<div class="flex h-full flex-col">
  <div class="flex-shrink-0 bg-white px-4 pb-0" style="padding-top: max(env(safe-area-inset-top), 1rem)">
    <h1 class="mb-3 text-xl font-black text-tb-slate-900">我的訂單</h1>
    <div class="flex gap-1 rounded-full bg-tb-slate-100 p-1">
      <button
        type="button"
        onclick={() => (tab = "active")}
        class="flex-1 rounded-full py-2 text-xs font-bold transition {tab === 'active'
          ? 'bg-white text-tb-slate-900 shadow-sm'
          : 'text-tb-slate-500'}"
      >
        預訂中 · {active.length}
      </button>
      <button
        type="button"
        onclick={() => (tab = "done")}
        class="flex-1 rounded-full py-2 text-xs font-bold transition {tab === 'done'
          ? 'bg-white text-tb-slate-900 shadow-sm'
          : 'text-tb-slate-500'}"
      >
        歷史 · {done.length}
      </button>
    </div>
  </div>

  <div class="no-scroll flex-1 overflow-y-auto bg-tb-slate-50 px-4 py-3">
    <div class="grid gap-3">
      {#if loading}
        {#each [0, 1] as i (i)}
          <div class="h-32 animate-pulse rounded-3xl bg-tb-slate-200"></div>
        {/each}
      {:else if error}
        <div
          class="grid place-items-center rounded-3xl border border-dashed border-tb-slate-300 bg-white py-16 text-center"
        >
          <p class="text-sm text-tb-slate-500">{error}</p>
          <button
            type="button"
            onclick={load}
            class="mt-3 rounded-full bg-tb-red-600 px-4 py-1.5 text-xs font-bold text-white"
          >
            重試
          </button>
        </div>
      {:else if list.length === 0}
        <div
          class="grid place-items-center rounded-3xl border border-dashed border-tb-slate-300 bg-white py-16 text-center"
        >
          <p class="text-sm text-tb-slate-500">
            目前沒有{tab === "active" ? "進行中" : "歷史"}訂單
          </p>
        </div>
      {:else}
        {#each list as o (o.id)}
          <article
            class="cursor-pointer rounded-3xl bg-white p-4 shadow-sm ring-1 ring-tb-slate-200/70 transition active:scale-[0.98]"
          >
            <div
              role="button"
              tabindex="0"
              onclick={() => goto(`/orders/${o.id}`)}
              onkeydown={(e) => e.key === "Enter" && goto(`/orders/${o.id}`)}
              class="mb-3 flex items-center gap-3"
            >
              <Plate
                color={plateColor(o.id)}
                class="h-14 w-14 flex-shrink-0 rounded-2xl"
              />
              <div class="min-w-0 flex-1">
                <div class="mb-0.5 flex items-center gap-2">
                  <StatusPill status={o.status} />
                  <span class="text-[10px] text-tb-slate-400">{dateLabel(o)}</span>
                </div>
                <div class="truncate text-sm font-extrabold text-tb-slate-900">
                  訂單 {o.id.slice(0, 8)}
                </div>
                <div class="truncate text-[11px] text-tb-slate-500">{summary(o)}</div>
              </div>
              <div class="text-right">
                <div class="text-base font-black tabular-nums text-tb-slate-900">
                  {money(o.total_price_minor)}
                </div>
                <div class="mt-0.5 text-[10px] text-tb-slate-400">
                  {o.plant || session.user?.plant || ""}
                </div>
              </div>
            </div>
            {#if o.status === "ready"}
              <button
                type="button"
                onclick={() => goto(`/totp?order=${o.id}`)}
                class="w-full rounded-2xl bg-tb-emerald-600 py-3 text-sm font-extrabold text-white"
              >
                出示領餐碼 →
              </button>
            {/if}
          </article>
        {/each}
      {/if}
    </div>
  </div>
</div>
