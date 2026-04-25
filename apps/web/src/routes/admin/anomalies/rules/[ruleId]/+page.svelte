<script lang="ts">
  import { onMount } from "svelte";

  import { PageHeader, Card, Button } from "$lib/components/ui";
  import RuleForm from "$lib/components/admin/rule-form.svelte";
  import { apiClient } from "$lib/platform/api";
  import {
    configureAdminApi,
    describeApiError,
    type AnomalyRuleView
  } from "$lib/admin/api";

  import type { PageData } from "./$types";

  let { data }: { data: PageData } = $props();

  const ruleId = $derived(data.ruleId);

  let loading = $state(true);
  let loadError = $state<string | null>(null);
  let rule = $state<AnomalyRuleView | null>(null);

  onMount(() => {
    void load();
  });

  async function load() {
    loading = true;
    loadError = null;
    try {
      configureAdminApi(data.auth.apiBearerToken);
      const response = await apiClient.admin.listAnomalyRules();
      rule = response.items.find((r) => r.ruleId === ruleId) ?? null;
      if (!rule) {
        loadError = `找不到規則 ${ruleId}`;
      }
    } catch (error) {
      loadError = describeApiError(error);
    } finally {
      loading = false;
    }
  }
</script>

<PageHeader
  eyebrow="異常治理"
  title={rule?.displayName ?? ruleId}
  description={`編輯 ${ruleId}`}
  breadcrumbs={data.breadcrumbs}
/>

{#if loading}
  <Card title="同步中">
    <p class="text-sm text-slate-600">載入規則中...</p>
  </Card>
{:else if loadError || !rule}
  <Card variant="danger" title="載入失敗">
    <p class="text-sm text-rose-900">{loadError ?? "規則不存在"}</p>
    <Button variant="secondary" href="/admin/anomalies/rules">回規則列表</Button>
  </Card>
{:else}
  <RuleForm
    mode="edit"
    apiBearerToken={data.auth.apiBearerToken}
    lockRuleId
    initial={{
      ruleId: rule.ruleId,
      kind: rule.kind,
      displayName: rule.displayName,
      description: rule.description,
      governanceIssueId: rule.governanceIssueId,
      enabled: rule.enabled,
      thresholdValue: String(rule.thresholdValue),
      thresholdComparator: rule.thresholdComparator,
      evaluationWindowDays: String(rule.evaluationWindowDays),
      slaMinutes: String(rule.slaMinutes),
      severity: rule.severity
    }}
  />
{/if}
