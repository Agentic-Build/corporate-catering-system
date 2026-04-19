<script lang="ts">
  import { onDestroy, onMount } from "svelte";
  import { goto } from "$app/navigation";
  import QRCode from "qrcode";

  import {
    PageHeader,
    Card,
    Button,
    StateTag,
    toasts
  } from "$lib/components/ui";
  import PlantGuard from "$lib/components/employee/plant-guard.svelte";
  import {
    configureEmployeeApi,
    describeApiError,
    findEmployeeOrderById,
    type EmployeeOrderView,
    type PickupQrView
  } from "$lib/employee/api";
  import { isPickupEligible, pickupQrSecondsRemaining } from "$lib/employee/portal";
  import { friendlyOrderStatus, orderStatusTone, maskIdentifier } from "$lib/platform/labels";
  import { apiClient } from "$lib/platform/api";

  import type { PageData } from "./$types";

  // Minimal structural type for the Wake Lock API so we don't depend on DOM lib additions.
  interface MinimalWakeLockSentinel {
    release: () => Promise<void>;
  }

  let { data }: { data: PageData } = $props();

  const actor = $derived(data.actor);
  const plantId = $derived(actor?.scope.plantIds[0] ?? null);
  const role = $derived(actor?.role ?? null);
  const orderId = $derived(data.orderId);

  let order = $state<EmployeeOrderView | null>(null);
  let pickupQr = $state<PickupQrView | null>(null);
  let pickupImage = $state<string | null>(null);
  let loading = $state(true);
  let qrLoading = $state(false);
  let qrError = $state<string | null>(null);
  let verifying = $state(false);

  let nowEpochMs = $state(Date.now());
  let clockTimer: ReturnType<typeof setInterval> | null = null;
  let refreshTimer: ReturnType<typeof setTimeout> | null = null;
  let wakeLock: MinimalWakeLockSentinel | null = null;

  const qrSeconds = $derived(
    pickupQr ? pickupQrSecondsRemaining(nowEpochMs, pickupQr) : 0
  );
  const countdownCritical = $derived(qrSeconds > 0 && qrSeconds <= 10);

  onMount(() => {
    clockTimer = setInterval(() => {
      nowEpochMs = Date.now();
    }, 1000);

    // Best-effort screen wake lock so the QR stays on during pickup.
    void requestWakeLock();

    if (role === "employee" && plantId) {
      void bootstrap(plantId, data.auth.apiBearerToken);
    } else {
      loading = false;
    }
  });

  onDestroy(() => {
    if (clockTimer) clearInterval(clockTimer);
    if (refreshTimer) clearTimeout(refreshTimer);
    releaseWakeLock();
  });

  async function requestWakeLock() {
    try {
      const nav = globalThis.navigator as unknown as {
        wakeLock?: { request: (kind: "screen") => Promise<MinimalWakeLockSentinel> };
      };
      if (!nav?.wakeLock) return;
      wakeLock = await nav.wakeLock.request("screen");
    } catch {
      // Wake lock is nice-to-have; ignore failures (unsupported browser / permission denied).
    }
  }

  function releaseWakeLock() {
    if (!wakeLock) return;
    void wakeLock.release().catch(() => {
      /* ignore */
    });
    wakeLock = null;
  }

  async function bootstrap(resolvedPlantId: string, bearerToken: string | null) {
    loading = true;
    try {
      configureEmployeeApi(resolvedPlantId, bearerToken);
      order = await findEmployeeOrderById(orderId, { plantId: resolvedPlantId });
      if (order && isPickupEligible(order.status)) {
        await refreshPickupQr();
      }
    } catch (error) {
      qrError = describeApiError(error);
    } finally {
      loading = false;
    }
  }

  async function refreshPickupQr() {
    if (!order || !isPickupEligible(order.status)) return;
    if (refreshTimer) {
      clearTimeout(refreshTimer);
      refreshTimer = null;
    }
    qrLoading = true;
    qrError = null;
    try {
      const payload = await apiClient.employee.getEmployeePickupVerificationQr(order.orderId);
      const imageDataUrl = await QRCode.toDataURL(payload.verificationCode, {
        width: 360,
        margin: 1,
        errorCorrectionLevel: "M",
        color: {
          dark: "#0f172a",
          light: "#ffffff"
        }
      });
      pickupQr = payload;
      pickupImage = imageDataUrl;
      scheduleAutoRefresh(payload.secondsUntilRefresh);
    } catch (error) {
      qrError = describeApiError(error);
      pickupQr = null;
      pickupImage = null;
    } finally {
      qrLoading = false;
    }
  }

  function scheduleAutoRefresh(secondsUntilRefresh: number) {
    if (refreshTimer) clearTimeout(refreshTimer);
    refreshTimer = setTimeout(() => {
      void refreshPickupQr();
    }, Math.max(1000, secondsUntilRefresh * 1000));
  }

  async function onRefreshNow() {
    await refreshPickupQr();
  }

  async function onComplete() {
    if (!order || !pickupQr || verifying) return;
    verifying = true;
    try {
      await apiClient.employee.verifyPickupOrder(order.orderId, {
        verificationCode: pickupQr.verificationCode
      });
      toasts.success(`已完成領餐核銷。`);
      await goto(`/employee/orders/${order.orderId}`);
    } catch (error) {
      toasts.error(describeApiError(error));
    } finally {
      verifying = false;
    }
  }
</script>

<PlantGuard role={role} plantId={plantId}>
  <div class="mx-auto grid max-w-md gap-4">
    <PageHeader
      eyebrow={`領餐驗證 ${maskIdentifier(orderId)}`}
      title="出示領餐 QR"
      description="到領餐點出示並請現場人員掃描，QR 每 30 秒自動刷新。"
      breadcrumbs={data.breadcrumbs}
    >
      {#snippet actions()}
        <Button href={`/employee/orders/${orderId}`} variant="ghost">返回訂單</Button>
      {/snippet}
    </PageHeader>

    {#if loading}
      <Card title="同步中">
        <p class="text-sm text-slate-600">訂單載入中...</p>
      </Card>
    {:else if !order}
      <Card variant="danger" title="找不到訂單">
        <p class="text-sm text-rose-900">訂單不存在或已不可見。</p>
        <div>
          <Button href="/employee/orders" variant="secondary">返回訂單列表</Button>
        </div>
      </Card>
    {:else if !isPickupEligible(order.status)}
      <Card variant="warning" title="目前狀態不可領餐">
        <p class="text-sm text-slate-700">
          訂單狀態為
          <StateTag
            label={friendlyOrderStatus(order.status)}
            tone={orderStatusTone(order.status)}
          />
          ，請至訂單詳情確認狀態。
        </p>
        <div>
          <Button href={`/employee/orders/${order.orderId}`} variant="secondary">返回訂單</Button>
        </div>
      </Card>
    {:else}
      <Card>
        <div class="grid gap-4 text-center">
          {#if qrLoading && !pickupImage}
            <p class="text-sm text-slate-600">QR 產生中...</p>
          {:else if qrError}
            <p class="text-sm text-rose-700">{qrError}</p>
          {:else if pickupImage && pickupQr}
            <img
              class="mx-auto w-full max-w-[320px] rounded-xl border border-slate-200 bg-white p-3"
              src={pickupImage}
              alt="pickup verification qr"
            />
            <p class="break-all rounded-md bg-slate-900 px-3 py-4 text-center text-4xl font-mono font-bold tracking-[0.3em] text-white">
              {pickupQr.verificationCode}
            </p>
            <p
              class={`text-sm font-semibold ${countdownCritical ? "text-rose-700" : "text-amber-900"}`}
              aria-live="polite"
            >
              QR 更新倒數 {qrSeconds} 秒（每 {pickupQr.refreshIntervalSeconds} 秒自動更新）
            </p>
          {/if}
          <div class="grid gap-2">
            <Button variant="primary" fullWidth disabled={!pickupQr} loading={verifying} onclick={onComplete}>
              完成領餐核銷
            </Button>
            <Button variant="ghost" fullWidth onclick={onRefreshNow} loading={qrLoading}>
              立即刷新 QR
            </Button>
          </div>
        </div>
      </Card>
    {/if}
  </div>
</PlantGuard>
