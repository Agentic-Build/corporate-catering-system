<script lang="ts">
  import { onMount } from "svelte";
  import { goto } from "$app/navigation";

  import { Button, Card, FormField, PageHeader, toasts } from "$lib/components/ui";
  import { apiClient, ensureApiClientConfigured } from "$lib/platform/api";
  import { normalizeApiFailure } from "$lib/platform/api/failure";
  import { pushRecentBatchId, todayTaipeiIsoDate } from "$lib/vendor/helpers";

  import type { PageData } from "./$types";

  let { data }: { data: PageData } = $props();

  let deliveryDate = $state(todayTaipeiIsoDate());
  let submitting = $state(false);

  onMount(() => {
    try {
      ensureApiClientConfigured(data.auth.apiBearerToken);
    } catch (error) {
      toasts.error(normalizeApiFailure(error).localizedMessage);
    }
  });

  async function create() {
    if (submitting) return;
    if (!deliveryDate) {
      toasts.error("請選擇配送日。");
      return;
    }
    submitting = true;
    try {
      const batch = await apiClient.vendor.createVendorFulfillmentExportBatch({
        deliveryDate
      });
      pushRecentBatchId(batch.batchId);
      toasts.success(`批次 ${batch.batchId} 已建立。`);
      await goto(`/vendor/batches/${batch.batchId}`);
    } catch (error) {
      toasts.error(normalizeApiFailure(error).localizedMessage);
    } finally {
      submitting = false;
    }
  }
</script>

<PageHeader
  title="建立備餐批次"
  description="批次為不可變快照，包含 DAILY_SUMMARY / PLANT_PARTITION_SHEET / LABELS / BASKET_LIST artifacts。"
  breadcrumbs={data.breadcrumbs}
/>

<Card title="批次資料">
  <div class="grid gap-3 md:max-w-md">
    <FormField label="配送日" required>
      <input
        type="date"
        class="rounded border border-slate-300 bg-white px-2 py-1.5"
        bind:value={deliveryDate}
      />
    </FormField>
    <div class="flex justify-end">
      <Button variant="primary" onclick={create} loading={submitting}>建立批次</Button>
    </div>
  </div>
</Card>
