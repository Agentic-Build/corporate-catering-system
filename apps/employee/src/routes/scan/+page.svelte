<script lang="ts">
  // 掃描核銷 — employee self-service pickup. Browser camera scans the meal
  // sticker QR (html5-qrcode, dynamically imported to avoid SSR); the parsed
  // order id is posted to the `scan` action. A manual order-number fallback
  // (?/manual) covers desktops / denied camera permission.
  import { PageHeader, Card, Button, Icon } from "@tbite/ui";
  import { enhance } from "$app/forms";
  import { onMount, onDestroy } from "svelte";
  import { parsePickupQR } from "@tbite/pickup";

  let { form } = $props();

  const SCANNER_ID = "qr-scanner";
  let scanner: { stop: () => Promise<void>; clear: () => void } | null = null;
  let cameraError = $state<string | null>(null);
  let scanning = $state(false);

  // Hidden form used to submit a scanned order id through the `scan` action.
  let scanForm: HTMLFormElement;
  let scannedId = $state("");
  let submitting = $state(false);

  function submitScanned(orderId: string) {
    if (submitting) return;
    submitting = true;
    scannedId = orderId;
    void stopScanner();
    scanForm.requestSubmit();
  }

  async function stopScanner() {
    if (!scanner) return;
    try {
      await scanner.stop();
      scanner.clear();
    } catch {
      // already stopped — ignore
    }
    scanner = null;
    scanning = false;
  }

  async function startScanner() {
    cameraError = null;
    const { Html5Qrcode } = await import("html5-qrcode");
    const instance = new Html5Qrcode(SCANNER_ID);
    scanner = instance;
    try {
      await instance.start(
        { facingMode: "environment" },
        { fps: 10, qrbox: { width: 220, height: 220 } },
        (decoded: string) => {
          const parsed = parsePickupQR(decoded);
          if (parsed) submitScanned(parsed.orderId);
        },
        () => {
          // per-frame decode failure — expected, ignore
        },
      );
      scanning = true;
    } catch {
      scanner = null;
      cameraError = "無法開啟相機，請改用下方的手動輸入。";
    }
  }

  onMount(() => {
    void startScanner();
  });

  onDestroy(() => {
    void stopScanner();
  });
</script>

<div class="mx-auto max-w-md">
  <PageHeader
    eyebrow="Pickup · 掃描核銷"
    title="掃描餐點 QR 領餐"
    subtitle="對準餐點貼紙上的 QR Code 即可自助核銷；無法掃描時可手動輸入訂單編號。"
  />

  {#if form?.ok}
    <Card tone="success">
      <div class="flex items-center gap-3">
        <span class="grid h-10 w-10 flex-shrink-0 place-items-center rounded-full bg-tb-emerald-100">
          <Icon name="check" class="h-5 w-5 text-tb-emerald-700" />
        </span>
        <div>
          <p class="font-bold text-tb-emerald-800">領餐完成</p>
          <p class="font-jetbrains-mono text-xs text-tb-slate-500">
            訂單 {form.pickedUpId?.slice(0, 8)}
          </p>
        </div>
      </div>
      <div class="mt-3 flex flex-wrap gap-2">
        <a
          href="/orders"
          class="inline-flex items-center gap-2 rounded-tb-lg border border-tb-slate-300 px-3.5 py-2 text-sm font-semibold text-tb-slate-800 transition hover:border-tb-slate-500"
        >
          <Icon name="doc" class="h-4 w-4" />我的訂單
        </a>
        <Button variant="primary" size="md" onclick={() => location.reload()}>繼續掃描</Button>
      </div>
    </Card>
  {:else}
    <div class="space-y-4">
      {#if form?.error}
        <p class="rounded-tb-xl bg-tb-rose-50 px-3 py-2 text-sm text-tb-rose-700">{form.error}</p>
      {/if}

      <Card>
        <div class="overflow-hidden rounded-tb-2xl bg-tb-slate-900">
          <div id={SCANNER_ID} class="aspect-square w-full"></div>
        </div>
        {#if cameraError}
          <p class="mt-3 rounded-tb-xl bg-tb-amber-50 px-3 py-2 text-sm text-tb-amber-800">
            {cameraError}
          </p>
        {:else if !scanning}
          <p class="mt-3 text-center text-sm text-tb-slate-500">正在開啟相機…</p>
        {:else}
          <p class="mt-3 text-center text-sm text-tb-slate-500">將餐點貼紙上的 QR 對準畫面框內</p>
        {/if}
      </Card>

      <Card title="手動輸入訂單編號" description="無法掃描時，輸入貼紙上的訂單編號（前 8 碼）。">
        <form method="POST" action="?/manual" class="flex flex-col gap-3 sm:flex-row sm:items-end" use:enhance>
          <label class="flex flex-1 flex-col gap-1.5 text-sm">
            <span class="text-[11px] font-bold uppercase tracking-eyebrow text-tb-slate-500">
              訂單編號
            </span>
            <input
              name="code"
              required
              maxlength="8"
              autocapitalize="off"
              autocomplete="off"
              placeholder="例如 3f9a1c4b"
              class="rounded-tb-lg border border-tb-slate-300 px-3 py-2 font-jetbrains-mono text-sm transition focus:border-tb-red-500 focus:outline-none focus:ring-4 focus:ring-tb-red-100"
            />
          </label>
          <Button variant="primary" size="md" type="submit">核銷</Button>
        </form>
      </Card>

      <p class="text-center text-sm text-tb-slate-500">
        找不到您的餐？
        <a href="/orders" class="font-semibold text-tb-red-600 hover:text-tb-red-700">
          前往訂單提出申訴 →
        </a>
      </p>
    </div>
  {/if}

  <!-- Hidden form: scanned full order id → `scan` action. -->
  <form bind:this={scanForm} method="POST" action="?/scan" class="hidden" use:enhance={() => {
    return async ({ update }) => {
      await update();
      submitting = false;
    };
  }}>
    <input type="hidden" name="orderId" value={scannedId} />
  </form>
</div>
