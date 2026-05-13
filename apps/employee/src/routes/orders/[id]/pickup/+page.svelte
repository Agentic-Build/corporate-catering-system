<script lang="ts">
  import QRCode from "qrcode";
  import { onMount } from "svelte";
  import { invalidateAll } from "$app/navigation";

  let { data } = $props();
  let qrDataURL = $state("");
  let secondsLeft = $state(data.code.expires_in_seconds);

  async function drawQR() {
    qrDataURL = await QRCode.toDataURL(
      `tbite://pickup?order=${data.code.order_id}&code=${data.code.code}`,
      { width: 280, margin: 2, color: { dark: "#0f172a", light: "#ffffff" } },
    );
  }

  $effect(() => {
    drawQR();
  });

  onMount(() => {
    const tick = setInterval(() => {
      secondsLeft = secondsLeft - 1;
      if (secondsLeft <= 0) {
        invalidateAll();
        secondsLeft = 30;
      }
    }, 1000);
    return () => clearInterval(tick);
  });
</script>

<section class="mx-auto max-w-md space-y-5 text-center">
  <header>
    <a href={`/orders/${data.order.id}`} class="text-xs text-tb-slate-500 hover:text-tb-slate-700">
      ← 返回訂單
    </a>
    <h1 class="mt-2 text-2xl font-black text-tb-slate-900">領餐核銷</h1>
    <p class="mt-1 text-xs uppercase tracking-eyebrow text-tb-slate-500">Pickup Verification</p>
  </header>

  <div class="rounded-tb-2xl border border-tb-slate-200 bg-white p-5 shadow-tb-sm">
    {#if qrDataURL}
      <img src={qrDataURL} alt="pickup QR code" class="mx-auto rounded-lg" />
    {:else}
      <div class="mx-auto h-[280px] w-[280px] animate-pulse rounded-lg bg-tb-slate-100"></div>
    {/if}
    <p class="mt-4 font-jetbrains-mono text-5xl font-black tabular-nums tracking-widest text-tb-slate-900">
      {data.code.code}
    </p>
    <p class="mt-2 text-xs text-tb-slate-500">
      剩餘 <span class="font-jetbrains-mono tabular-nums">{Math.max(0, secondsLeft)}</span> 秒 · 每 30 秒自動換
    </p>
  </div>

  <div class="rounded-tb-2xl border border-tb-amber-300 bg-tb-amber-50/60 p-4 text-left text-xs text-tb-slate-700">
    <p class="font-semibold uppercase tracking-eyebrow">Pro Tip</p>
    <p class="mt-1">於領餐區出示此 QR Code 與動態碼，由工讀生快速核銷。</p>
    <p class="mt-1 text-tb-slate-500">領餐時請避免截圖外流；遺失可至訂單頁重新產生。</p>
  </div>
</section>
