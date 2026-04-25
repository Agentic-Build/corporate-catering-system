<script lang="ts">
  import { onMount } from "svelte";

  import {
    PageHeader,
    Card,
    Button,
    DataTable,
    StateTag,
    FormField
  } from "$lib/components/ui";
  import { formatTaipeiDateTime } from "$lib/admin/portal";
  import { apiClient } from "$lib/platform/api";
  import {
    configureAdminApi,
    describeApiError,
    vendorStatusTone,
    VENDOR_STATUS_OPTIONS,
    VENDOR_SORT_FIELD_OPTIONS,
    type SortOrder,
    type VendorPage,
    type VendorSortField,
    type VendorStatus,
    type VendorView
  } from "$lib/admin/api";

  import type { PageData } from "./$types";

  let { data }: { data: PageData } = $props();

  const actor = $derived(data.actor);

  let loading = $state(true);
  let loadError = $state<string | null>(null);
  let vendors = $state<VendorView[]>([]);
  let pageMeta = $state<VendorPage["page"] | null>(null);

  let statusFilter = $state<"ALL" | VendorStatus>("ALL");
  let sortBy = $state<VendorSortField>("createdAt");
  let sortOrder = $state<SortOrder>("desc");

  const columns = [
    { id: "vendorId", label: "Vendor ID", width: "16%" },
    { id: "displayName", label: "名稱", width: "24%" },
    { id: "vendorCategory", label: "分類", width: "12%" },
    { id: "status", label: "狀態", width: "14%" },
    { id: "updatedAt", label: "最後更新", width: "22%" },
    { id: "action", label: "動作", width: "12%" }
  ];

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
      const page = await apiClient.admin.listAdminVendors(
        1,
        200,
        sortBy,
        sortOrder,
        statusFilter === "ALL" ? undefined : statusFilter
      );
      vendors = page.items;
      pageMeta = page.page;
    } catch (error) {
      loadError = describeApiError(error);
    } finally {
      loading = false;
    }
  }
</script>

<PageHeader
  eyebrow="福委會入口"
  title="商家清單"
  description="審核、檢視文件、管理廠區映射皆由此進入。"
  breadcrumbs={data.breadcrumbs}
/>

<Card title="篩選與排序">
  <div class="grid gap-3 md:grid-cols-4">
    <FormField label="狀態">
      <select
        bind:value={statusFilter}
        class="rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm"
      >
        <option value="ALL">全部</option>
        {#each VENDOR_STATUS_OPTIONS as status}
          <option value={status}>{status}</option>
        {/each}
      </select>
    </FormField>
    <FormField label="排序欄位">
      <select
        bind:value={sortBy}
        class="rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm"
      >
        {#each VENDOR_SORT_FIELD_OPTIONS as field}
          <option value={field}>{field}</option>
        {/each}
      </select>
    </FormField>
    <FormField label="排序方向">
      <select
        bind:value={sortOrder}
        class="rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm"
      >
        <option value="asc">asc</option>
        <option value="desc">desc</option>
      </select>
    </FormField>
    <div class="flex items-end">
      <Button variant="primary" loading={loading} onclick={() => void refresh()}>
        套用 / 重新載入
      </Button>
    </div>
  </div>
</Card>

{#if loadError}
  <Card variant="danger" title="載入失敗">
    <p class="text-sm text-rose-900">{loadError}</p>
  </Card>
{:else}
  <Card
    title="商家"
    description={pageMeta
      ? `page ${pageMeta.page} / ${pageMeta.totalPages}，共 ${pageMeta.totalItems} 筆`
      : undefined}
  >
    <DataTable rows={vendors} {columns} emptyLabel={loading ? "載入中..." : "尚無商家"}>
      {#snippet row(vendor: VendorView)}
        <tr class="hover:bg-slate-50">
          <td class="px-3 py-2 font-mono text-xs text-slate-700">{vendor.vendorId}</td>
          <td class="px-3 py-2">
            <a
              class="font-medium text-cyan-700 hover:text-cyan-900"
              href={`/admin/vendors/${vendor.vendorId}`}
            >
              {vendor.displayName}
            </a>
          </td>
          <td class="px-3 py-2 text-xs text-slate-600">{vendor.vendorCategory}</td>
          <td class="px-3 py-2">
            <StateTag label={vendor.status} tone={vendorStatusTone(vendor.status)} />
          </td>
          <td class="px-3 py-2 text-xs text-slate-600">{formatTaipeiDateTime(vendor.updatedAt)}</td>
          <td class="px-3 py-2">
            <Button
              variant="secondary"
              size="sm"
              href={`/admin/vendors/${vendor.vendorId}`}
            >
              詳情
            </Button>
          </td>
        </tr>
      {/snippet}
    </DataTable>
  </Card>
{/if}
