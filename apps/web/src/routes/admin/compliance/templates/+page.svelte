<script lang="ts">
  import { onMount } from "svelte";

  import { PageHeader, Card, Button, DataTable, FormField, StateTag } from "$lib/components/ui";
  import { apiClient } from "$lib/platform/api";
  import {
    configureAdminApi,
    describeApiError,
    VENDOR_CATEGORY_OPTIONS,
    type TemplateView,
    type VendorCategory
  } from "$lib/admin/api";

  import type { PageData } from "./$types";

  let { data }: { data: PageData } = $props();

  let loading = $state(true);
  let loadError = $state<string | null>(null);
  let templates = $state<TemplateView[]>([]);
  let categoryFilter = $state<"ALL" | VendorCategory>("ALL");

  const columns = [
    { id: "templateId", label: "Template ID", width: "20%" },
    { id: "vendorCategory", label: "分類", width: "14%" },
    { id: "displayName", label: "顯示名稱", width: "24%" },
    { id: "required", label: "必填", width: "8%" },
    { id: "validity", label: "有效天數", width: "12%" },
    { id: "reminders", label: "提醒", width: "12%" },
    { id: "action", label: "動作", width: "10%" }
  ];

  onMount(() => {
    void refresh();
  });

  async function refresh() {
    loading = true;
    loadError = null;
    try {
      configureAdminApi(data.auth.apiBearerToken);
      const page = await apiClient.admin.listComplianceDocumentTemplates(
        categoryFilter === "ALL" ? undefined : categoryFilter
      );
      templates = page.items;
    } catch (error) {
      loadError = describeApiError(error);
    } finally {
      loading = false;
    }
  }

  function detailHref(template: TemplateView): string {
    const id = `${template.vendorCategory}-${template.templateId}`;
    return `/admin/compliance/templates/${encodeURIComponent(id)}`;
  }
</script>

<PageHeader
  eyebrow="合規治理"
  title="合規文件模板"
  description="依商家分類定義必交文件與到期邏輯。"
  breadcrumbs={data.breadcrumbs}
>
  {#snippet actions()}
    <Button variant="secondary" href="/admin/compliance/lifecycle">執行 lifecycle</Button>
    <Button variant="primary" href="/admin/compliance/templates/new">新增模板</Button>
  {/snippet}
</PageHeader>

<Card title="篩選">
  <div class="grid gap-3 md:grid-cols-3">
    <FormField label="商家分類">
      <select
        class="rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm"
        bind:value={categoryFilter}
      >
        <option value="ALL">全部</option>
        {#each VENDOR_CATEGORY_OPTIONS as option}
          <option value={option}>{option}</option>
        {/each}
      </select>
    </FormField>
    <div class="flex items-end">
      <Button variant="primary" loading={loading} onclick={() => void refresh()}>套用</Button>
    </div>
  </div>
</Card>

{#if loadError}
  <Card variant="danger" title="載入失敗">
    <p class="text-sm text-rose-900">{loadError}</p>
  </Card>
{:else}
  <Card title="模板">
    <DataTable
      rows={templates}
      {columns}
      emptyLabel={loading ? "載入中..." : "尚無模板"}
    >
      {#snippet row(template: TemplateView)}
        <tr class="hover:bg-slate-50">
          <td class="px-3 py-2">
            <a
              class="font-mono text-xs font-semibold text-cyan-700 hover:text-cyan-900"
              href={detailHref(template)}
            >
              {template.templateId}
            </a>
          </td>
          <td class="px-3 py-2 text-xs">{template.vendorCategory}</td>
          <td class="px-3 py-2 text-xs">{template.displayName}</td>
          <td class="px-3 py-2">
            <StateTag
              label={template.required ? "YES" : "NO"}
              tone={template.required ? "warning" : "neutral"}
            />
          </td>
          <td class="px-3 py-2 text-xs">{template.maxValidityDays}d</td>
          <td class="px-3 py-2 text-xs text-slate-600">
            {template.reminderDaysBeforeExpiry.join(", ")}
          </td>
          <td class="px-3 py-2">
            <Button variant="ghost" size="sm" href={detailHref(template)}>編輯</Button>
          </td>
        </tr>
      {/snippet}
    </DataTable>
  </Card>
{/if}
