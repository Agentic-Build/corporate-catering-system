<script lang="ts">
  import { onMount } from "svelte";

  import {
    PageHeader,
    Card,
    Button,
    FormField,
    StateTag,
    EmptyState,
    ChipInput,
    toasts
  } from "$lib/components/ui";
  import {
    ANOMALY_RELEASE_SIGN_OFF_ISSUE_ID,
    formatTaipeiDateTime
  } from "$lib/admin/portal";
  import { apiClient } from "$lib/platform/api";
  import {
    configureAdminApi,
    describeApiError,
    anomalyStatusTone,
    slaStatusTone,
    normalizeOptional,
    type AnomalyAlertView
  } from "$lib/admin/api";
  import {
    friendlyAnomalyStatus,
    friendlyAnomalySeverity,
    anomalySeverityTone,
    friendlyAnomalyRuleKind,
    maskIdentifier
  } from "$lib/platform/labels";

  import type { PageData } from "./$types";

  let { data }: { data: PageData } = $props();

  const alertId = $derived(data.alertId);

  let loading = $state(true);
  let loadError = $state<string | null>(null);
  let alert = $state<AnomalyAlertView | null>(null);

  // Per-action state
  let assignOwnerId = $state("");
  let note = $state("");
  let submitting = $state(false);
  let actionError = $state<string | null>(null);

  // Assign modal
  let assignOpen = $state(false);

  // Close modal
  let closeOpen = $state(false);
  let closeSignedOff = $state(false);
  let closureNote = $state("");
  let closureEvidenceRefs = $state<string[]>([]);
  let ticketReference = $state("");
  let closeError = $state<string | null>(null);

  onMount(() => {
    void refresh();
  });

  async function refresh() {
    loading = true;
    loadError = null;
    try {
      configureAdminApi(data.auth.apiBearerToken);
      const response = await apiClient.admin.listAnomalyAlerts();
      alert = response.items.find((a) => a.alertId === alertId) ?? null;
      if (!alert) {
        loadError = `找不到告警 ${alertId}`;
      }
    } catch (error) {
      loadError = describeApiError(error);
    } finally {
      loading = false;
    }
  }

  // Enable/disable actions based on current alert state.
  const canAcknowledge = $derived(alert?.status === "OPEN");
  const canStartRemediation = $derived(alert?.status === "ACKNOWLEDGED");
  const canEscalate = $derived(
    alert ? alert.status !== "CLOSED" && alert.status !== "ESCALATED" : false
  );
  const canClose = $derived(alert ? alert.status !== "CLOSED" : false);
  const canAssign = $derived(alert ? alert.status !== "CLOSED" : false);

  async function patchSimple(operation: "ACKNOWLEDGE" | "START_REMEDIATION" | "ESCALATE") {
    actionError = null;
    submitting = true;
    try {
      configureAdminApi(data.auth.apiBearerToken);
      const updated = await apiClient.admin.updateAdminAnomalyAlert(alertId, {
        operation,
        note: normalizeOptional(note)
      });
      toasts.success(`告警已更新為 ${friendlyAnomalyStatus(updated.status)}。`);
      alert = { ...alert!, ...updated };
      note = "";
      await refresh();
    } catch (error) {
      const message = describeApiError(error);
      actionError = message;
      toasts.error(message);
    } finally {
      submitting = false;
    }
  }

  async function submitAssign() {
    actionError = null;
    const owner = assignOwnerId.trim();
    if (owner.length === 0) {
      actionError = "請輸入負責人 ActorId。";
      return;
    }
    submitting = true;
    try {
      configureAdminApi(data.auth.apiBearerToken);
      const updated = await apiClient.admin.updateAdminAnomalyAlert(alertId, {
        operation: "ASSIGN_OWNER",
        ownerActorId: owner,
        note: normalizeOptional(note)
      });
      toasts.success(`已指派給 ${owner}。`);
      alert = { ...alert!, ...updated };
      assignOpen = false;
      assignOwnerId = "";
      note = "";
      await refresh();
    } catch (error) {
      const message = describeApiError(error);
      actionError = message;
      toasts.error(message);
    } finally {
      submitting = false;
    }
  }

  async function submitClose() {
    closeError = null;
    if (!closeSignedOff) {
      closeError = `請先勾選已取得 ${ANOMALY_RELEASE_SIGN_OFF_ISSUE_ID} 簽核。`;
      return;
    }
    if (closureNote.trim().length === 0) {
      closeError = "結案備註為必填。";
      return;
    }
    if (closureEvidenceRefs.length === 0) {
      closeError = "至少需要一個結案證據（audit:// 連結或工單編號）。";
      return;
    }
    submitting = true;
    try {
      configureAdminApi(data.auth.apiBearerToken);
      const updated = await apiClient.admin.updateAdminAnomalyAlert(alertId, {
        operation: "CLOSE",
        issueChecklist: [ANOMALY_RELEASE_SIGN_OFF_ISSUE_ID],
        closureNote: closureNote.trim(),
        closureEvidenceRefs: [...closureEvidenceRefs],
        ticketReference: normalizeOptional(ticketReference),
        note: normalizeOptional(note)
      });
      toasts.success(`告警 ${updated.alertId} 已結案。`);
      alert = { ...alert!, ...updated };
      closeOpen = false;
      closeSignedOff = false;
      closureNote = "";
      closureEvidenceRefs = [];
      ticketReference = "";
      note = "";
      await refresh();
    } catch (error) {
      const message = describeApiError(error);
      closeError = message;
      toasts.error(message);
    } finally {
      submitting = false;
    }
  }
</script>

<PageHeader
  eyebrow="異常告警"
  title={alert?.ruleDisplayName ?? alertId}
  description="推進告警狀態，或以 ISS-007 簽核的 CLOSE 結案。"
  breadcrumbs={data.breadcrumbs}
/>

{#if loading}
  <Card title="同步中">
    <p class="text-sm text-slate-600">載入告警中...</p>
  </Card>
{:else if loadError || !alert}
  <Card variant="danger" title="載入失敗">
    <p class="text-sm text-rose-900">{loadError ?? "找不到告警"}</p>
    <Button variant="secondary" href="/admin/anomalies">回告警列表</Button>
  </Card>
{:else}
  <!-- Hero: severity pill · SLA countdown · owner · observed vs threshold -->
  <Card>
    <div class="grid gap-3 md:grid-cols-4">
      <div class="grid gap-1">
        <span class="text-xs text-slate-500">嚴重度</span>
        <StateTag
          label={friendlyAnomalySeverity(alert.severity)}
          tone={anomalySeverityTone(alert.severity)}
        />
        <p class="text-xs text-slate-600">{friendlyAnomalyRuleKind(alert.ruleKind)}</p>
      </div>
      <div class="grid gap-1">
        <span class="text-xs text-slate-500">SLA 到期</span>
        <p class="text-lg font-semibold text-slate-900">
          {formatTaipeiDateTime(alert.slaDueAt)}
        </p>
        <StateTag
          label={alert.slaStatus === "BREACHED" ? "超時" : "進行中"}
          tone={slaStatusTone(alert.slaStatus)}
        />
      </div>
      <div class="grid gap-1">
        <span class="text-xs text-slate-500">目前負責人</span>
        <p class="text-sm font-mono text-slate-800">
          {alert.ownerActorId ? maskIdentifier(alert.ownerActorId, 6) : "（尚未指派）"}
        </p>
        <StateTag
          label={friendlyAnomalyStatus(alert.status)}
          tone={anomalyStatusTone(alert.status)}
        />
      </div>
      <div class="grid gap-1">
        <span class="text-xs text-slate-500">觀測值 vs 門檻</span>
        <p class="text-lg font-semibold text-slate-900">
          {alert.observedValue}
          <span class="text-sm font-normal text-slate-600">
            （{alert.thresholdComparator} {alert.thresholdValue}）
          </span>
        </p>
        <p class="text-xs text-slate-600">商家 {maskIdentifier(alert.vendorId, 6)}</p>
      </div>
    </div>

    <div class="mt-4 flex flex-wrap gap-2">
      <Button
        variant="primary"
        disabled={!canAcknowledge || submitting}
        onclick={() => void patchSimple("ACKNOWLEDGE")}
      >
        Acknowledge 確認
      </Button>
      <Button
        variant="secondary"
        disabled={!canStartRemediation || submitting}
        onclick={() => void patchSimple("START_REMEDIATION")}
      >
        Start Remediation 開始處理
      </Button>
      <Button
        variant="secondary"
        disabled={!canEscalate || submitting}
        onclick={() => void patchSimple("ESCALATE")}
      >
        Escalate 升級
      </Button>
      <Button
        variant="secondary"
        disabled={!canAssign || submitting}
        onclick={() => {
          actionError = null;
          assignOpen = true;
        }}
      >
        Assign 指派
      </Button>
      <Button
        variant="danger"
        disabled={!canClose || submitting}
        onclick={() => {
          closeError = null;
          closeOpen = true;
        }}
      >
        Close 結案
      </Button>
    </div>

    {#if actionError}
      <p class="mt-2 rounded-lg border border-rose-200 bg-rose-50 px-3 py-2 text-xs text-rose-900">
        {actionError}
      </p>
    {/if}

    <div class="mt-3">
      <FormField label="備註（選填，附在下一個動作上）">
        <textarea
          class="min-h-[64px] rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm"
          bind:value={note}
        ></textarea>
      </FormField>
    </div>

    <details class="mt-3 rounded-lg border border-slate-200 bg-slate-50/60 p-3 text-sm">
      <summary class="cursor-pointer font-semibold text-slate-800">技術細節</summary>
      <dl class="mt-3 grid gap-2 text-sm text-slate-700 md:grid-cols-3">
        <div>
          <dt class="text-xs text-slate-500">alertId</dt>
          <dd class="font-mono text-xs">{alert.alertId}</dd>
        </div>
        <div>
          <dt class="text-xs text-slate-500">ruleKind（原）</dt>
          <dd class="font-mono text-xs">{alert.ruleKind}</dd>
        </div>
        <div>
          <dt class="text-xs text-slate-500">severity（原）</dt>
          <dd class="font-mono text-xs">{alert.severity}</dd>
        </div>
        <div>
          <dt class="text-xs text-slate-500">vendor</dt>
          <dd class="font-mono text-xs">{alert.vendorId}</dd>
        </div>
        <div>
          <dt class="text-xs text-slate-500">owner</dt>
          <dd class="font-mono text-xs">{alert.ownerActorId ?? "-"}</dd>
        </div>
        <div>
          <dt class="text-xs text-slate-500">開啟時間</dt>
          <dd>{formatTaipeiDateTime(alert.openedAt)}</dd>
        </div>
        <div>
          <dt class="text-xs text-slate-500">governance</dt>
          <dd>{alert.governanceIssueId}</dd>
        </div>
        <div>
          <dt class="text-xs text-slate-500">ticketRef</dt>
          <dd class="font-mono text-xs">{alert.ticketReference ?? "-"}</dd>
        </div>
      </dl>
    </details>
  </Card>

  <Card title="轉換歷程">
    {#if alert.trace.length === 0}
      <EmptyState title="尚無歷程" description="任何狀態轉換後會在此 append-only 記錄。" />
    {:else}
      <ol class="grid gap-2">
        {#each alert.trace as entry}
          <li class="rounded-lg border border-slate-200 bg-white p-3">
            <div class="flex items-baseline justify-between gap-2">
              <span class="text-sm font-semibold text-slate-800">{entry.eventType}</span>
              <span class="text-xs text-slate-500">{formatTaipeiDateTime(entry.occurredAt)}</span>
            </div>
            <p class="mt-1 font-mono text-[11px] text-slate-500">{entry.actorId}</p>
            {#if entry.note}
              <p class="mt-1 text-sm text-slate-700">{entry.note}</p>
            {/if}
          </li>
        {/each}
      </ol>
    {/if}
  </Card>

  <Button variant="ghost" href="/admin/anomalies">返回列表</Button>
{/if}

<!-- Assign modal -->
{#if assignOpen}
  <div
    class="fixed inset-0 z-50 flex items-center justify-center bg-slate-900/40 px-4"
    role="dialog"
    aria-modal="true"
  >
    <div class="w-full max-w-md rounded-2xl bg-white p-5 shadow-xl">
      <h3 class="text-lg font-semibold text-slate-900">指派負責人</h3>
      <p class="mt-1 text-sm text-slate-600">請輸入新負責人的 ActorId。</p>
      <div class="mt-4 grid gap-3">
        <FormField label="ownerActorId" required>
          <input
            class="rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm"
            bind:value={assignOwnerId}
            placeholder="例：actor-admin-001"
          />
        </FormField>
        {#if actionError}
          <p class="rounded-lg border border-rose-200 bg-rose-50 px-3 py-2 text-xs text-rose-900">
            {actionError}
          </p>
        {/if}
      </div>
      <div class="mt-5 flex justify-end gap-2">
        <Button variant="ghost" onclick={() => (assignOpen = false)} disabled={submitting}>
          取消
        </Button>
        <Button variant="primary" loading={submitting} onclick={() => void submitAssign()}>
          指派
        </Button>
      </div>
    </div>
  </div>
{/if}

<!-- Close modal with ISS-007 sign-off checkbox + evidence chip input -->
{#if closeOpen}
  <div
    class="fixed inset-0 z-50 flex items-center justify-center bg-slate-900/40 px-4"
    role="dialog"
    aria-modal="true"
  >
    <div class="w-full max-w-2xl rounded-2xl bg-white p-5 shadow-xl">
      <h3 class="text-lg font-semibold text-slate-900">結案告警</h3>
      <p class="mt-1 text-sm text-slate-600">
        結案會寫入 append-only 歷程並鎖定狀態，無法復原。
      </p>
      <div class="mt-4 grid gap-3">
        <label class="flex items-start gap-2 rounded-lg border border-slate-200 bg-slate-50 p-3 text-sm text-slate-800">
          <input type="checkbox" class="mt-0.5" bind:checked={closeSignedOff} />
          <span>
            我已在 Governance Issue
            <strong class="font-semibold">{ANOMALY_RELEASE_SIGN_OFF_ISSUE_ID}</strong>
            上取得告警結案簽核。
          </span>
        </label>
        <FormField label="結案備註" required>
          <textarea
            class="min-h-[96px] rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm"
            bind:value={closureNote}
            placeholder="說明告警已被確實修復或不再適用的原因。"
          ></textarea>
        </FormField>
        <FormField label="結案證據" hint="按 Enter 加入；可放 audit:// 連結或工單代號。" required>
          <ChipInput
            bind:values={closureEvidenceRefs}
            placeholder="例：audit://evidence/123"
          />
        </FormField>
        <FormField label="工單代號（選填）">
          <input
            class="rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm"
            bind:value={ticketReference}
            placeholder="例：JIRA-4521"
          />
        </FormField>
        {#if closeError}
          <p class="rounded-lg border border-rose-200 bg-rose-50 px-3 py-2 text-xs text-rose-900">
            {closeError}
          </p>
        {/if}
      </div>
      <div class="mt-5 flex justify-end gap-2">
        <Button variant="ghost" onclick={() => (closeOpen = false)} disabled={submitting}>
          取消
        </Button>
        <Button variant="danger" loading={submitting} onclick={() => void submitClose()}>
          確定結案
        </Button>
      </div>
    </div>
  </div>
{/if}
