<script lang="ts">
  import { PageHeader } from "$lib/components/ui";
  import RuleForm from "$lib/components/admin/rule-form.svelte";
  import { ANOMALY_RELEASE_SIGN_OFF_ISSUE_ID } from "$lib/admin/portal";

  import type { PageData } from "./$types";

  let { data }: { data: PageData } = $props();
</script>

<PageHeader
  eyebrow="異常治理"
  title="新增異常規則"
  description="定義 kind、門檻、SLA 與嚴重度。"
  breadcrumbs={data.breadcrumbs}
/>

<RuleForm
  mode="create"
  apiBearerToken={data.auth.apiBearerToken}
  initial={{
    ruleId: "rule-custom-governance",
    kind: "EXPIRY_RISK",
    displayName: "Custom Governance Rule",
    description: "Custom anomaly governance threshold",
    governanceIssueId: ANOMALY_RELEASE_SIGN_OFF_ISSUE_ID,
    enabled: true,
    thresholdValue: "7",
    thresholdComparator: "LTE",
    evaluationWindowDays: "7",
    slaMinutes: "240",
    severity: "WARNING"
  }}
/>
