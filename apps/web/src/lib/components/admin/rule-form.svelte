<script lang="ts">
  import { goto } from "$app/navigation";

  import { Card, Button, FormField, ChipInput, toasts } from "$lib/components/ui";
  import {
    ANOMALY_RELEASE_SIGN_OFF_ISSUE_ID,
    SETTLEMENT_RELEASE_SIGN_OFF_ISSUE_ID,
    parseRequiredNumber
  } from "$lib/admin/portal";
  import { apiClient } from "$lib/platform/api";
  import {
    configureAdminApi,
    describeApiError,
    ANOMALY_COMPARATOR_OPTIONS,
    ANOMALY_RULE_KIND_OPTIONS,
    ANOMALY_SEVERITY_OPTIONS,
    type AnomalyRuleKind,
    type AnomalyRuleSeverity,
    type AnomalyThresholdComparator
  } from "$lib/admin/api";
  import {
    friendlyAnomalyRuleKind,
    friendlyAnomalySeverity
  } from "$lib/platform/labels";

  interface InitialValues {
    ruleId: string;
    kind: AnomalyRuleKind;
    displayName: string;
    description: string;
    governanceIssueId: string;
    enabled: boolean;
    thresholdValue: string;
    thresholdComparator: AnomalyThresholdComparator;
    evaluationWindowDays: string;
    slaMinutes: string;
    severity: AnomalyRuleSeverity;
  }

  interface Props {
    mode: "create" | "edit";
    initial: InitialValues;
    apiBearerToken: string | null;
    lockRuleId?: boolean;
  }

  let { mode, initial, apiBearerToken, lockRuleId = false }: Props = $props();

  // Governance issue is now a chip input so we can suggest ISS-007 / ISS-003.
  let draft = $state(
    ((init: InitialValues) => ({
      ...init,
      governanceIssueIds:
        init.governanceIssueId.trim().length > 0 ? [init.governanceIssueId.trim()] : []
    }))(initial)
  );
  let submitting = $state(false);
  let formError = $state<string | null>(null);

  const COMPARATOR_LABEL: Record<AnomalyThresholdComparator, string> = {
    LT: "小於",
    LTE: "小於等於",
    GT: "大於",
    GTE: "大於等於"
  };

  const previewSentence = $derived.by(() => {
    const kind = friendlyAnomalyRuleKind(draft.kind);
    const comparator = COMPARATOR_LABEL[draft.thresholdComparator] ?? draft.thresholdComparator;
    const threshold = draft.thresholdValue.trim().length === 0 ? "?" : draft.thresholdValue.trim();
    const window = draft.evaluationWindowDays.trim().length === 0 ? "?" : draft.evaluationWindowDays.trim();
    const severity = friendlyAnomalySeverity(draft.severity);
    const sla = draft.slaMinutes.trim().length === 0 ? "?" : draft.slaMinutes.trim();
    return `若「${kind}」${comparator} ${threshold} 持續 ${window} 天，將以「${severity}」建立告警並於 ${sla} 分鐘內應回應。`;
  });

  async function submit(event: SubmitEvent) {
    event.preventDefault();
    formError = null;

    const ruleId = draft.ruleId.trim();
    const displayName = draft.displayName.trim();
    const description = draft.description.trim();
    const governanceIssueId = (draft.governanceIssueIds[0] ?? "").trim();
    if (
      ruleId.length === 0 ||
      displayName.length === 0 ||
      description.length === 0 ||
      governanceIssueId.length === 0
    ) {
      formError = "ruleId / displayName / description / governanceIssueId 不可為空。";
      return;
    }

    let thresholdValue: number;
    let evaluationWindowDays: number;
    let slaMinutes: number;
    try {
      thresholdValue = parseRequiredNumber(draft.thresholdValue, "thresholdValue");
      evaluationWindowDays = parseRequiredNumber(
        draft.evaluationWindowDays,
        "evaluationWindowDays"
      );
      slaMinutes = parseRequiredNumber(draft.slaMinutes, "slaMinutes");
    } catch (error) {
      formError = error instanceof Error ? error.message : "規則參數無效";
      return;
    }
    if (!Number.isInteger(evaluationWindowDays) || evaluationWindowDays <= 0) {
      formError = "評估窗口必須是正整數。";
      return;
    }
    if (!Number.isInteger(slaMinutes) || slaMinutes <= 0) {
      formError = "SLA 分鐘必須是正整數。";
      return;
    }

    submitting = true;
    try {
      configureAdminApi(apiBearerToken);
      await apiClient.admin.upsertAnomalyRule(ruleId, {
        kind: draft.kind,
        displayName,
        description,
        governanceIssueId,
        enabled: draft.enabled,
        thresholdValue,
        thresholdComparator: draft.thresholdComparator,
        evaluationWindowDays,
        slaMinutes,
        severity: draft.severity
      });
      toasts.success(`規則 ${ruleId} 已儲存。`);
      await goto("/admin/anomalies/rules");
    } catch (error) {
      const message = describeApiError(error);
      formError = message;
      toasts.error(message);
    } finally {
      submitting = false;
    }
  }
</script>

<form class="grid gap-4" onsubmit={submit}>
  <Card title="身份" description="規則識別、基本描述與關聯 Governance Issue。">
    <div class="grid gap-3 md:grid-cols-2">
      <FormField label="Rule ID" required>
        <input
          class="rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm font-mono disabled:bg-slate-100"
          disabled={lockRuleId}
          bind:value={draft.ruleId}
        />
      </FormField>
      <FormField label="規則類型" required>
        <select
          class="rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm"
          bind:value={draft.kind}
        >
          {#each ANOMALY_RULE_KIND_OPTIONS as option}
            <option value={option}>{friendlyAnomalyRuleKind(option)}</option>
          {/each}
        </select>
      </FormField>
      <div class="md:col-span-2">
        <FormField label="顯示名稱" required>
          <input
            class="rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm"
            bind:value={draft.displayName}
          />
        </FormField>
      </div>
      <div class="md:col-span-2">
        <FormField label="描述" required>
          <textarea
            class="min-h-[96px] rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm"
            bind:value={draft.description}
          ></textarea>
        </FormField>
      </div>
      <div class="md:col-span-2">
        <FormField label="關聯 Governance Issue" hint="按 Enter 加入；通常是 ISS-007。" required>
          <ChipInput
            bind:values={draft.governanceIssueIds}
            placeholder="例：ISS-007"
            suggestions={[ANOMALY_RELEASE_SIGN_OFF_ISSUE_ID, SETTLEMENT_RELEASE_SIGN_OFF_ISSUE_ID]}
          />
        </FormField>
      </div>
      <label class="flex items-center gap-2 rounded-lg border border-slate-200 bg-white px-3 py-2 text-sm md:col-span-2">
        <input type="checkbox" bind:checked={draft.enabled} />
        啟用此規則
      </label>
    </div>
  </Card>

  <Card title="閾值" description="告警的觸發條件。">
    <div class="grid gap-3 md:grid-cols-3">
      <FormField label="比較器" required>
        <select
          class="rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm"
          bind:value={draft.thresholdComparator}
        >
          {#each ANOMALY_COMPARATOR_OPTIONS as option}
            <option value={option}>{COMPARATOR_LABEL[option] ?? option}</option>
          {/each}
        </select>
      </FormField>
      <FormField label="閾值" required>
        <input
          class="rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm"
          bind:value={draft.thresholdValue}
        />
      </FormField>
      <FormField label="評估窗口（天）" required>
        <input
          type="number"
          min="1"
          class="rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm"
          bind:value={draft.evaluationWindowDays}
        />
      </FormField>
    </div>
  </Card>

  <Card title="嚴重度 & SLA" description="告警的處理優先度。">
    <div class="grid gap-3 md:grid-cols-2">
      <FormField label="嚴重度" required>
        <select
          class="rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm"
          bind:value={draft.severity}
        >
          {#each ANOMALY_SEVERITY_OPTIONS as option}
            <option value={option}>{friendlyAnomalySeverity(option)}</option>
          {/each}
        </select>
      </FormField>
      <FormField label="SLA 應回應分鐘" required>
        <input
          type="number"
          min="1"
          class="rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm"
          bind:value={draft.slaMinutes}
        />
      </FormField>
    </div>
  </Card>

  <aside class="rounded-xl border border-cyan-200 bg-cyan-50/60 p-4 text-sm text-slate-800">
    <p class="text-xs font-semibold tracking-[0.14em] text-cyan-700">即時預覽</p>
    <p class="mt-1 text-sm text-slate-800">{previewSentence}</p>
  </aside>

  {#if formError}
    <p class="rounded-lg border border-rose-200 bg-rose-50 px-3 py-2 text-xs text-rose-900">
      {formError}
    </p>
  {/if}

  <div class="flex gap-2">
    <Button type="submit" variant="primary" loading={submitting}>
      {mode === "create" ? "建立規則" : "儲存變更"}
    </Button>
    <Button variant="ghost" href="/admin/anomalies/rules">取消</Button>
  </div>
</form>
