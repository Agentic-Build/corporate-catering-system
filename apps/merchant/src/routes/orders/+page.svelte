<script lang="ts">
  import { StateTag, Card, Button, Modal, Icon } from "@tbite/ui";
  import { invalidateAll } from "$app/navigation";
  import { parsePickupQR } from "@tbite/pickup";
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

  // Scan-to-serve: open the camera, decode the meal-sticker QR, mark that
  // single order ready (placed/cutoff → ready). html5-qrcode is loaded only
  // in the browser to avoid SSR failures.
  let scanOpen = $state(false);
  let scanError = $state("");
  let markReadyForm = $state<HTMLFormElement>();
  let scannedID = $state("");
  let scanner: import("html5-qrcode").Html5Qrcode | null = null;
  // Guards against html5-qrcode's decode callback firing again before
  // stopScan() resolves, which would submit mark-ready twice.
  let scanBusy = false;

  const SCAN_REGION_ID = "serve-scan-region";

  async function startScan() {
    scanError = "";
    scannedID = "";
    scanBusy = false;
    const { Html5Qrcode } = await import("html5-qrcode");
    scanner = new Html5Qrcode(SCAN_REGION_ID);
    try {
      await scanner.start(
        { facingMode: "environment" },
        { fps: 10, qrbox: { width: 220, height: 220 } },
        (decoded: string) => onDecoded(decoded),
        () => {},
      );
    } catch {
      scanError = "無法開啟相機，請確認瀏覽器相機權限。";
    }
  }

  async function stopScan() {
    if (scanner) {
      try {
        await scanner.stop();
      } catch {
        // already stopped
      }
      scanner = null;
    }
  }

  function onDecoded(text: string) {
    if (scanBusy) return;
    const parsed = parsePickupQR(text);
    if (!parsed) {
      scanError = "無法辨識的 QR，請掃描餐點貼紙。";
      return;
    }
    scanBusy = true;
    scannedID = parsed.orderId;
    stopScan().then(() => {
      scanOpen = false;
      markReadyForm?.requestSubmit();
    });
  }

  // Open the scan modal and mount the camera once the region exists.
  $effect(() => {
    if (scanOpen) {
      requestAnimationFrame(() => startScan());
    }
  });

  function closeScan() {
    stopScan();
    scanOpen = false;
  }
</script>

<section class="mb-6">
  <div class="flex items-start justify-between gap-3">
    <div>
      <div class="text-[11px] font-bold uppercase tracking-eyebrow text-tb-red-600">
        Prep Board · 備餐看板
      </div>
      <h1 class="mt-1 text-3xl font-black tracking-tight text-tb-slate-900">備餐看板</h1>
      <p class="mt-1 text-sm text-tb-slate-500">
        {data.date} · {data.totalCount} 筆訂單 · 即時更新
      </p>
    </div>
    <Button variant="primary" size="md" onclick={() => (scanOpen = true)}>
      <Icon name="qr" class="h-4 w-4" />掃描出餐
    </Button>
  </div>
  <div class="mt-2 flex flex-wrap gap-3">
    <a
      href="/prep-sheet?date={data.date}"
      class="inline-flex items-center gap-1 text-sm font-semibold text-tb-red-600 hover:text-tb-red-700"
    >
      <Icon name="doc" class="h-3.5 w-3.5" />備餐與配送輸出（分區表 · 標籤 · 配送清單）
    </a>
    <a
      href="/labels?date={data.date}"
      class="inline-flex items-center gap-1 text-sm font-semibold text-tb-slate-600 hover:text-tb-slate-900"
    >
      <Icon name="doc" class="h-3.5 w-3.5" />列印今日貼紙
    </a>
  </div>
</section>

<div
  class="no-scrollbar mb-4 flex gap-1 overflow-x-auto rounded-full bg-tb-slate-100 p-1 md:flex-wrap"
>
  {#each data.days as d (d.id)}
    <a
      href="?date={d.id}"
      class="shrink-0 whitespace-nowrap rounded-full px-3 py-1 text-xs font-semibold {data.date ===
      d.id
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

<!-- Single-order serve form, submitted programmatically after a successful scan. -->
<form method="POST" action="?/markReady" bind:this={markReadyForm} class="hidden">
  <input type="hidden" name="order_id" value={scannedID} />
</form>

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
                <th class="pb-2">訂單</th>
                <th class="pb-2">訂購明細</th>
                <th class="pb-2 text-right">金額</th>
                <th class="pb-2">狀態</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-tb-slate-100">
              {#each orders as o (o.id)}
                <tr class="hover:bg-tb-slate-50/60">
                  <td class="py-2.5 pr-3 align-top">
                    <span
                      class="font-jetbrains-mono text-[10px] text-tb-slate-400 break-all leading-tight"
                    >{o.id}</span>
                  </td>
                  <td class="py-2.5 pr-3 align-top text-xs text-tb-slate-700">
                    {#each o.items as item (item.menu_item_id)}
                      <div>
                        {data.itemsById[item.menu_item_id]?.name ?? "未知餐點"} ×{item.qty}
                      </div>
                    {/each}
                  </td>
                  <td
                    class="py-2.5 text-right align-top font-jetbrains-mono tabular-nums text-tb-slate-900"
                  >
                    ${o.total_price_minor.toLocaleString()}
                  </td>
                  <td class="py-2.5 align-top">
                    <StateTag tone={statusTone[o.status] ?? "neutral"}>
                      {statusLabel[o.status] ?? o.status}
                    </StateTag>
                  </td>
                </tr>
                {#if o.notes}
                  <tr class="bg-tb-amber-50/70">
                    <td colspan="4" class="pb-2.5 text-xs text-tb-amber-800">
                      <span class="font-bold">特殊需求：</span>{o.notes}
                    </td>
                  </tr>
                {/if}
              {/each}
            </tbody>
          </table>
        </div>
      </Card>
    {/each}
  </div>
{/if}

<Modal open={scanOpen} onClose={closeScan} title="掃描出餐">
  {#snippet children()}
    <p class="mb-3 text-xs text-tb-slate-500">
      掃描餐點貼紙上的 QR，將該筆訂單標記為備餐完成（出餐）。
    </p>
    <div
      id={SCAN_REGION_ID}
      class="mx-auto aspect-square w-full max-w-xs overflow-hidden rounded-tb-2xl bg-tb-slate-900"
    ></div>
    {#if scanError}
      <p class="mt-3 rounded-lg bg-tb-rose-50 px-3 py-2 text-sm text-tb-rose-700">{scanError}</p>
    {/if}
    <p class="mt-3 text-xs text-tb-slate-400">
      掃描成功後系統會自動標記該訂單為備餐完成（出餐）。
    </p>
  {/snippet}
  {#snippet footer()}
    <Button variant="secondary" onclick={closeScan}>關閉</Button>
  {/snippet}
</Modal>
