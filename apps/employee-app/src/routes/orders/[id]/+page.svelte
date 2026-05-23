<script lang="ts">
  // Single order detail. Lists items and offers the scan-to-pickup action
  // when the order is ready. Reached from the orders list.
  import { onMount } from "svelte";
  import { goto } from "$app/navigation";
  import { page } from "$app/stores";
  import { getOrder, type Order } from "$lib/api";
  import { money } from "$lib/sample";
  import AppIcon from "$lib/components/AppIcon.svelte";
  import StatusPill from "$lib/components/StatusPill.svelte";

  const orderId = $derived($page.params.id ?? "");

  let order = $state<Order | null>(null);
  let loading = $state(true);
  let error = $state<string | null>(null);

  async function load(id: string) {
    loading = true;
    error = null;
    try {
      order = await getOrder(id);
    } catch (e) {
      error = e instanceof Error ? e.message : "載入失敗";
      order = null;
    } finally {
      loading = false;
    }
  }

  onMount(() => load(orderId));
</script>

<div class="flex h-full flex-col bg-tb-slate-50">
  <div
    class="flex flex-shrink-0 items-center gap-3 bg-white px-4 pb-3"
    style="padding-top: max(env(safe-area-inset-top), 1rem)"
  >
    <button
      type="button"
      aria-label="返回"
      onclick={() => goto("/orders")}
      class="grid h-9 w-9 place-items-center rounded-full bg-tb-slate-100"
    >
      <AppIcon name="back" class="h-5 w-5 text-tb-slate-900" />
    </button>
    <h1 class="text-lg font-black text-tb-slate-900">訂單明細</h1>
  </div>

  <div class="no-scroll flex-1 overflow-y-auto px-4 py-3">
    {#if loading}
      <div class="h-48 animate-pulse rounded-3xl bg-tb-slate-200"></div>
    {:else if error || !order}
      <div
        class="grid place-items-center rounded-3xl border border-dashed border-tb-slate-300 bg-white py-16 text-center"
      >
        <p class="text-sm text-tb-slate-500">{error ?? "找不到訂單"}</p>
      </div>
    {:else}
      <div class="rounded-3xl bg-white p-4 shadow-sm ring-1 ring-tb-slate-200/70">
        <div class="flex items-center justify-between">
          <StatusPill status={order.status} />
          <span class="text-xs text-tb-slate-400">{order.plant}</span>
        </div>
        <div class="mt-3 grid gap-2">
          {#each order.items ?? [] as it (it.id)}
            <div class="flex items-center justify-between text-sm">
              <span class="text-tb-slate-700">餐點 ×{it.qty}</span>
              <span class="font-bold tabular-nums text-tb-slate-900">
                {money(it.unit_price_minor * it.qty)}
              </span>
            </div>
          {/each}
        </div>
        {#if order.notes}
          <div class="mt-3 rounded-xl bg-tb-slate-50 px-3 py-2 text-xs text-tb-slate-600">
            備註:{order.notes}
          </div>
        {/if}
        <div
          class="mt-3 flex items-center justify-between border-t border-tb-slate-100 pt-3 text-sm"
        >
          <span class="text-tb-slate-500">合計(薪資代扣)</span>
          <span class="text-lg font-black tabular-nums text-tb-slate-900">
            {money(order.total_price_minor)}
          </span>
        </div>
      </div>

      {#if order.status === "ready"}
        <button
          type="button"
          onclick={() => goto("/scan")}
          class="mt-4 w-full rounded-2xl bg-tb-emerald-600 py-3.5 text-sm font-extrabold text-white"
        >
          掃描領餐 →
        </button>
      {/if}
    {/if}
  </div>
</div>
