<script lang="ts">
  import { StateTag, Card } from "@tbite/ui";
  import { invalidateAll } from "$app/navigation";
  import { onMount } from "svelte";

  let { data, form } = $props();

  // Auto-refresh every 15s
  onMount(() => {
    const t = setInterval(() => invalidateAll(), 15_000);
    return () => clearInterval(t);
  });

  const statusTone = {
    placed:    "info",
    cutoff:    "warning",
    ready:     "success",
    picked_up: "neutral",
    no_show:   "danger",
    cancelled: "neutral",
  } as Record<string, "info" | "neutral" | "warning" | "danger" | "success">;
  const statusLabel = {
    placed: "已預訂", cutoff: "已截單", ready: "備餐完成",
    picked_up: "已領取", no_show: "未領取", cancelled: "已取消",
  } as Record<string, string>;

  // Modal state
  let verifyOpen = $state(false);
  let verifyOrderID = $state("");
  let verifyOrderLabel = $state("");

  function openVerify(orderID: string, plant: string, total: number) {
    verifyOrderID = orderID;
    verifyOrderLabel = `${plant} · $${total.toLocaleString()}`;
    verifyOpen = true;
  }
</script>

<section class="space-y-4">
  <header class="flex items-end justify-between gap-3">
    <div>
      <h1 class="text-2xl font-black text-tb-slate-900">備餐看板</h1>
      <p class="mt-1 text-sm text-tb-slate-500">{data.date} · {data.totalCount} 筆訂單（每 15 秒自動更新）</p>
    </div>
    <div class="flex flex-wrap gap-1 rounded-full bg-tb-slate-100 p-1">
      {#each data.days as d}
        <a href="?date={d.id}"
           class="rounded-full px-3 py-1 text-xs font-semibold {data.date === d.id ? 'bg-tb-slate-900 text-white' : 'text-tb-slate-700'}">
          {d.label}
        </a>
      {/each}
    </div>
  </header>

  {#if form?.error}
    <p class="rounded-lg bg-tb-rose-50 px-3 py-2 text-sm text-tb-rose-700">{form.error}</p>
  {/if}
  {#if form?.success && form?.count}
    <p class="rounded-lg bg-emerald-50 px-3 py-2 text-sm text-emerald-700">已標記 {form.count} 筆為備餐完成</p>
  {/if}
  {#if form?.success && form?.verifiedID}
    <p class="rounded-lg bg-emerald-50 px-3 py-2 text-sm text-emerald-700">已核銷訂單</p>
  {/if}

  {#if Object.keys(data.byPlant).length === 0}
    <Card>
      <p class="text-center text-sm text-tb-slate-500">本日無訂單</p>
    </Card>
  {:else}
    {#each Object.entries(data.byPlant) as [plant, orders] (plant)}
      <form method="POST" action="?/markReady">
        <Card>
          <header class="mb-3 flex items-center justify-between">
            <h2 class="text-sm font-bold text-tb-slate-900">{plant}</h2>
            <span class="text-xs text-tb-slate-500 font-jetbrains-mono">{orders.length} 筆</span>
          </header>
          <table class="w-full text-sm">
            <thead class="text-left text-xs uppercase tracking-eyebrow text-tb-slate-500">
              <tr><th class="pb-2 pl-2">選</th><th class="pb-2">訂單</th><th class="pb-2">項目數</th><th class="pb-2 text-right">金額</th><th class="pb-2">狀態</th><th class="pb-2"></th></tr>
            </thead>
            <tbody>
              {#each orders as o (o.id)}
                <tr class="border-t border-tb-slate-100">
                  <td class="py-2 pl-2">
                    {#if o.status === "cutoff" || o.status === "placed"}
                      <input type="checkbox" name="order_id" value={o.id} />
                    {/if}
                  </td>
                  <td class="py-2 font-jetbrains-mono text-xs text-tb-slate-500">{o.id.slice(0, 8)}…</td>
                  <td class="py-2">{o.items.length}</td>
                  <td class="py-2 text-right font-jetbrains-mono tabular-nums">${o.total_price_minor.toLocaleString()}</td>
                  <td class="py-2">
                    <StateTag tone={statusTone[o.status] ?? "neutral"}>{statusLabel[o.status] ?? o.status}</StateTag>
                  </td>
                  <td class="py-2 text-right">
                    {#if o.status === "ready"}
                      <button type="button"
                        onclick={() => openVerify(o.id, o.plant, o.total_price_minor)}
                        class="rounded-lg border border-tb-red-600 px-2 py-1 text-xs font-semibold text-tb-red-700 hover:bg-tb-red-50">
                        核銷
                      </button>
                    {/if}
                  </td>
                </tr>
              {/each}
            </tbody>
          </table>
          {#if orders.some((o: any) => o.status === "cutoff" || o.status === "placed")}
            <div class="mt-3 flex justify-end">
              <button type="submit" class="rounded-lg bg-tb-red-600 px-3.5 py-2 text-sm font-semibold text-white hover:bg-tb-red-700">
                標記選取為備餐完成
              </button>
            </div>
          {/if}
        </Card>
      </form>
    {/each}
  {/if}

  {#if verifyOpen}
    <div class="fixed inset-0 z-50 flex items-center justify-center bg-tb-slate-900/40 backdrop-blur-sm" onclick={() => (verifyOpen = false)}>
      <div class="w-full max-w-md rounded-tb-2xl bg-white p-6 shadow-2xl" onclick={(e) => e.stopPropagation()}>
        <h2 class="text-lg font-black text-tb-slate-900">核銷取餐</h2>
        <p class="mt-1 text-xs text-tb-slate-500">{verifyOrderLabel}</p>
        <form method="POST" action="?/verifyPickup" class="mt-4 space-y-3">
          <input type="hidden" name="order_id" value={verifyOrderID} />
          <label class="block text-sm">
            <span class="font-semibold">員工出示的 6 位數動態碼</span>
            <input name="code" required pattern="\d{6}" inputmode="numeric" autofocus
              class="mt-1 w-full rounded-lg border border-tb-slate-300 px-3 py-2 font-jetbrains-mono text-2xl tabular-nums tracking-widest text-center" />
          </label>
          <div class="flex justify-end gap-2">
            <button type="button" onclick={() => (verifyOpen = false)}
              class="rounded-lg border border-tb-slate-300 px-3.5 py-2 text-sm font-semibold text-tb-slate-700">取消</button>
            <button type="submit"
              class="rounded-lg bg-tb-red-600 px-3.5 py-2 text-sm font-semibold text-white hover:bg-tb-red-700">完成核銷</button>
          </div>
        </form>
      </div>
    </div>
  {/if}
</section>
