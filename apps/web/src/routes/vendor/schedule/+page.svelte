<script lang="ts">
  import { onMount } from "svelte";

  import {
    Button,
    Card,
    ConfirmDialog,
    FormField,
    PageHeader,
    TimeInput,
    toasts
  } from "$lib/components/ui";
  import { zhTW } from "$lib/i18n/zh-tw";
  import { apiClient, ensureApiClientConfigured } from "$lib/platform/api";
  import { normalizeApiFailure } from "$lib/platform/api/failure";
  import { minuteOfDayToTime, todayIsoDate } from "$lib/platform/time-formats";

  import type { PageData } from "./$types";

  let { data }: { data: PageData } = $props();

  type Policy = Awaited<ReturnType<typeof apiClient.vendor.getVendorOrderingPolicy>>;

  let policy = $state<Policy | null>(null);
  let loading = $state(true);
  let saving = $state(false);
  let errorMessage = $state<string | null>(null);
  let confirmOpen = $state(false);

  let preorderDays = $state(3);
  let cutoffMinute = $state(1080); // default 18:00

  onMount(async () => {
    try {
      ensureApiClientConfigured(data.auth.apiBearerToken);
    } catch (error) {
      errorMessage = normalizeApiFailure(error).localizedMessage;
      loading = false;
      return;
    }
    await refresh();
  });

  async function refresh() {
    loading = true;
    errorMessage = null;
    try {
      const result = await apiClient.vendor.getVendorOrderingPolicy();
      policy = result;
      preorderDays = result.preorderOpenDaysAhead;
      cutoffMinute = result.modifyCancelCutoffMinuteOfDay;
    } catch (error) {
      errorMessage = normalizeApiFailure(error).localizedMessage;
    } finally {
      loading = false;
    }
  }

  function addDaysIso(iso: string, offset: number): string {
    const [y, m, d] = iso.split("-").map((p) => Number.parseInt(p, 10));
    const dt = new Date(Date.UTC(y, m - 1, d));
    dt.setUTCDate(dt.getUTCDate() + offset);
    const yy = dt.getUTCFullYear();
    const mm = `${dt.getUTCMonth() + 1}`.padStart(2, "0");
    const dd = `${dt.getUTCDate()}`.padStart(2, "0");
    return `${yy}-${mm}-${dd}`;
  }

  const previewToday = $derived(todayIsoDate());
  const previewDeliveryDate = $derived(addDaysIso(previewToday, 1));
  const previewCutoffDate = $derived(addDaysIso(previewDeliveryDate, -1));
  const previewCutoffTime = $derived(minuteOfDayToTime(cutoffMinute));
  const hasChanges = $derived(
    policy !== null &&
      (preorderDays !== policy.preorderOpenDaysAhead ||
        cutoffMinute !== policy.modifyCancelCutoffMinuteOfDay)
  );

  function requestSave() {
    if (!hasChanges) {
      toasts.info("政策未變更。");
      return;
    }
    confirmOpen = true;
  }

  async function confirmSave() {
    if (saving) return;
    saving = true;
    try {
      const result = await apiClient.vendor.upsertVendorOrderingPolicy({
        preorderOpenDaysAhead: preorderDays,
        modifyCancelCutoffMinuteOfDay: cutoffMinute
      });
      policy = result;
      preorderDays = result.preorderOpenDaysAhead;
      cutoffMinute = result.modifyCancelCutoffMinuteOfDay;
      toasts.success("訂購政策已更新。");
      confirmOpen = false;
    } catch (error) {
      toasts.error(normalizeApiFailure(error).localizedMessage);
    } finally {
      saving = false;
    }
  }
</script>

<PageHeader
  title={zhTW.vendor.schedule.title}
  description={zhTW.vendor.schedule.description}
  breadcrumbs={data.breadcrumbs}
/>

{#if errorMessage}
  <div class="mb-4 rounded-lg border border-rose-200 bg-rose-50 px-3 py-2 text-sm text-rose-900">
    {errorMessage}
  </div>
{/if}

{#if loading}
  <p class="text-sm text-slate-600">{zhTW.common.pageLoading}</p>
{:else}
  <div class="grid gap-4">
    <Card title="更新政策">
      <div class="grid gap-4 md:grid-cols-2">
        <FormField label="預購開放天數（1–7）" required hint="員工最多可提前幾天下訂">
          <div class="flex flex-wrap gap-1.5">
            {#each [1, 2, 3, 4, 5, 6, 7] as day}
              <button
                type="button"
                class={`rounded-lg border px-3 py-1.5 text-sm font-semibold transition ${
                  preorderDays === day
                    ? "border-cyan-700 bg-cyan-700 text-white"
                    : "border-slate-300 bg-white text-slate-700 hover:border-cyan-600 hover:text-cyan-800"
                }`}
                onclick={() => (preorderDays = day)}
              >
                {day} 天
              </button>
            {/each}
          </div>
        </FormField>

        <FormField label="前日截單時間" required hint="每 15 分鐘為一刻度，範圍 15:00–20:00">
          <TimeInput
            bind:value={cutoffMinute}
            min={900}
            max={1200}
            step={900}
          />
        </FormField>
      </div>

      <Card variant="info">
        <p class="text-sm text-cyan-900">
          設定預覽：設定為 <strong>{previewCutoffTime}</strong>
          → 預計 <strong>{previewDeliveryDate}</strong>
          配送的訂單，將於 <strong>{previewCutoffDate} {previewCutoffTime}</strong> 後無法修改或取消。
        </p>
      </Card>

      <div class="flex justify-end">
        <Button variant="primary" onclick={requestSave} disabled={!hasChanges}>
          {zhTW.vendor.schedule.submit}
        </Button>
      </div>
    </Card>
  </div>
{/if}

<ConfirmDialog
  open={confirmOpen}
  title="確認更新訂購政策？"
  description={`更新後即時生效，員工下訂流程、截單時間將立刻採用新規則（預購 ${preorderDays} 天，截單 ${previewCutoffTime}）。`}
  confirmLabel="確認更新"
  tone="danger"
  loading={saving}
  onConfirm={confirmSave}
  onCancel={() => (confirmOpen = false)}
/>
