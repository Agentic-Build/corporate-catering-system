<script lang="ts">
  import { onMount } from "svelte";

  import {
    PageHeader,
    Card,
    Button,
    StateTag,
    EmptyState,
    FormField,
    toasts
  } from "$lib/components/ui";
  import { formatTaipeiDateTime } from "$lib/admin/portal";
  import { apiClient } from "$lib/platform/api";
  import {
    configureAdminApi,
    describeApiError,
    vendorStatusTone,
    type VendorView
  } from "$lib/admin/api";

  import type { PageData } from "./$types";

  let { data }: { data: PageData } = $props();

  const actor = $derived(data.actor);
  const vendorId = $derived(data.vendorId);

  let loading = $state(true);
  let loadError = $state<string | null>(null);
  let vendor = $state<VendorView | null>(null);
  let activeTab = $state<"documents" | "review" | "lifecycle">("documents");

  let objectStorageRef = $state("");
  let objectStorageResult = $state<Awaited<
    ReturnType<typeof apiClient.admin.createAdminObjectStorageAccessLink>
  > | null>(null);
  let objectStorageLoading = $state(false);
  let objectStorageError = $state<string | null>(null);

  onMount(() => {
    if (actor?.role === "admin") {
      void refresh();
    } else {
      loading = false;
    }
  });

  async function refresh() {
    loading = true;
    loadError = null;
    try {
      configureAdminApi(data.auth.apiBearerToken);
      // Admin API does not expose getVendor; reuse list + filter.
      const page = await apiClient.admin.listAdminVendors(1, 200);
      vendor = page.items.find((v) => v.vendorId === vendorId) ?? null;
      if (!vendor) {
        loadError = `找不到商家 ${vendorId}`;
      }
    } catch (error) {
      loadError = describeApiError(error);
    } finally {
      loading = false;
    }
  }

  async function createAccessLink() {
    const ref = objectStorageRef.trim();
    if (ref.length === 0) {
      toasts.error("請先填寫 objectRef。");
      return;
    }
    objectStorageLoading = true;
    objectStorageError = null;
    objectStorageResult = null;
    try {
      objectStorageResult = await apiClient.admin.createAdminObjectStorageAccessLink({
        objectRef: ref,
        locale: "zh-TW"
      });
      toasts.success("已生成文件存取連結。");
    } catch (error) {
      const message = describeApiError(error);
      objectStorageError = message;
      toasts.error(message);
    } finally {
      objectStorageLoading = false;
    }
  }
</script>

<PageHeader
  eyebrow="商家詳情"
  title={vendor?.displayName ?? vendorId}
  description="管理商家合規、審核決策與廠區映射。"
  breadcrumbs={data.breadcrumbs}
>
  {#snippet actions()}
    <Button variant="secondary" href={`/admin/vendors/${vendorId}/mappings`}>管理廠區映射</Button>
    <Button variant="primary" href={`/admin/vendors/${vendorId}/review`}>提交審核決策</Button>
  {/snippet}
</PageHeader>

{#if loading}
  <Card title="同步中">
    <p class="text-sm text-slate-600">載入商家資料中...</p>
  </Card>
{:else if loadError || !vendor}
  <Card variant="danger" title="載入失敗">
    <p class="text-sm text-rose-900">{loadError ?? "商家不存在"}</p>
    <Button variant="secondary" href="/admin/vendors">回商家清單</Button>
  </Card>
{:else}
  <Card title="商家 meta">
    <dl class="grid gap-2 text-sm text-slate-700 md:grid-cols-4">
      <div>
        <dt class="text-xs text-slate-500">Vendor ID</dt>
        <dd class="font-mono text-xs">{vendor.vendorId}</dd>
      </div>
      <div>
        <dt class="text-xs text-slate-500">分類</dt>
        <dd>{vendor.vendorCategory}</dd>
      </div>
      <div>
        <dt class="text-xs text-slate-500">狀態</dt>
        <dd>
          <StateTag label={vendor.status} tone={vendorStatusTone(vendor.status)} />
        </dd>
      </div>
      <div>
        <dt class="text-xs text-slate-500">最後更新</dt>
        <dd>{formatTaipeiDateTime(vendor.updatedAt)}</dd>
      </div>
      <div>
        <dt class="text-xs text-slate-500">審核歷程</dt>
        <dd>{vendor.reviewHistory.length} 筆</dd>
      </div>
      <div>
        <dt class="text-xs text-slate-500">文件</dt>
        <dd>{vendor.compliance.documents.length} 筆</dd>
      </div>
      <div>
        <dt class="text-xs text-slate-500">lifecycle 歷程</dt>
        <dd>{vendor.compliance.lifecycleHistory.length} 筆</dd>
      </div>
      <div>
        <dt class="text-xs text-slate-500">保留期</dt>
        <dd>
          審核 {vendor.compliance.retentionPolicy.reviewHistoryDays}d /
          lifecycle {vendor.compliance.retentionPolicy.lifecycleHistoryDays}d
        </dd>
      </div>
    </dl>
  </Card>

  <Card>
    <div class="flex flex-wrap gap-1 border-b border-slate-200 pb-2">
      <button
        type="button"
        class={`rounded-t-lg px-3 py-2 text-sm font-medium transition ${activeTab === "documents" ? "bg-cyan-50 text-cyan-800" : "text-slate-600 hover:text-slate-900"}`}
        onclick={() => (activeTab = "documents")}
      >
        文件狀態 ({vendor.compliance.documents.length})
      </button>
      <button
        type="button"
        class={`rounded-t-lg px-3 py-2 text-sm font-medium transition ${activeTab === "review" ? "bg-cyan-50 text-cyan-800" : "text-slate-600 hover:text-slate-900"}`}
        onclick={() => (activeTab = "review")}
      >
        審核歷程 ({vendor.reviewHistory.length})
      </button>
      <button
        type="button"
        class={`rounded-t-lg px-3 py-2 text-sm font-medium transition ${activeTab === "lifecycle" ? "bg-cyan-50 text-cyan-800" : "text-slate-600 hover:text-slate-900"}`}
        onclick={() => (activeTab = "lifecycle")}
      >
        lifecycle 歷程 ({vendor.compliance.lifecycleHistory.length})
      </button>
    </div>

    {#if activeTab === "documents"}
      {#if vendor.compliance.documents.length === 0}
        <EmptyState title="目前沒有文件紀錄" description="商家提交文件後會顯示在這裡。" />
      {:else}
        <div class="overflow-x-auto">
          <table class="min-w-full divide-y divide-slate-200 text-xs">
            <thead class="bg-slate-50 text-slate-700">
              <tr>
                <th class="px-2 py-1 text-left">Template</th>
                <th class="px-2 py-1 text-left">Status</th>
                <th class="px-2 py-1 text-left">Expires</th>
                <th class="px-2 py-1 text-left">Object Ref</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-slate-100 bg-white">
              {#each vendor.compliance.documents as document}
                <tr>
                  <td class="px-2 py-1">{document.templateId}</td>
                  <td class="px-2 py-1">{document.status}</td>
                  <td class="px-2 py-1">{formatTaipeiDateTime(document.expiresOn)}</td>
                  <td class="break-all px-2 py-1 font-mono text-[11px]">{document.documentRef}</td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
      {/if}
    {:else if activeTab === "review"}
      {#if vendor.reviewHistory.length === 0}
        <EmptyState title="尚無審核歷程" description="第一次決策後會寫入這個 append-only 表。" />
      {:else}
        <div class="overflow-x-auto">
          <table class="min-w-full divide-y divide-slate-200 text-xs">
            <thead class="bg-slate-50 text-slate-700">
              <tr>
                <th class="px-2 py-1 text-left">Time</th>
                <th class="px-2 py-1 text-left">Decision</th>
                <th class="px-2 py-1 text-left">Actor</th>
                <th class="px-2 py-1 text-left">Comment</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-slate-100 bg-white">
              {#each vendor.reviewHistory as history}
                <tr>
                  <td class="px-2 py-1">{formatTaipeiDateTime(history.decidedAt)}</td>
                  <td class="px-2 py-1">{history.decision}</td>
                  <td class="px-2 py-1 font-mono text-[11px]">{history.decidedByActorId}</td>
                  <td class="px-2 py-1">{history.comment}</td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
      {/if}
    {:else if vendor.compliance.lifecycleHistory.length === 0}
      <EmptyState title="尚無 lifecycle 歷程" description="執行合規生命週期後會寫入這裡。" />
    {:else}
      <div class="overflow-x-auto">
        <table class="min-w-full divide-y divide-slate-200 text-xs">
          <thead class="bg-slate-50 text-slate-700">
            <tr>
              <th class="px-2 py-1 text-left">Time</th>
              <th class="px-2 py-1 text-left">Event</th>
              <th class="px-2 py-1 text-left">Template</th>
              <th class="px-2 py-1 text-left">Summary</th>
            </tr>
          </thead>
          <tbody class="divide-y divide-slate-100 bg-white">
            {#each vendor.compliance.lifecycleHistory as entry}
              <tr>
                <td class="px-2 py-1">{formatTaipeiDateTime(entry.occurredAt)}</td>
                <td class="px-2 py-1">{entry.eventType}</td>
                <td class="px-2 py-1">{entry.templateId ?? "-"}</td>
                <td class="px-2 py-1">{entry.summary}</td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    {/if}
  </Card>

  <Card title="下載文件" description="輸入 objectRef 後可產生下載連結。">
    <div class="grid gap-3 md:grid-cols-[1fr_auto]">
      <FormField label="objectRef">
        <input
          class="rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm"
          bind:value={objectStorageRef}
          placeholder="vendor-docs/xxx.pdf"
        />
      </FormField>
      <div class="flex items-end">
        <Button variant="primary" loading={objectStorageLoading} onclick={() => void createAccessLink()}>
          產生連結
        </Button>
      </div>
    </div>
    {#if objectStorageError}
      <p class="text-xs text-rose-700">{objectStorageError}</p>
    {/if}
    {#if objectStorageResult}
      <div class="rounded-lg border border-emerald-200 bg-emerald-50/60 p-3 text-xs text-slate-800">
        <p>
          <span class="font-semibold">URL：</span>
          <a
            class="break-all text-cyan-700 underline"
            href={objectStorageResult.downloadUrl}
            target="_blank"
            rel="noreferrer"
          >
            {objectStorageResult.downloadUrl}
          </a>
        </p>
        <p class="mt-1">
          <span class="font-semibold">到期（epoch seconds）：</span>
          {objectStorageResult.downloadExpiresAtEpochSeconds}
        </p>
      </div>
    {/if}
  </Card>
{/if}
