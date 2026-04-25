<script lang="ts">
  import { onMount } from "svelte";

  import {
    PageHeader,
    Card,
    Button,
    FormField,
    StateTag,
    toasts
  } from "$lib/components/ui";
  import { formatTaipeiDateTime } from "$lib/admin/portal";
  import { apiClient } from "$lib/platform/api";
  import { configureAdminApi, describeApiError } from "$lib/admin/api";
  import { maskIdentifier } from "$lib/platform/labels";

  import type { PageData } from "./$types";

  let { data }: { data: PageData } = $props();

  const cycleKey = $derived(data.cycleKey);

  type CycleSummary = Awaited<
    ReturnType<typeof apiClient.admin.listPayrollSettlementCycles>
  >["items"][number];

  let summary = $state<CycleSummary | null>(null);
  let loadError = $state<string | null>(null);
  let lockReason = $state("");
  let unlockReason = $state("");
  let locking = $state(false);
  let unlocking = $state(false);
  let lockState = $state<Awaited<
    ReturnType<typeof apiClient.admin.lockPayrollSettlementCycle>
  >["settlementCycle"] | null>(null);

  onMount(() => {
    void loadSummary();
  });

  async function loadSummary() {
    loadError = null;
    try {
      configureAdminApi(data.auth.apiBearerToken);
      // Walk pages until we find the matching cycle. Bounds at 5 pages × 200 = 1000 cycles;
      // anything beyond that is very old history and warrants a proper cycle-get API.
      let page = 1;
      const pageSize = 200;
      for (; page <= 5; page += 1) {
        const response = await apiClient.admin.listPayrollSettlementCycles(page, pageSize);
        const hit = response.items.find((entry) => entry.cycleKey === cycleKey);
        if (hit) {
          summary = hit;
          return;
        }
        if (response.items.length < pageSize) break;
      }
      summary = null;
    } catch (error) {
      loadError = describeApiError(error);
    }
  }

  async function lockCycle(event: SubmitEvent) {
    event.preventDefault();
    const reason = lockReason.trim();
    if (reason.length === 0) {
      toasts.error("lock reason 為必填。");
      return;
    }
    locking = true;
    try {
      configureAdminApi(data.auth.apiBearerToken);
      const response = await apiClient.admin.lockPayrollSettlementCycle(cycleKey, { reason });
      lockState = response.settlementCycle;
      toasts.success(`週期 ${cycleKey} 已鎖定。`);
      lockReason = "";
    } catch (error) {
      toasts.error(describeApiError(error));
    } finally {
      locking = false;
    }
  }

  async function unlockCycle(event: SubmitEvent) {
    event.preventDefault();
    const reason = unlockReason.trim();
    if (reason.length === 0) {
      toasts.error("unlock reason 為必填。");
      return;
    }
    unlocking = true;
    try {
      configureAdminApi(data.auth.apiBearerToken);
      const response = await apiClient.admin.unlockPayrollSettlementCycle(cycleKey, { reason });
      lockState = response.settlementCycle;
      toasts.success(`週期 ${cycleKey} 已解鎖。`);
      unlockReason = "";
    } catch (error) {
      toasts.error(describeApiError(error));
    } finally {
      unlocking = false;
    }
  }
</script>

<PageHeader
  eyebrow="月結作業"
  title={`週期 ${cycleKey}`}
  description="鎖定 / 解鎖月結週期。鎖定後任何寫入都會被拒絕。"
  breadcrumbs={data.breadcrumbs}
/>

<Card title="週期資訊">
  {#if loadError}
    <p class="text-sm text-rose-700">{loadError}</p>
  {:else if summary}
    <dl class="grid gap-2 text-sm text-slate-700 md:grid-cols-4">
      <div>
        <dt class="text-xs text-slate-500">cycleKey</dt>
        <dd class="font-medium">{summary.cycleKey}</dd>
      </div>
      <div>
        <dt class="text-xs text-slate-500">薪資月份</dt>
        <dd class="font-mono tabular-nums">{summary.payPeriod}</dd>
      </div>
      <div>
        <dt class="text-xs text-slate-500">batchId</dt>
        <dd class="font-mono text-xs" title={summary.batchId}>{maskIdentifier(summary.batchId, 10)}</dd>
      </div>
      <div>
        <dt class="text-xs text-slate-500">鎖定狀態</dt>
        <dd>
          <StateTag
            label={summary.lockState === "LOCKED" ? "已鎖定" : "未鎖定"}
            tone={summary.lockState === "LOCKED" ? "danger" : "success"}
          />
        </dd>
      </div>
      <div>
        <dt class="text-xs text-slate-500">關帳時間</dt>
        <dd>{formatTaipeiDateTime(summary.generatedAt)}</dd>
      </div>
      <div>
        <dt class="text-xs text-slate-500">總筆數</dt>
        <dd class="font-medium">{summary.totalRecords}</dd>
      </div>
      <div>
        <dt class="text-xs text-slate-500">爭議</dt>
        <dd class="font-medium text-amber-700">{summary.disputedRecords}</dd>
      </div>
      <div>
        <dt class="text-xs text-slate-500">扣款失敗</dt>
        <dd class="font-medium text-rose-700">{summary.deductionFailedRecords}</dd>
      </div>
    </dl>
  {:else}
    <p class="text-sm text-slate-600">
      找不到週期 <span class="font-mono">{cycleKey}</span>（已查詢最近 1000 筆）。仍可透過下方表單嘗試鎖定 / 解鎖 — 後端會以真實狀態回應。
    </p>
  {/if}
</Card>

{#if lockState}
  <Card title="最近一次鎖定狀態" variant="info">
    <dl class="grid gap-2 text-sm text-slate-700 md:grid-cols-4">
      <div>
        <dt class="text-xs text-slate-500">狀態</dt>
        <dd>
          <StateTag
            label={lockState.lockState}
            tone={lockState.lockState === "LOCKED" ? "danger" : "success"}
          />
        </dd>
      </div>
      <div>
        <dt class="text-xs text-slate-500">變更時間</dt>
        <dd>{formatTaipeiDateTime(lockState.changedAt)}</dd>
      </div>
      <div>
        <dt class="text-xs text-slate-500">執行人</dt>
        <dd class="font-mono text-xs">{lockState.actorId}</dd>
      </div>
      <div>
        <dt class="text-xs text-slate-500">reason</dt>
        <dd>{lockState.reason}</dd>
      </div>
    </dl>
  </Card>
{/if}

<div class="grid gap-3 md:grid-cols-2">
  <Card title="鎖定週期" description="鎖定後，週期不可再被修改。">
    <form class="grid gap-3" onsubmit={lockCycle}>
      <FormField label="lock reason" required>
        <textarea
          class="min-h-[96px] rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm"
          bind:value={lockReason}
          placeholder="例：已提交給 HR，鎖定避免再變更"
        ></textarea>
      </FormField>
      <Button type="submit" variant="danger" loading={locking}>鎖定週期</Button>
    </form>
  </Card>

  <Card title="解鎖週期" description="僅在授權重新計算時使用。">
    <form class="grid gap-3" onsubmit={unlockCycle}>
      <FormField label="unlock reason" required>
        <textarea
          class="min-h-[96px] rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm"
          bind:value={unlockReason}
          placeholder="例：需要重新計算修訂的退款"
        ></textarea>
      </FormField>
      <Button type="submit" variant="primary" loading={unlocking}>解鎖週期</Button>
    </form>
  </Card>
</div>
