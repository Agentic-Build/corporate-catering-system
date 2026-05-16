<script lang="ts">
  // Pickup-code view — ports the TbTotpModal body (QR + 6-digit code +
  // countdown + amber note). The QR is built from the *real* pickup code,
  // not a decorative pattern. Shared by the global TOTP modal and the
  // /orders/[id]/pickup full-page route.
  import QRCode from "qrcode";

  interface Props {
    orderId: string;
    code: string;
    /** Seconds remaining until this code expires (from the API). */
    expiresInSeconds: number;
    /** Called when the countdown hits zero so the caller can re-fetch. */
    onExpire?: () => void;
  }
  let { orderId, code, expiresInSeconds, onExpire }: Props = $props();

  let qrDataURL = $state("");
  let seconds = $state(expiresInSeconds);

  // Redraw the QR whenever the code changes; reset the countdown too.
  $effect(() => {
    seconds = expiresInSeconds;
    QRCode.toDataURL(`tbite://pickup?order=${orderId}&code=${code}`, {
      width: 280,
      margin: 1,
      color: { dark: "#0f172a", light: "#ffffff" },
    }).then((url) => {
      qrDataURL = url;
    });
  });

  // 1s countdown; on zero, ask the caller to re-fetch a fresh code.
  $effect(() => {
    const t = setInterval(() => {
      if (seconds <= 1) {
        onExpire?.();
        seconds = 30;
      } else {
        seconds -= 1;
      }
    }, 1000);
    return () => clearInterval(t);
  });
</script>

<div class="grid gap-4">
  <p class="text-sm text-tb-slate-600">於領餐區出示此 QR Code 與動態碼，由工讀生快速核銷。</p>

  <div class="grid place-items-center rounded-tb-2xl border border-tb-slate-200 bg-tb-slate-50 p-5">
    <div class="relative grid place-items-center">
      <span class="absolute left-0 top-0 h-4 w-4 border-l-2 border-t-2 border-tb-red-600"></span>
      <span class="absolute right-0 top-0 h-4 w-4 border-r-2 border-t-2 border-tb-red-600"></span>
      <span class="absolute bottom-0 left-0 h-4 w-4 border-b-2 border-l-2 border-tb-red-600"></span>
      <span class="absolute bottom-0 right-0 h-4 w-4 border-b-2 border-r-2 border-tb-red-600"></span>
      {#if qrDataURL}
        <img
          src={qrDataURL}
          alt="領餐 QR code"
          class="h-44 w-44 rounded-tb bg-white p-2 shadow-inner"
        />
      {:else}
        <div class="h-44 w-44 animate-pulse rounded-tb bg-tb-slate-200"></div>
      {/if}
    </div>
  </div>

  <div class="grid gap-1 text-center">
    <div class="text-[10px] font-bold uppercase tracking-eyebrow text-tb-slate-500">動態碼</div>
    <div
      class="font-jetbrains-mono text-3xl font-black tracking-[0.18em] tabular-nums text-tb-slate-900"
    >
      {code}
    </div>
    <div class="text-xs text-tb-slate-500">
      動態碼將於
      <span class="font-jetbrains-mono font-bold tabular-nums text-tb-red-700">
        {Math.max(0, seconds)}s
      </span>
      後更新
    </div>
  </div>

  <div
    class="flex items-start gap-2 rounded-tb-xl bg-tb-amber-50 px-3 py-2 text-xs text-tb-amber-900"
  >
    <span aria-hidden="true">ⓘ</span>
    <span>領餐時請避免截圖外流；遺失可至訂單頁重新產生。</span>
  </div>
</div>
