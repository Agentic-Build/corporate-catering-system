<script lang="ts">
  import { onMount } from "svelte";
  import { browser } from "$app/environment";

  import { Button, Card, PageHeader, toasts } from "$lib/components/ui";
  import { zhTW } from "$lib/i18n/zh-tw";
  import { apiClient, ensureApiClientConfigured } from "$lib/platform/api";
  import { normalizeApiFailure } from "$lib/platform/api/failure";
  import {
    artifactTypeLabel,
    formatTaipeiDateTime,
    pushRecentBatchId
  } from "$lib/vendor/helpers";

  import type { PageData } from "./$types";

  let { data }: { data: PageData } = $props();

  type Batch = Awaited<ReturnType<typeof apiClient.vendor.getVendorFulfillmentExportBatch>>;

  let batch = $state<Batch | null>(null);
  let loading = $state(true);
  let errorMessage = $state<string | null>(null);

  onMount(async () => {
    try {
      ensureApiClientConfigured(data.auth.apiBearerToken);
    } catch (error) {
      errorMessage = normalizeApiFailure(error).localizedMessage;
      loading = false;
      return;
    }
    try {
      const result = await apiClient.vendor.getVendorFulfillmentExportBatch(data.batchId);
      batch = result;
      pushRecentBatchId(result.batchId);
    } catch (error) {
      errorMessage = normalizeApiFailure(error).localizedMessage;
      toasts.error(errorMessage);
    } finally {
      loading = false;
    }
  });

  function printBatch() {
    if (!browser) return;
    window.print();
  }

  let downloadingObjectRef = $state<string | null>(null);

  async function downloadArtifact(objectRef: string) {
    if (downloadingObjectRef) return;
    downloadingObjectRef = objectRef;
    try {
      const link = await apiClient.vendor.createVendorObjectStorageAccessLink({ objectRef });
      if (browser) {
        window.open(link.downloadUrl, "_blank", "noopener,noreferrer");
      }
    } catch (error) {
      toasts.error(normalizeApiFailure(error).localizedMessage);
    } finally {
      downloadingObjectRef = null;
    }
  }
</script>

<svelte:head>
  <style>
    @media print {
      header, aside, nav, .no-print { display: none !important; }
      body { background: white !important; }
      main { padding: 0 !important; }
    }
  </style>
</svelte:head>

<PageHeader
  title={zhTW.vendor.batches.detailTitle}
  description="批次快照：artifacts 與 objectRef 為不可變內容。"
  breadcrumbs={data.breadcrumbs}
>
  {#snippet actions()}
    <Button onclick={printBatch} disabled={!batch}>{zhTW.vendor.batches.print}</Button>
    <Button href="/vendor/batches" variant="ghost">返回列表</Button>
  {/snippet}
</PageHeader>

{#if errorMessage}
  <div class="mb-4 rounded-lg border border-rose-200 bg-rose-50 px-3 py-2 text-sm text-rose-900">
    {errorMessage}
  </div>
{/if}

{#if loading}
  <p class="text-sm text-slate-600">{zhTW.common.pageLoading}</p>
{:else if batch}
  <div class="grid gap-4">
    <Card title="批次基本資料">
      <dl class="grid gap-2 text-sm text-slate-700 md:grid-cols-2">
        <div class="flex justify-between gap-2">
          <dt class="text-slate-500">批次編號</dt>
          <dd class="font-mono font-semibold">{batch.batchId}</dd>
        </div>
        <div class="flex justify-between gap-2">
          <dt class="text-slate-500">Vendor</dt>
          <dd>{batch.vendorId}</dd>
        </div>
        <div class="flex justify-between gap-2">
          <dt class="text-slate-500">配送日</dt>
          <dd>{batch.deliveryDate}</dd>
        </div>
        <div class="flex justify-between gap-2">
          <dt class="text-slate-500">擷取時間</dt>
          <dd>{formatTaipeiDateTime(batch.capturedAt)}</dd>
        </div>
        <div class="flex justify-between gap-2">
          <dt class="text-slate-500">建立者</dt>
          <dd>{batch.generatedByActorId}</dd>
        </div>
      </dl>
    </Card>

    <Card title="artifacts">
      <div class="overflow-x-auto rounded-lg border border-slate-200">
        <table class="min-w-full text-sm">
          <thead class="bg-slate-50 text-left text-xs font-semibold text-slate-600">
            <tr>
              <th class="px-3 py-2">類型</th>
              <th class="px-3 py-2">objectRef</th>
              <th class="px-3 py-2">MIME</th>
              <th class="px-3 py-2">大小</th>
              <th class="px-3 py-2"></th>
            </tr>
          </thead>
          <tbody>
            {#if batch.artifacts.references.length === 0}
              <tr><td class="px-3 py-3 text-slate-500" colspan="5">批次沒有 artifacts。</td></tr>
            {:else}
              {#each batch.artifacts.references as artifact}
                <tr class="border-t border-slate-100">
                  <td class="px-3 py-2 font-medium">{artifactTypeLabel(artifact.artifactType)}</td>
                  <td class="px-3 py-2 font-mono text-xs break-all">{artifact.objectRef}</td>
                  <td class="px-3 py-2 text-xs">{artifact.mimeType}</td>
                  <td class="px-3 py-2 tabular-nums text-xs">{(artifact.sizeBytes / 1024).toFixed(1)} KB</td>
                  <td class="px-3 py-2 text-right">
                    <Button
                      size="sm"
                      variant="primary"
                      loading={downloadingObjectRef === artifact.objectRef}
                      onclick={() => downloadArtifact(artifact.objectRef)}
                    >
                      下載
                    </Button>
                  </td>
                </tr>
              {/each}
            {/if}
          </tbody>
        </table>
      </div>
    </Card>
  </div>
{/if}
