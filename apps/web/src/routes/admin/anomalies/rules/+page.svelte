<script lang="ts">
  import { onMount } from "svelte";

  import { PageHeader, Card, Button, DataTable, StateTag } from "$lib/components/ui";
  import { apiClient } from "$lib/platform/api";
  import {
    configureAdminApi,
    describeApiError,
    type AnomalyRuleView
  } from "$lib/admin/api";

  import type { PageData } from "./$types";

  let { data }: { data: PageData } = $props();

  let loading = $state(true);
  let loadError = $state<string | null>(null);
  let rules = $state<AnomalyRuleView[]>([]);

  const columns = [
    { id: "ruleId", label: "ruleId", width: "18%" },
    { id: "displayName", label: "名稱", width: "22%" },
    { id: "kind", label: "kind", width: "14%" },
    { id: "severity", label: "嚴重度", width: "10%" },
    { id: "threshold", label: "門檻", width: "14%" },
    { id: "enabled", label: "啟用", width: "10%" },
    { id: "action", label: "動作", width: "12%" }
  ];

  onMount(() => {
    void refresh();
  });

  async function refresh() {
    loading = true;
    loadError = null;
    try {
      configureAdminApi(data.auth.apiBearerToken);
      const response = await apiClient.admin.listAnomalyRules();
      rules = response.items;
    } catch (error) {
      loadError = describeApiError(error);
    } finally {
      loading = false;
    }
  }

  function detailHref(rule: AnomalyRuleView): string {
    return `/admin/anomalies/rules/${encodeURIComponent(rule.ruleId)}`;
  }
</script>

<PageHeader
  eyebrow="異常治理"
  title="異常規則"
  description="管理規則的閾值、SLA、嚴重度與啟用狀態。"
  breadcrumbs={data.breadcrumbs}
>
  {#snippet actions()}
    <Button variant="primary" href="/admin/anomalies/rules/new">新增規則</Button>
  {/snippet}
</PageHeader>

{#if loadError}
  <Card variant="danger" title="載入失敗">
    <p class="text-sm text-rose-900">{loadError}</p>
  </Card>
{:else}
  <Card title="已設定規則">
    <DataTable
      rows={rules}
      {columns}
      emptyLabel={loading ? "載入中..." : "尚無規則"}
    >
      {#snippet row(rule: AnomalyRuleView)}
        <tr class="hover:bg-slate-50">
          <td class="px-3 py-2">
            <a class="font-mono text-xs font-semibold text-cyan-700 hover:text-cyan-900" href={detailHref(rule)}>
              {rule.ruleId}
            </a>
          </td>
          <td class="px-3 py-2 text-sm">{rule.displayName}</td>
          <td class="px-3 py-2 text-xs">{rule.kind}</td>
          <td class="px-3 py-2 text-xs">{rule.severity}</td>
          <td class="px-3 py-2 text-xs">
            {rule.thresholdComparator} {rule.thresholdValue}（{rule.evaluationWindowDays}d / {rule.slaMinutes}m）
          </td>
          <td class="px-3 py-2">
            <StateTag label={rule.enabled ? "ON" : "OFF"} tone={rule.enabled ? "success" : "pending"} />
          </td>
          <td class="px-3 py-2">
            <Button variant="ghost" size="sm" href={detailHref(rule)}>編輯</Button>
          </td>
        </tr>
      {/snippet}
    </DataTable>
  </Card>
{/if}
