<script lang="ts">
  import { PageHeader, Card, Button, FormField, EmptyState, toasts } from "$lib/components/ui";
  import {
    parseOptionalEpochDay,
    parseOptionalMinuteOfDay,
    parseOptionalNumber
  } from "$lib/admin/portal";
  import { apiClient } from "$lib/platform/api";
  import {
    configureAdminApi,
    describeApiError,
    normalizeOptional
  } from "$lib/admin/api";

  import type { PageData } from "./$types";

  let { data }: { data: PageData } = $props();

  const defaultOwnerId = $derived(data.actor?.id ?? "");

  let draft = $state({
    vendorId: "",
    defaultOwnerActorId: "",
    daysUntilExpiry: "",
    onTimeRate: "",
    satisfactionScore: "",
    complaintCount: "",
    observedAtEpochDay: "",
    observedAtMinuteOfDay: ""
  });
  let submitting = $state(false);
  let formError = $state<string | null>(null);
  let result = $state<Awaited<
    ReturnType<typeof apiClient.admin.evaluateAnomalyAlerts>
  > | null>(null);

  $effect(() => {
    if (draft.defaultOwnerActorId.trim().length === 0 && defaultOwnerId) {
      draft.defaultOwnerActorId = defaultOwnerId;
    }
  });

  async function submit(event: SubmitEvent) {
    event.preventDefault();
    formError = null;

    const vendorId = draft.vendorId.trim();
    if (vendorId.length === 0) {
      formError = "vendorId 為必填。";
      return;
    }

    submitting = true;
    try {
      configureAdminApi(data.auth.apiBearerToken);
      result = await apiClient.admin.evaluateAnomalyAlerts({
        vendorId,
        defaultOwnerActorId: normalizeOptional(draft.defaultOwnerActorId),
        daysUntilExpiry: parseOptionalNumber(draft.daysUntilExpiry),
        onTimeRate: parseOptionalNumber(draft.onTimeRate),
        satisfactionScore: parseOptionalNumber(draft.satisfactionScore),
        complaintCount: parseOptionalNumber(draft.complaintCount),
        observedAtEpochDay: parseOptionalEpochDay(draft.observedAtEpochDay),
        observedAtMinuteOfDay: parseOptionalMinuteOfDay(draft.observedAtMinuteOfDay)
      });
      toasts.success(
        `評估完成，觸發 ${result.triggeredAlerts.length} 筆告警。`
      );
    } catch (error) {
      const message = error instanceof Error ? error.message : describeApiError(error);
      formError = message;
      toasts.error(message);
    } finally {
      submitting = false;
    }
  }
</script>

<PageHeader
  eyebrow="異常治理"
  title="手動評估異常規則"
  description="對特定商家提供測試數據，觸發規則評估。"
  breadcrumbs={data.breadcrumbs}
/>

<Card title="評估參數">
  <form class="grid gap-3" onsubmit={submit}>
    <div class="grid gap-3 md:grid-cols-2">
      <FormField label="vendorId" required>
        <input
          class="rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm"
          bind:value={draft.vendorId}
        />
      </FormField>
      <FormField label="defaultOwnerActorId">
        <input
          class="rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm"
          bind:value={draft.defaultOwnerActorId}
        />
      </FormField>
      <FormField label="daysUntilExpiry">
        <input
          type="number"
          class="rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm"
          bind:value={draft.daysUntilExpiry}
        />
      </FormField>
      <FormField label="onTimeRate (0..1)">
        <input
          type="number"
          step="0.01"
          min="0"
          max="1"
          class="rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm"
          bind:value={draft.onTimeRate}
        />
      </FormField>
      <FormField label="satisfactionScore (0..1)">
        <input
          type="number"
          step="0.01"
          min="0"
          max="1"
          class="rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm"
          bind:value={draft.satisfactionScore}
        />
      </FormField>
      <FormField label="complaintCount">
        <input
          type="number"
          min="0"
          class="rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm"
          bind:value={draft.complaintCount}
        />
      </FormField>
      <FormField label="observedAtEpochDay">
        <input
          type="number"
          min="1"
          class="rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm"
          bind:value={draft.observedAtEpochDay}
        />
      </FormField>
      <FormField label="observedAtMinuteOfDay (0..1439)">
        <input
          type="number"
          min="0"
          max="1439"
          class="rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm"
          bind:value={draft.observedAtMinuteOfDay}
        />
      </FormField>
    </div>

    {#if formError}
      <p class="rounded-lg border border-rose-200 bg-rose-50 px-3 py-2 text-xs text-rose-900">
        {formError}
      </p>
    {/if}

    <div class="flex gap-2">
      <Button type="submit" variant="primary" loading={submitting}>送出評估</Button>
      <Button variant="ghost" href="/admin/anomalies">返回列表</Button>
    </div>
  </form>
</Card>

{#if result}
  <Card title="觸發告警" description={`共 ${result.triggeredAlerts.length} 筆`}>
    {#if result.triggeredAlerts.length === 0}
      <EmptyState title="此次評估沒有觸發任何規則" description="可嘗試不同輸入或調整規則閾值。" />
    {:else}
      <div class="grid gap-2">
        {#each result.triggeredAlerts as alert (alert.alertId)}
          <article class="rounded-lg border border-amber-200 bg-amber-50/60 p-3 text-xs text-slate-800">
            <p class="font-semibold">{alert.ruleDisplayName}</p>
            <p>alertId: <a class="text-cyan-700 underline" href={`/admin/anomalies/${alert.alertId}`}>{alert.alertId}</a></p>
            <p>狀態：{alert.status}｜嚴重度：{alert.severity}</p>
            <p>
              觀測值 {alert.observedValue}（門檻 {alert.thresholdComparator} {alert.thresholdValue}）
            </p>
          </article>
        {/each}
      </div>
    {/if}
  </Card>
{/if}
