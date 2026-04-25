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
  import { apiClient } from "$lib/platform/api";
  import {
    configureAdminApi,
    describeApiError,
    readRecentSettlements,
    PAYROLL_DISPUTE_OPERATION_OPTIONS,
    type PayrollDisputeOperation,
    type PayrollDisputeView
  } from "$lib/admin/api";
  import {
    friendlyDisputeStatus,
    disputeStatusTone,
    maskIdentifier
  } from "$lib/platform/labels";
  import { formatTaipeiDateTime } from "$lib/admin/portal";

  import type { PageData } from "./$types";

  let { data }: { data: PageData } = $props();

  const disputeId = $derived(data.disputeId);

  let heroContext = $state<
    | {
        cycleKey: string;
        employeeMasked: string;
        closedAt: string;
        status: string;
      }
    | null
  >(null);

  onMount(() => {
    const recent = readRecentSettlements();
    for (const entry of recent) {
      const match = entry.exceptions.find((e) => e.disputeId === disputeId);
      if (match) {
        heroContext = {
          cycleKey: entry.cycleKey,
          employeeMasked: maskIdentifier(match.employeeActorId, 6),
          closedAt: new Date(entry.closedAtEpochMs).toISOString(),
          status: match.status
        };
        return;
      }
    }
    heroContext = null;
  });

  let activeTab = $state<PayrollDisputeOperation>("ASSIGN_OWNER");
  let ownerActorId = $state("");
  let note = $state("");
  let refundAmountMinor = $state("");
  let submitting = $state(false);
  let formError = $state<string | null>(null);
  let result = $state<PayrollDisputeView | null>(null);

  async function submit(event: SubmitEvent) {
    event.preventDefault();
    formError = null;
    if (disputeId.trim().length === 0) {
      formError = "disputeId 為必填。";
      return;
    }

    submitting = true;
    try {
      configureAdminApi(data.auth.apiBearerToken);
      if (activeTab === "ASSIGN_OWNER") {
        const owner = ownerActorId.trim();
        if (owner.length === 0) {
          formError = "ASSIGN_OWNER 需要 ownerActorId。";
          submitting = false;
          return;
        }
        result = await apiClient.admin.updateAdminPayrollDispute(disputeId, {
          operation: "ASSIGN_OWNER",
          ownerActorId: owner,
          note: note.trim().length === 0 ? undefined : note.trim()
        });
      } else if (activeTab === "RESOLVE_REFUND") {
        const trimmedNote = note.trim();
        if (trimmedNote.length < 5) {
          formError = "RESOLVE_REFUND 需要 note ≥ 5 字。";
          submitting = false;
          return;
        }
        let refund: number | undefined;
        if (refundAmountMinor.trim().length > 0) {
          const parsed = Number(refundAmountMinor);
          if (!Number.isInteger(parsed) || parsed < 1) {
            formError = "refundAmountMinor 必須是 ≥ 1 的整數。";
            submitting = false;
            return;
          }
          refund = parsed;
        }
        result = await apiClient.admin.updateAdminPayrollDispute(disputeId, {
          operation: "RESOLVE_REFUND",
          note: trimmedNote,
          refundAmountMinor: refund
        });
      } else {
        const trimmedNote = note.trim();
        if (trimmedNote.length < 5) {
          formError = "RESOLVE_REJECTED 需要 note ≥ 5 字。";
          submitting = false;
          return;
        }
        result = await apiClient.admin.updateAdminPayrollDispute(disputeId, {
          operation: "RESOLVE_REJECTED",
          note: trimmedNote
        });
      }
      toasts.success(`爭議 ${disputeId} 已更新為 ${result.status}。`);
    } catch (error) {
      const message = describeApiError(error);
      formError = message;
      toasts.error(message);
    } finally {
      submitting = false;
    }
  }
</script>

<PageHeader
  eyebrow="月結爭議"
  title={`爭議 ${maskIdentifier(disputeId, 6)}`}
  description="對員工薪資申訴指派負責人、核准退款或駁回。"
  breadcrumbs={data.breadcrumbs}
/>

<Card title="爭議全景" description="由本瀏覽器最近一次關帳紀錄推測；若為他人關帳，請以權威系統記錄為準。">
  {#if heroContext}
    <dl class="grid gap-3 text-sm text-slate-700 md:grid-cols-4">
      <div>
        <dt class="text-xs text-slate-500">disputeId</dt>
        <dd class="font-mono text-xs" title={disputeId}>{maskIdentifier(disputeId, 6)}</dd>
      </div>
      <div>
        <dt class="text-xs text-slate-500">週期</dt>
        <dd class="font-medium">{heroContext.cycleKey}</dd>
      </div>
      <div>
        <dt class="text-xs text-slate-500">員工（遮罩）</dt>
        <dd class="font-mono text-xs">{heroContext.employeeMasked}</dd>
      </div>
      <div>
        <dt class="text-xs text-slate-500">最後同步</dt>
        <dd>{formatTaipeiDateTime(heroContext.closedAt)}</dd>
      </div>
    </dl>
  {:else}
    <dl class="grid gap-3 text-sm text-slate-700 md:grid-cols-3">
      <div>
        <dt class="text-xs text-slate-500">disputeId</dt>
        <dd class="font-mono text-xs" title={disputeId}>{maskIdentifier(disputeId, 6)}</dd>
      </div>
      <div class="md:col-span-2">
        <dt class="text-xs text-slate-500">額外資訊</dt>
        <dd class="text-xs text-slate-600">
          本爭議未出現在瀏覽器本地最近關帳紀錄。請直接執行下方動作，系統會在送出後回傳最新狀態。
        </dd>
      </div>
    </dl>
  {/if}
</Card>

<Card>
  <div class="flex flex-wrap gap-1 border-b border-slate-200 pb-2">
    {#each PAYROLL_DISPUTE_OPERATION_OPTIONS as op}
      <button
        type="button"
        class={`rounded-t-lg px-3 py-2 text-sm font-medium transition ${activeTab === op ? "bg-cyan-50 text-cyan-800" : "text-slate-600 hover:text-slate-900"}`}
        onclick={() => (activeTab = op)}
      >
        {op}
      </button>
    {/each}
  </div>

  <form class="grid gap-3" onsubmit={submit}>
    {#if activeTab === "ASSIGN_OWNER"}
      <FormField label="ownerActorId" required>
        <input
          class="rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm"
          bind:value={ownerActorId}
        />
      </FormField>
      <FormField label="note（選填）">
        <textarea
          class="min-h-[96px] rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm"
          bind:value={note}
        ></textarea>
      </FormField>
    {:else if activeTab === "RESOLVE_REFUND"}
      <FormField label="note（≥ 5 字）" required>
        <textarea
          class="min-h-[96px] rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm"
          bind:value={note}
        ></textarea>
      </FormField>
      <FormField label="refundAmountMinor（選填，≥ 1）">
        <input
          type="number"
          min="1"
          class="rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm"
          bind:value={refundAmountMinor}
        />
      </FormField>
    {:else}
      <FormField label="note（≥ 5 字）" required>
        <textarea
          class="min-h-[96px] rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm"
          bind:value={note}
        ></textarea>
      </FormField>
    {/if}

    {#if formError}
      <p class="rounded-lg border border-rose-200 bg-rose-50 px-3 py-2 text-xs text-rose-900">
        {formError}
      </p>
    {/if}

    <div class="flex gap-2">
      <Button type="submit" variant="primary" loading={submitting}>送出 {activeTab}</Button>
      <Button variant="ghost" href="/admin/settlement/disputes">返回爭議列表</Button>
    </div>
  </form>
</Card>

{#if result}
  <Card title="最新狀態" variant="success">
    <dl class="grid gap-2 text-sm text-slate-700 md:grid-cols-3">
      <div>
        <dt class="text-xs text-slate-500">disputeId</dt>
        <dd class="font-mono text-xs">{result.disputeId}</dd>
      </div>
      <div>
        <dt class="text-xs text-slate-500">狀態</dt>
        <dd>
          <StateTag
            label={friendlyDisputeStatus(result.status)}
            tone={disputeStatusTone(result.status)}
          />
        </dd>
      </div>
      <div>
        <dt class="text-xs text-slate-500">負責人</dt>
        <dd class="font-mono text-xs">{result.ownerActorId}</dd>
      </div>
    </dl>
  </Card>
{/if}
