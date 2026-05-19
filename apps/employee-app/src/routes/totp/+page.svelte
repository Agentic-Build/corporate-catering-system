<script lang="ts">
  // TotpScreen — QR + rotating pickup code + 30s countdown + steps.
  // The code comes from /api/employee/orders/{id}/pickup-code; it is
  // re-fetched when the countdown elapses. If no ?order= param is given
  // it picks the first "ready" order.
  import { onDestroy, onMount } from "svelte";
  import { goto } from "$app/navigation";
  import { page } from "$app/stores";
  import { getPickupCode, listOrders, type PickupCode } from "$lib/api";
  import AppIcon from "$lib/components/AppIcon.svelte";

  let orderId = $state<string | null>(null);
  let code = $state<PickupCode | null>(null);
  let secs = $state(30);
  let error = $state<string | null>(null);
  let loading = $state(true);
  let qrModules = $state<boolean[]>([]);

  let timer: ReturnType<typeof setInterval> | undefined;

  // Faux-QR matrix: a deterministic pattern keyed off the code string so it
  // visibly changes each rotation. A real QR is produced by the `qrcode`
  // dep during native bring-up — left as a TODO to keep the build light.
  function buildMatrix(seed: string): boolean[] {
    const out: boolean[] = [];
    for (let i = 0; i < 121; i++) {
      const r = Math.floor(i / 11);
      const c = i % 11;
      const corner = (r < 3 && c < 3) || (r < 3 && c > 7) || (r > 7 && c < 3);
      const s = seed.charCodeAt(i % seed.length) || 1;
      out.push(corner ? r === 0 || r === 2 || c === 0 || c === 2 || (r === 1 && c === 1) : (r * 7 + c * 3 + s) % 3 === 0);
    }
    return out;
  }

  async function fetchCode(id: string) {
    try {
      const res = await getPickupCode(id);
      code = res;
      secs = res.expires_in_seconds > 0 ? res.expires_in_seconds : 30;
      qrModules = buildMatrix(res.code);
      error = null;
    } catch (e) {
      error = e instanceof Error ? e.message : "無法取得領餐碼";
    } finally {
      loading = false;
    }
  }

  onMount(async () => {
    const param = $page.url.searchParams.get("order");
    let id = param;
    if (!id) {
      // No order passed — find the first ready order.
      try {
        const orders = await listOrders();
        id = orders.find((o) => o.status === "ready")?.id ?? null;
      } catch {
        id = null;
      }
    }
    if (!id) {
      error = "目前沒有可領取的訂單";
      loading = false;
      return;
    }
    orderId = id;
    await fetchCode(id);

    timer = setInterval(() => {
      if (secs <= 1) {
        if (orderId) void fetchCode(orderId);
      } else {
        secs -= 1;
      }
    }, 1000);
  });

  onDestroy(() => clearInterval(timer));

  const display = $derived(code ? code.code : "······");
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
      <h1 class="text-xl font-black text-white">領餐碼</h1>
      {#if orderId}
        <p class="text-xs text-tb-slate-400">訂單 {orderId.slice(0, 8)}</p>
      {/if}
    </div>
  </div>

  <div class="flex flex-1 flex-col items-center justify-center gap-6 px-6">
    {#if loading}
      <div class="h-52 w-52 animate-pulse rounded-3xl bg-white/10"></div>
    {:else if error}
      <div class="rounded-3xl bg-white/10 px-6 py-10 text-center text-sm text-tb-slate-300">
        {error}
      </div>
    {:else}
      <!-- QR -->
      <div class="rounded-3xl bg-white p-5 shadow-2xl">
        <div class="grid h-52 w-52 grid-cols-11 grid-rows-11 gap-[3px]">
          {#each qrModules as on, i (i)}
            <div class={on ? "bg-tb-slate-900" : "bg-transparent"}></div>
          {/each}
        </div>
      </div>

      <!-- Code + countdown -->
      <div class="text-center">
        <div class="font-mono text-4xl font-black tracking-[0.18em] text-white">
          {display}
        </div>
        <div class="mt-2 text-xs text-tb-slate-400">
          動態碼 · <span class="font-bold text-tb-amber-400">{secs}s</span> 後更新
        </div>
        <div class="mx-auto mt-2 h-1.5 w-40 overflow-hidden rounded-full bg-white/15">
          <div
            class="h-full bg-tb-amber-400 transition-all duration-1000"
            style="width: {(secs / 30) * 100}%"
          ></div>
        </div>
      </div>

      <!-- Steps -->
      <div class="grid w-full gap-2 rounded-3xl bg-white/10 p-4 text-sm text-tb-slate-300">
        {#each ["前往領餐區", "於取餐時段出示此碼", "工讀生掃描完成核銷"] as step, i (i)}
          <div class="flex items-center gap-3">
            <span
              class="grid h-6 w-6 flex-shrink-0 place-items-center rounded-full bg-tb-red-600 text-[11px] font-black text-white"
            >
              {i + 1}
            </span>
            {step}
          </div>
        {/each}
      </div>
    {/if}
  </div>

  <div class="flex-shrink-0 px-4 py-4" style="padding-bottom: max(env(safe-area-inset-bottom), 1rem)">
    <div class="rounded-2xl bg-tb-amber-900/40 px-3 py-2 text-[11px] text-tb-amber-200">
      ⚠ 請勿截圖外流,動態碼每 30 秒自動更新
    </div>
  </div>
</div>
