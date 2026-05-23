<script lang="ts">
  // Self-service pickup. The backend verifies the order belongs to the
  // logged-in employee, so scanning someone else's sticker is rejected.
  // html5-qrcode is imported client-side in onMount (touches getUserMedia —
  // must not run during SSR / Tauri prerender).
  import { onDestroy, onMount } from "svelte";
  import { goto } from "$app/navigation";
  import { pickupOrder } from "$lib/api";
  import { parsePickupQR } from "@tbite/pickup";
  import type { Html5Qrcode } from "html5-qrcode";
  import AppIcon from "$lib/components/AppIcon.svelte";

  type Phase = "starting" | "scanning" | "redeeming" | "success" | "error";

  const SCANNER_ID = "tb-qr-reader";

  let phase = $state<Phase>("starting");
  let message = $state<string>("");
  let lastOrderId = $state<string | null>(null);

  let scanner: Html5Qrcode | null = null;
  // Guards against the forever-scan callback firing again while we redeem.
  let busy = false;

  async function startScanner() {
    phase = "starting";
    message = "";
    busy = false;
    try {
      const { Html5Qrcode } = await import("html5-qrcode");
      scanner = new Html5Qrcode(SCANNER_ID);
      await scanner.start(
        { facingMode: "environment" },
        { fps: 10, qrbox: { width: 240, height: 240 } },
        onScan,
        undefined,
      );
      phase = "scanning";
    } catch (e) {
      phase = "error";
      message =
        e instanceof Error && /permission|denied|NotAllowed/i.test(e.message)
          ? "無法存取相機,請於系統設定允許相機權限後再試。"
          : "無法啟動相機,請確認裝置相機可用。";
    }
  }

  async function stopScanner() {
    if (scanner && scanner.isScanning) {
      try {
        await scanner.stop();
      } catch {
        // already stopped / tearing down
      }
    }
    scanner = null;
  }

  async function onScan(decodedText: string) {
    if (busy) return;
    const parsed = parsePickupQR(decodedText);
    if (!parsed) return;
    busy = true;
    lastOrderId = parsed.orderId;
    await stopScanner();
    phase = "redeeming";
    try {
      await pickupOrder(parsed.orderId);
      phase = "success";
      message = "領餐完成!";
    } catch (e) {
      phase = "error";
      message = e instanceof Error ? e.message : "核銷失敗,請稍後再試。";
    }
  }

  onMount(() => {
    void startScanner();
  });

  onDestroy(() => {
    void stopScanner();
  });
</script>

<div class="flex h-full flex-col bg-tb-slate-900">
  <div
    class="flex flex-shrink-0 items-center gap-3 px-4 pb-4"
    style="padding-top: max(env(safe-area-inset-top), 1.25rem)"
  >
    <button
      type="button"
      aria-label="返回"
      onclick={() => goto("/orders")}
      class="grid h-9 w-9 place-items-center rounded-full bg-white/15 text-white"
    >
      <AppIcon name="back" class="h-5 w-5" />
    </button>
    <div>
      <h1 class="text-xl font-black text-white">掃描領餐</h1>
      <p class="text-xs text-tb-slate-400">對準餐點貼紙上的 QR 條碼</p>
    </div>
  </div>

  <div class="flex flex-1 flex-col items-center justify-center gap-6 px-6">
    <!-- Camera viewport: html5-qrcode injects the <video> here. Kept mounted
         across phases so the library always has its target element. -->
    <div
      class="overflow-hidden rounded-3xl bg-black shadow-2xl {phase === 'scanning' ||
      phase === 'starting'
        ? 'block'
        : 'hidden'}"
    >
      <div id={SCANNER_ID} class="h-72 w-72"></div>
    </div>

    {#if phase === "starting"}
      <p class="text-sm text-tb-slate-400">啟動相機中…</p>
    {:else if phase === "scanning"}
      <div class="rounded-2xl bg-white/10 px-4 py-2 text-center text-xs text-tb-slate-300">
        將餐盒上的 QR 對準框內即可自動核銷
      </div>
    {:else if phase === "redeeming"}
      <div class="flex flex-col items-center gap-3">
        <div
          class="h-12 w-12 animate-spin rounded-full border-4 border-white/20 border-t-white"
        ></div>
        <p class="text-sm text-tb-slate-300">核銷中…</p>
      </div>
    {:else if phase === "success"}
      <div class="flex flex-col items-center gap-4 text-center">
        <div class="grid h-20 w-20 place-items-center rounded-full bg-tb-emerald-600">
          <AppIcon name="check" class="h-10 w-10 text-white" />
        </div>
        <div>
          <p class="text-lg font-black text-white">{message}</p>
          {#if lastOrderId}
            <p class="mt-1 text-xs text-tb-slate-400">訂單 {lastOrderId.slice(0, 8)}</p>
          {/if}
        </div>
        <button
          type="button"
          onclick={() => goto("/orders")}
          class="rounded-2xl bg-white px-6 py-3 text-sm font-extrabold text-tb-slate-900"
        >
          回到訂單
        </button>
      </div>
    {:else if phase === "error"}
      <div class="flex flex-col items-center gap-4 text-center">
        <div class="rounded-3xl bg-tb-red-900/40 px-6 py-6 text-sm text-tb-red-200">
          {message}
        </div>
        <div class="flex gap-3">
          <button
            type="button"
            onclick={startScanner}
            class="rounded-2xl bg-white px-6 py-3 text-sm font-extrabold text-tb-slate-900"
          >
            重新掃描
          </button>
          <button
            type="button"
            onclick={() => goto("/orders")}
            class="rounded-2xl bg-white/15 px-6 py-3 text-sm font-extrabold text-white"
          >
            回到訂單
          </button>
        </div>
      </div>
    {/if}
  </div>
</div>
