<script lang="ts">
  import {
    PageHeader,
    Card,
    Button,
    FormField,
    ConfirmDialog,
    StateTag,
    MoneyAmount,
    DataTable,
    EmptyState,
    toasts
  } from "$lib/components/ui";
  import Wizard from "$lib/components/ui/wizard.svelte";
  import {
    SETTLEMENT_RELEASE_SIGN_OFF_ISSUE_ID
  } from "$lib/admin/portal";
  import { apiClient } from "$lib/platform/api";
  import {
    configureAdminApi,
    describeApiError,
    rememberSettlementClose,
    mapSettlementExceptionClass,
    type SettlementPage,
    type SettlementRecord
  } from "$lib/admin/api";
  import { suggestSettlementCycleKeys } from "$lib/admin/cycle-suggestions";
  import { maskIdentifier } from "$lib/platform/labels";

  import type { PageData } from "./$types";

  let { data }: { data: PageData } = $props();

  const cycleSuggestions = suggestSettlementCycleKeys();

  const steps = [
    {
      key: "cycle",
      label: "1. 選週期",
      description: "挑選要關帳的月份。"
    },
    {
      key: "preview",
      label: "2. 預檢例外",
      description: "上一次關帳中仍有幾筆爭議或失敗？"
    },
    {
      key: "signoff",
      label: "3. ISS-003 簽核",
      description: "確認已完成結算發佈簽核。"
    },
    {
      key: "execute",
      label: "4. 執行",
      description: "建立 HR SFTP 批次並鎖定資料源。"
    }
  ];

  let currentStep = $state(0);

  let cycleKey = $state(cycleSuggestions[0]?.cycleKey ?? "");
  let signedOff = $state(false);
  let typedCycleKey = $state("");

  let confirmOpen = $state(false);
  let submitting = $state(false);
  let result = $state<SettlementPage | null>(null);
  let stepError = $state<string | null>(null);

  const canProceedFromCycle = $derived(cycleKey.trim().length > 0);
  const canProceedFromSignoff = $derived(signedOff);
  const canExecute = $derived(typedCycleKey.trim() === cycleKey.trim() && signedOff);

  const exceptionItems = $derived(
    (result?.items ?? []).filter((item) => mapSettlementExceptionClass(item) !== null)
  );

  const exceptionColumns = [
    { id: "employee", label: "員工", width: "40%" },
    { id: "status", label: "狀態", width: "20%" },
    { id: "deliveryDate", label: "出餐日", width: "20%" },
    { id: "dispute", label: "爭議狀態", width: "20%" }
  ];

  function goNext() {
    stepError = null;
    if (currentStep === 0 && !canProceedFromCycle) {
      stepError = "請先選擇要關帳的週期。";
      return;
    }
    if (currentStep === 2 && !canProceedFromSignoff) {
      stepError = "請勾選已取得 ISS-003 結算發佈簽核。";
      return;
    }
    currentStep = Math.min(currentStep + 1, steps.length - 1);
  }

  function goBack() {
    stepError = null;
    currentStep = Math.max(currentStep - 1, 0);
  }

  function requestConfirm() {
    if (!canExecute) {
      stepError = "請輸入正確的 cycleKey 並完成 ISS-003 簽核。";
      return;
    }
    confirmOpen = true;
  }

  async function executeClose() {
    confirmOpen = false;
    submitting = true;
    stepError = null;
    try {
      configureAdminApi(data.auth.apiBearerToken);
      const page = await apiClient.admin.closePayrollMonthlySettlement({
        cycleKey: cycleKey.trim(),
        issueChecklist: [SETTLEMENT_RELEASE_SIGN_OFF_ISSUE_ID],
        page: 1,
        pageSize: 200,
        sortBy: "deliveryDate",
        sortOrder: "desc"
      });
      result = page;
      rememberSettlementClose({
        cycleKey: page.exchangeBatch.cycleKey,
        batchId: page.exchangeBatch.batchId,
        closedAtEpochMs: Date.now(),
        totalRecords: page.exchangeBatch.reconciliation.totalRecords,
        disputedRecords: page.exchangeBatch.reconciliation.disputedRecords,
        deductionFailedRecords: page.exchangeBatch.reconciliation.deductionFailedRecords,
        refundedRecords: page.exchangeBatch.reconciliation.refundedRecords,
        exceptions: page.items
          .filter((item) => item.status !== "READY")
          .map((item) => ({
            employeeActorId: item.employeeActorCiphertext,
            status: item.status,
            amountMinor: 0,
            currency: "TWD"
          }))
      });
      toasts.success(`月結關帳完成，批次 ${page.exchangeBatch.batchId} 已建立。`);
    } catch (error) {
      const message = describeApiError(error);
      stepError = message;
      toasts.error(message);
    } finally {
      submitting = false;
    }
  }

  function formatDeliveryDate(record: SettlementRecord): string {
    return record.deliveryDate;
  }
</script>

<PageHeader
  eyebrow="月結作業"
  title="執行月結關帳"
  description="需 ISS-003 簽核；關帳後會建立 HR SFTP 批次並鎖定資料源。"
  breadcrumbs={data.breadcrumbs}
/>

{#if result}
  <Card title="關帳結果" variant="success">
    <dl class="grid gap-3 text-sm text-slate-700 md:grid-cols-4">
      <div>
        <dt class="text-xs text-slate-500">batchId</dt>
        <dd class="font-mono text-xs">{result.exchangeBatch.batchId}</dd>
      </div>
      <div>
        <dt class="text-xs text-slate-500">週期</dt>
        <dd class="font-medium">{result.exchangeBatch.cycleKey}</dd>
      </div>
      <div>
        <dt class="text-xs text-slate-500">HR 同步</dt>
        <dd>
          <StateTag
            label={result.exchangeBatch.hrApiSyncStatus}
            tone={result.exchangeBatch.hrApiSyncStatus === "SUCCEEDED"
              ? "success"
              : result.exchangeBatch.hrApiSyncStatus === "FAILED"
                ? "danger"
                : "neutral"}
          />
        </dd>
      </div>
      <div>
        <dt class="text-xs text-slate-500">本批總金額</dt>
        <dd>
          <MoneyAmount
            amountMinor={result.exchangeBatch.reconciliation.totalAmountMinor}
            currency="TWD"
          />
        </dd>
      </div>
      <div>
        <dt class="text-xs text-slate-500">總筆數</dt>
        <dd class="font-medium">{result.exchangeBatch.reconciliation.totalRecords}</dd>
      </div>
      <div>
        <dt class="text-xs text-slate-500">爭議</dt>
        <dd class="font-medium text-amber-700">
          {result.exchangeBatch.reconciliation.disputedRecords}
        </dd>
      </div>
      <div>
        <dt class="text-xs text-slate-500">扣款失敗</dt>
        <dd class="font-medium text-rose-700">
          {result.exchangeBatch.reconciliation.deductionFailedRecords}
        </dd>
      </div>
      <div>
        <dt class="text-xs text-slate-500">退款</dt>
        <dd class="font-medium">{result.exchangeBatch.reconciliation.refundedRecords}</dd>
      </div>
    </dl>
    <div class="flex flex-wrap gap-2">
      <Button
        variant="secondary"
        href={`/admin/settlement/cycles/${encodeURIComponent(result.exchangeBatch.cycleKey)}`}
      >
        前往週期詳情
      </Button>
      <Button variant="ghost" href="/admin/settlement/disputes">查看爭議列表</Button>
    </div>
  </Card>

  <Card
    title="本批例外"
    description={`共 ${exceptionItems.length} 筆例外（DISPUTED / DEDUCTION_FAILED / EMPLOYEE_TERMINATED / REFUNDED）。`}
  >
    {#if exceptionItems.length === 0}
      <EmptyState title="本批無例外" description="所有訂單皆成功扣款。" />
    {:else}
      <DataTable rows={exceptionItems} columns={exceptionColumns}>
        {#snippet row(record: SettlementRecord)}
          <tr class="hover:bg-slate-50">
            <td class="px-3 py-2 font-mono text-xs text-slate-700">
              {maskIdentifier(record.employeeActorCiphertext, 6)}
            </td>
            <td class="px-3 py-2">
              <StateTag label={record.status} tone="warning" />
            </td>
            <td class="px-3 py-2 text-xs text-slate-600">{formatDeliveryDate(record)}</td>
            <td class="px-3 py-2 text-xs text-slate-600">
              {record.disputeStatus ?? "-"}
            </td>
          </tr>
        {/snippet}
      </DataTable>
      <div class="flex justify-end">
        <Button variant="secondary" href="/admin/settlement/disputes">前往爭議列表</Button>
      </div>
    {/if}
  </Card>
{:else}
  <Wizard {steps} bind:currentStep>
    {#snippet children({ index })}
      <Card>
        {#if index === 0}
          <div class="grid gap-3">
            <h3 class="text-base font-semibold text-slate-900">選擇要關帳的週期</h3>
            <p class="text-sm text-slate-600">
              預設為前一個月（Taipei）。若已逾期，請往下選較早的月份。
            </p>
            <FormField label="cycleKey" required>
              <select
                class="rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm"
                bind:value={cycleKey}
              >
                {#each cycleSuggestions as suggestion}
                  <option value={suggestion.cycleKey}>{suggestion.label}</option>
                {/each}
              </select>
            </FormField>
            <p class="text-xs text-slate-500">
              若需關更早的週期，請聯絡系統管理員補 ISS-003 追溯簽核。
            </p>
          </div>
        {:else if index === 1}
          <div class="grid gap-3">
            <h3 class="text-base font-semibold text-slate-900">預檢例外</h3>
            <p class="text-sm text-slate-600">
              本系統目前僅提供「關帳後」才會回傳的例外清單。執行關帳前請先確認：
            </p>
            <ul class="grid gap-2 text-sm text-slate-700">
              <li class="rounded-lg border border-slate-200 bg-slate-50 px-3 py-2">
                所有未解決的爭議皆已於
                <a
                  class="font-semibold text-cyan-700 hover:text-cyan-900"
                  href="/admin/settlement/disputes"
                >
                  爭議列表
                </a>
                處理或獲得財務核可。
              </li>
              <li class="rounded-lg border border-slate-200 bg-slate-50 px-3 py-2">
                與 HR 確認本月無臨時離職需剔除。
              </li>
              <li class="rounded-lg border border-slate-200 bg-slate-50 px-3 py-2">
                已準備下一步的 ISS-003 結算發佈簽核文件。
              </li>
            </ul>
            <p class="text-xs text-slate-500">
              注意：本系統的合約 API 不支援關帳前 dry-run；實際金額、筆數、例外會在第 4 步關帳完成後呈現。
            </p>
          </div>
        {:else if index === 2}
          <div class="grid gap-3">
            <h3 class="text-base font-semibold text-slate-900">ISS-003 簽核確認</h3>
            <p class="text-sm text-slate-600">
              結算發佈需先於 Governance Issue {SETTLEMENT_RELEASE_SIGN_OFF_ISSUE_ID} 上取得簽核。
            </p>
            <label class="flex items-start gap-2 rounded-lg border border-slate-200 bg-slate-50 p-3 text-sm text-slate-800">
              <input type="checkbox" class="mt-0.5" bind:checked={signedOff} />
              <span>
                我已在 Governance Issue
                <strong class="font-semibold">{SETTLEMENT_RELEASE_SIGN_OFF_ISSUE_ID}</strong>
                上取得結算發佈簽核。
              </span>
            </label>
            {#if !signedOff}
              <p class="text-xs text-amber-700">勾選後才能進入下一步。</p>
            {/if}
          </div>
        {:else}
          <div class="grid gap-3">
            <h3 class="text-base font-semibold text-slate-900">執行關帳</h3>
            <p class="text-sm text-slate-600">
              此動作會建立不可回復的 HR SFTP 批次並鎖定
              <strong class="font-semibold text-slate-900">{cycleKey}</strong>
              資料源。請在下方重新輸入 cycleKey 以啟用「執行關帳」按鈕。
            </p>
            <FormField label={`再次輸入 ${cycleKey} 以確認`} required>
              <input
                class="rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm font-mono"
                bind:value={typedCycleKey}
                placeholder={cycleKey}
                autocomplete="off"
              />
            </FormField>
          </div>
        {/if}

        {#if stepError}
          <p class="rounded-lg border border-rose-200 bg-rose-50 px-3 py-2 text-xs text-rose-900">
            {stepError}
          </p>
        {/if}

        <div class="mt-2 flex flex-wrap items-center justify-between gap-2">
          <div class="flex gap-2">
            {#if index > 0}
              <Button variant="ghost" onclick={goBack} disabled={submitting}>上一步</Button>
            {/if}
            <Button variant="ghost" href="/admin/settlement" disabled={submitting}>取消</Button>
          </div>
          <div class="flex gap-2">
            {#if index < steps.length - 1}
              <Button variant="primary" onclick={goNext}>下一步</Button>
            {:else}
              <Button
                variant="danger"
                disabled={!canExecute || submitting}
                loading={submitting}
                onclick={requestConfirm}
              >
                執行關帳
              </Button>
            {/if}
          </div>
        </div>
      </Card>
    {/snippet}
  </Wizard>
{/if}

<ConfirmDialog
  open={confirmOpen}
  title={`確認對 ${cycleKey} 執行月結關帳`}
  description={`此動作會建立不可回復的 HR SFTP 批次並鎖定資料源，且已於 ${SETTLEMENT_RELEASE_SIGN_OFF_ISSUE_ID} 取得簽核；確認要繼續嗎？`}
  confirmLabel="確定關帳"
  cancelLabel="取消"
  tone="danger"
  loading={submitting}
  onConfirm={() => void executeClose()}
  onCancel={() => (confirmOpen = false)}
/>
