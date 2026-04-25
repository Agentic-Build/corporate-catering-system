<script lang="ts">
  import { onMount } from "svelte";

  import { PageHeader, Card, Button } from "$lib/components/ui";
  import TemplateForm from "$lib/components/admin/template-form.svelte";
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

  const compositeId = $derived(data.compositeId);

  let loading = $state(true);
  let loadError = $state<string | null>(null);
  let template = $state<TemplateView | null>(null);

  const parsed = $derived.by<{ category: VendorCategory | null; templateId: string | null }>(() => {
    const decoded = decodeURIComponent(compositeId);
    for (const category of VENDOR_CATEGORY_OPTIONS) {
      const prefix = `${category}-`;
      if (decoded.startsWith(prefix)) {
        return { category, templateId: decoded.slice(prefix.length) };
      }
    }
    return { category: null, templateId: null };
  });

  onMount(() => {
    void load();
  });

  async function load() {
    loading = true;
    loadError = null;
    template = null;
    try {
      configureAdminApi(data.auth.apiBearerToken);
      if (!parsed.category || !parsed.templateId) {
        loadError = `無法解析模板 ID：${compositeId}`;
        return;
      }
      const page = await apiClient.admin.listComplianceDocumentTemplates(parsed.category);
      template =
        page.items.find((item) => item.templateId === parsed.templateId) ?? null;
      if (!template) {
        loadError = `找不到模板 ${parsed.category}-${parsed.templateId}`;
      }
    } catch (error) {
      loadError = describeApiError(error);
    } finally {
      loading = false;
    }
  }
</script>

<PageHeader
  eyebrow="合規治理"
  title="編輯合規文件模板"
  description={parsed.category && parsed.templateId
    ? `${parsed.category} / ${parsed.templateId}`
    : "編輯模板"}
  breadcrumbs={data.breadcrumbs}
/>

{#if loading}
  <Card title="同步中">
    <p class="text-sm text-slate-600">載入模板中...</p>
  </Card>
{:else if loadError || !template}
  <Card variant="danger" title="載入失敗">
    <p class="text-sm text-rose-900">{loadError ?? "模板不存在"}</p>
    <Button variant="secondary" href="/admin/compliance/templates">回模板清單</Button>
  </Card>
{:else}
  <TemplateForm
    mode="edit"
    apiBearerToken={data.auth.apiBearerToken}
    lockKey
    initial={{
      vendorCategory: template.vendorCategory,
      templateId: template.templateId,
      displayName: template.displayName,
      required: template.required,
      maxValidityDays: template.maxValidityDays,
      reminderDaysBeforeExpiryCsv: template.reminderDaysBeforeExpiry.join(","),
      suspensionGraceDays: template.suspensionGraceDays
    }}
  />
{/if}
