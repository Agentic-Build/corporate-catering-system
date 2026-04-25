<script lang="ts">
  import { onMount } from "svelte";
  import { goto } from "$app/navigation";

  import { Button, Card, EmptyState, FormField, PageHeader, toasts } from "$lib/components/ui";
  import { zhTW } from "$lib/i18n/zh-tw";
  import { loadRecentBatchIds } from "$lib/vendor/helpers";

  import type { PageData } from "./$types";

  let { data }: { data: PageData } = $props();

  let recentBatchIds = $state<string[]>([]);
  let lookupId = $state("");

  onMount(() => {
    recentBatchIds = loadRecentBatchIds();
  });

  function lookup() {
    const id = lookupId.trim();
    if (!id) {
      toasts.error("請輸入批次編號。");
      return;
    }
    void goto(`/vendor/batches/${id}`);
  }
</script>

<PageHeader
  title={zhTW.vendor.batches.listTitle}
  description={zhTW.vendor.batches.listDescription}
  breadcrumbs={data.breadcrumbs}
>
  {#snippet actions()}
    <Button href="/vendor/batches/new" variant="primary">{zhTW.vendor.batches.create}</Button>
  {/snippet}
</PageHeader>

<div class="grid gap-4 md:grid-cols-2">
  <Card title="批次查詢">
    <div class="grid gap-3">
      <FormField label={zhTW.vendor.batches.lookupLabel}>
        <input
          class="rounded border border-slate-300 bg-white px-2 py-1.5"
          placeholder="fbatch-..."
          bind:value={lookupId}
        />
      </FormField>
      <div class="flex justify-end">
        <Button variant="primary" onclick={lookup}>開啟批次詳情</Button>
      </div>
    </div>
  </Card>

  <Card title={zhTW.vendor.batches.recentLabel}>
    {#if recentBatchIds.length === 0}
      <EmptyState title="尚無最近批次" description="建立第一個批次或輸入批次編號開始查詢。" />
    {:else}
      <ul class="grid gap-1 text-sm">
        {#each recentBatchIds as batchId}
          <li>
            <a class="block rounded border border-slate-200 bg-slate-50 px-2 py-1.5 font-mono text-xs text-cyan-700 hover:border-slate-400" href={`/vendor/batches/${batchId}`}>
              {batchId}
            </a>
          </li>
        {/each}
      </ul>
    {/if}
  </Card>
</div>
