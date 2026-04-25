<script lang="ts">
  import { PageHeader } from "$lib/components/ui";
  import TemplateForm from "$lib/components/admin/template-form.svelte";

  import type { PageData } from "./$types";

  let { data }: { data: PageData } = $props();
</script>

<PageHeader
  eyebrow="合規治理"
  title="新增合規文件模板"
  description="定義每類商家必交哪些文件、幾天到期、提前幾天提醒、寬限幾天。"
  breadcrumbs={data.breadcrumbs}
/>

<TemplateForm
  mode="create"
  apiBearerToken={data.auth.apiBearerToken}
  initial={{
    vendorCategory: "RESTAURANT",
    templateId: "business-license",
    displayName: "商業登記文件",
    required: true,
    maxValidityDays: 365,
    reminderDaysBeforeExpiryCsv: "30,14,7",
    suspensionGraceDays: 3
  }}
/>
