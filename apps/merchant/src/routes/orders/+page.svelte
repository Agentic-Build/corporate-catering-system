<script lang="ts">
  import { StateTag, Card, Button, Modal, Icon } from "@tbite/ui";
  import { invalidateAll } from "$app/navigation";
  import { onMount } from "svelte";

  let { data, form } = $props();

  // Live updates: an SSE stream pushes order events the moment they happen;
  // each event triggers a board re-fetch. A slow fallback poll keeps the
  // board fresh if SSE is unavailable (NATS down / proxy issue).
  onMount(() => {
    const es = new EventSource("/orders/events");
    es.onmessage = (e) => {
      let kind = "";
      try {
        kind = (JSON.parse(e.data)?.kind as string) ?? "";
      } catch {
        // Unparseable payload still signals activity — refetch anyway.
      }
      if (kind !== "ping") invalidateAll();
    };
    const fallback = setInterval(() => invalidateAll(), 60_000);
    return () => {
      es.close();
      clearInterval(fallback);
    };
  });

  const statusTone = {
    placed: "info",
    cutoff: "warning",
    ready: "success",
    picked_up: "neutral",
    no_show: "danger",
    cancelled: "neutral",
  } as Record<string, "info" | "neutral" | "warning" | "danger" | "success">;
  const statusLabel = {
    placed: "已預訂",
    cutoff: "已截單",
    ready: "備餐完成",
    picked_up: "已領取",
    no_show: "未領取",
    cancelled: "已取消",
  } as Record<string, string>;

  // Verify-pickup modal state.
  let verifyOpen = $state(false);
  let verifyOrderID = $state("");
  let verifyOrderLabel = $state("");
  let codeInput = $state<HTMLInputElement>();

  function openVerify(orderID: string, plant: string, total: number) {
    verifyOrderID = orderID;
    verifyOrderLabel = `${plant} · $${total.toLocaleString()}`;
    verifyOpen = true;
  }

  // The shared Modal focuses its close button on open; move focus to the
  // code input once the dialog has mounted so merchants can type at once.
  $effect(() => {
    if (verifyOpen) requestAnimationFrame(() => codeInput?.focus());
  });
</script>

<section class="mb-6">
  <div class="text-[11px] font-bold uppercase tracking-eyebrow text-tb-red-600">
    Prep Board · 備餐看板
  </div>
  <h1 class="mt-1 text-3xl font-black tracking-tight text-tb-slate-900">備餐看板</h1>
  <p class="mt-1 text-sm text-tb-slate-500">
    {data.date} · {data.totalCount} 筆訂單 · 即時更新
  </p>
  <a
    href="/prep-sheet?date={data.date}"
    class="mt-2 inline-flex items-center gap-1 text-sm font-semibold text-tb-red-600 hover:text-tb-red-700"
  >
    <Icon name="doc" class="h-3.5 w-3.5" />備餐與配送輸出（分區表 · 標籤 · 配送清單）
  </a>
</section>

<div class="mb-4 flex flex-wrap gap-1 rounded-full bg-tb-slate-100 p-1">
  {#each data.days as d (d.id)}
    <a
      href="?date={d.id}"
      class="rounded-full px-3 py-1 text-xs font-semibold {data.date === d.id
        ? 'bg-tb-slate-900 text-white'
        : 'text-tb-slate-700 hover:text-tb-slate-900'}"
    >
      {d.label}
    </a>
  {/each}
</div>

{#if form?.error}
  <p class="mb-4 rounded-lg bg-tb-rose-50 px-3 py-2 text-sm text-tb-rose-700">
    {form.error}
  </p>
{/if}
{#if form?.success && form?.count}
  <p class="mb-4 rounded-lg bg-tb-emerald-50 px-3 py-2 text-sm text-tb-emerald-700">
    已標記 {form.count} 筆為備餐完成
  </p>
{/if}
{#if form?.success && form?.verifiedID}
  <p class="mb-4 rounded-lg bg-tb-emerald-50 px-3 py-2 text-sm text-tb-emerald-700">已核銷訂單</p>
{/if}

{#if Object.keys(data.byPlant).length === 0}
  <div
    class="grid place-items-center rounded-tb-2xl border border-dashed border-tb-slate-300 bg-white py-16 text-center"
  >
    <Icon name="doc" class="h-9 w-9 text-tb-slate-300" />
    <p class="mt-2 text-sm font-bold text-tb-slate-700">本日無訂單</p>
    <p class="mt-1 text-xs text-tb-slate-500">員工下單後，將依廠區彙總顯示於此。</p>
  </div>
{:else}
  <div class="space-y-4">
    {#each Object.entries(data.byPlant) as [plant, orders] (plant)}
      <form method="POST" action="?/markReady">
        <Card>
          <header class="mb-3 flex items-center justify-between">
            <h2 class="text-base font-bold text-tb-slate-900">{plant}</h2>
            <span class="font-jetbrains-mono text-xs text-tb-slate-500 tabular-nums">
              {orders.length} 筆
            </span>
          </header>
          <div class="overflow-x-auto">
            <table class="w-full text-sm">
              <thead
                class="text-left text-[11px] font-bold uppercase tracking-eyebrow text-tb-slate-500"
              >
                <tr>
                  <th class="pb-2 pl-2">選</th>
                  <th class="pb-2">訂單</th>
                  <th class="pb-2 text-right">項目數</th>
                  <th class="pb-2 text-right">金額</th>
                  <th class="pb-2">狀態</th>
                  <th class="pb-2"></th>
                </tr>
              </thead>
              <tbody class="divide-y divide-tb-slate-100">
                {#each orders as o (o.id)}
                  <tr class="hover:bg-tb-slate-50/60">
                    <td class="py-2.5 pl-2">
                      {#if o.status === "cutoff" || o.status === "placed"}
                        <input
                          type="checkbox"
                          name="order_id"
                          value={o.id}
                          class="accent-tb-red-600"
                        />
                      {/if}
                    </td>
                    <td class="py-2.5 font-jetbrains-mono text-xs text-tb-slate-500">
                      {o.id.slice(0, 8)}…
                    </td>
                    <td
                      class="py-2.5 text-right font-jetbrains-mono tabular-nums text-tb-slate-700"
                    >
                      {o.items.length}
                    </td>
                    <td
                      class="py-2.5 text-right font-jetbrains-mono tabular-nums text-tb-slate-900"
                    >
                      ${o.total_price_minor.toLocaleString()}
                    </td>
                    <td class="py-2.5">
                      <StateTag tone={statusTone[o.status] ?? "neutral"}>
                        {statusLabel[o.status] ?? o.status}
                      </StateTag>
                    </td>
                    <td class="py-2.5 text-right">
                      {#if o.status === "ready"}
                        <Button
                          variant="secondary"
                          size="sm"
                          onclick={() => openVerify(o.id, o.plant, o.total_price_minor)}
                        >
                          <Icon name="qr" class="h-3.5 w-3.5" />核銷
                        </Button>
                      {/if}
                    </td>
                  </tr>
                  {#if o.notes}
                    <tr class="bg-tb-amber-50/70">
                      <td></td>
                      <td colspan="5" class="pb-2.5 text-xs text-tb-amber-800">
                        <span class="font-bold">特殊需求：</span>{o.notes}
                      </td>
                    </tr>
                  {/if}
                {/each}
              </tbody>
            </table>
          </div>
          {#if orders.some((o: any) => o.status === "cutoff" || o.status === "placed")}
            <div class="mt-3 flex justify-end">
              <Button variant="primary" size="md" type="submit">
                <Icon name="check" class="h-4 w-4" />標記選取為備餐完成
              </Button>
            </div>
          {/if}
        </Card>
      </form>
    {/each}
  </div>
{/if}

<Modal open={verifyOpen} onClose={() => (verifyOpen = false)} title="核銷取餐">
  {#snippet children()}
    <p class="mb-4 text-xs text-tb-slate-500">{verifyOrderLabel}</p>
    <form method="POST" action="?/verifyPickup" class="space-y-3">
      <input type="hidden" name="order_id" value={verifyOrderID} />
      <label class="block text-sm">
        <span class="font-semibold text-tb-slate-800">員工出示的 6 位數動態碼</span>
        <input
          bind:this={codeInput}
          name="code"
          required
          pattern="\d{6}"
          inputmode="numeric"
          class="mt-1 w-full rounded-lg border border-tb-slate-300 px-3 py-2 text-center font-jetbrains-mono text-2xl tabular-nums tracking-widest focus:border-tb-red-500 focus:outline-none focus:ring-4 focus:ring-tb-red-100"
        />
      </label>
      <div class="flex justify-end gap-2">
        <Button variant="secondary" onclick={() => (verifyOpen = false)}>取消</Button>
        <Button variant="primary" type="submit">
          <Icon name="check" class="h-4 w-4" />完成核銷
        </Button>
      </div>
    </form>
  {/snippet}
</Modal>
