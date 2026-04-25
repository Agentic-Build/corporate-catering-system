<script lang="ts">
  import { onMount } from "svelte";

  import {
    PageHeader,
    Card,
    Button,
    StateTag,
    MoneyAmount,
    EmptyState
  } from "$lib/components/ui";
  import PlantGuard from "$lib/components/employee/plant-guard.svelte";
  import {
    configureEmployeeApi,
    describeApiError,
    findEmployeeOrderById,
    loadMenuItemNameMap,
    summarizeOrderLineItems,
    type EmployeeOrderView,
    type PayrollLedgerView
  } from "$lib/employee/api";
  import {
    friendlyLedgerKind,
    ledgerKindIsDebit,
    friendlyDisputeStatus,
    disputeStatusTone,
    friendlyOrderEvent,
    maskIdentifier
  } from "$lib/platform/labels";
  import { apiClient } from "$lib/platform/api";

  import type { PageData } from "./$types";

  let { data }: { data: PageData } = $props();

  const actor = $derived(data.actor);
  const plantId = $derived(actor?.scope.plantIds[0] ?? null);
  const role = $derived(actor?.role ?? null);
  const orderId = $derived(data.orderId);

  let order = $state<EmployeeOrderView | null>(null);
  let menuItemNameMap = $state<Record<string, string>>({});
  let ledger = $state<PayrollLedgerView | null>(null);
  let loading = $state(true);
  let loadError = $state<string | null>(null);

  onMount(() => {
    if (role === "employee" && plantId) {
      void loadAll(plantId, data.auth.apiBearerToken);
    }
  });

  async function loadAll(resolvedPlantId: string, bearerToken: string | null) {
    loading = true;
    loadError = null;
    try {
      configureEmployeeApi(resolvedPlantId, bearerToken);
      const [ledgerResp, orderResp] = await Promise.all([
        apiClient.employee.getEmployeeOrderPayrollLedger(orderId),
        findEmployeeOrderById(orderId, { plantId: resolvedPlantId }).catch(() => null)
      ]);
      ledger = ledgerResp;
      order = orderResp;
      if (orderResp) {
        const ids = orderResp.lineItems.map((li) => li.menuItemId);
        menuItemNameMap = await loadMenuItemNameMap(resolvedPlantId, ids);
      }
    } catch (error) {
      loadError = describeApiError(error);
    } finally {
      loading = false;
    }
  }

  const originalMinor = $derived(order?.total.amountMinor ?? 0);
  const refundMinor = $derived.by(() => {
    if (!ledger) return 0;
    let refund = 0;
    for (const entry of ledger.ledgerEntries) {
      if (!ledgerKindIsDebit(entry.kind)) {
        refund += entry.amount.amountMinor;
      }
    }
    return refund;
  });
</script>

<PlantGuard role={role} plantId={plantId}>
  <PageHeader
    eyebrow={`扣款明細 ${maskIdentifier(orderId)}`}
    title="薪資流水"
    description="此訂單的薪資扣款流水、退款事件與申訴追蹤。"
    breadcrumbs={data.breadcrumbs}
  >
    {#snippet actions()}
      <Button href="/employee/wallet" variant="ghost">返回列表</Button>
      <Button
        href={`/employee/orders/${orderId}/dispute`}
        variant="secondary"
      >
        提交新申訴
      </Button>
    {/snippet}
  </PageHeader>

  {#if loading}
    <Card title="同步中">
      <p class="text-sm text-slate-600">載入扣款明細中...</p>
    </Card>
  {:else if loadError}
    <Card variant="danger" title="載入失敗">
      <p class="text-sm text-rose-900">{loadError}</p>
    </Card>
  {:else if !ledger}
    <Card title="沒有明細">
      <EmptyState title="尚無流水" description="訂單結算後會自動產生記錄。" />
    </Card>
  {:else}
    <Card title="訂單概要">
      <div class="grid gap-2 md:grid-cols-3">
        <div>
          <p class="text-xs text-slate-500">配送日</p>
          <p class="text-sm font-semibold text-slate-900">{ledger.deliveryDate}</p>
        </div>
        <div class="md:col-span-2">
          <p class="text-xs text-slate-500">品項摘要</p>
          <p class="text-sm text-slate-700">
            {order ? summarizeOrderLineItems(order.lineItems, menuItemNameMap) : "—"}
          </p>
        </div>
      </div>
      <div class="mt-3 grid gap-1 rounded-lg border border-slate-200 bg-white p-3 text-sm">
        <div class="flex items-center justify-between">
          <span class="text-slate-600">原訂單金額</span>
          <MoneyAmount amountMinor={originalMinor} currency={ledger.currency} />
        </div>
        <div class="flex items-center justify-between">
          <span class="text-slate-600">扣除退款</span>
          <span class="inline-flex items-center gap-1">
            <span class="text-emerald-700">−</span>
            <MoneyAmount amountMinor={refundMinor} currency={ledger.currency} />
          </span>
        </div>
        <div class="flex items-center justify-between border-t border-slate-200 pt-1 font-semibold">
          <span class="text-slate-900">淨扣款</span>
          <MoneyAmount amountMinor={ledger.netAmountMinor} currency={ledger.currency} />
        </div>
      </div>
      <p class="mt-2 text-xs text-slate-500">訂單編號：{ledger.orderId}</p>
    </Card>

    <Card title="薪資流水" description="每筆來源事件都會寫入不可變流水。">
      {#if ledger.ledgerEntries.length === 0}
        <EmptyState title="尚無流水" description="此訂單尚未觸發任何扣款或退款。" />
      {:else}
        <div class="grid gap-2">
          {#each ledger.ledgerEntries as entry (entry.ledgerEntryId)}
            {@const isDebit = ledgerKindIsDebit(entry.kind)}
            <article class="grid gap-1 rounded-lg border border-slate-200 bg-white p-3 shadow-sm">
              <div class="flex flex-wrap items-center justify-between gap-2">
                <p class="text-sm font-semibold text-slate-900">{friendlyLedgerKind(entry.kind)}</p>
                <span class={`inline-flex items-center gap-1 tabular-nums font-semibold ${isDebit ? "text-rose-700" : "text-emerald-700"}`}>
                  <span>{isDebit ? "−" : "+"}</span>
                  <MoneyAmount
                    amountMinor={entry.amount.amountMinor}
                    currency={entry.amount.currency}
                  />
                </span>
              </div>
              <p class="text-xs text-slate-600">時間：{entry.occurredAt}</p>
              <p class="text-xs text-slate-500">
                來源：{friendlyOrderEvent(entry.sourceEventKind)}（{maskIdentifier(entry.sourceEventReference)}）
              </p>
            </article>
          {/each}
        </div>
      {/if}
    </Card>

    <Card title="申訴追蹤">
      {#if ledger.disputes.length === 0}
        <EmptyState
          title="尚無申訴"
          description="若此訂單發生扣款爭議，可從訂單詳情提交申訴。"
        >
          {#snippet actions()}
            <Button href={`/employee/orders/${orderId}/dispute`} variant="primary">
              提交申訴
            </Button>
          {/snippet}
        </EmptyState>
      {:else}
        <div class="grid gap-2">
          {#each ledger.disputes as dispute (dispute.disputeId)}
            <article class="grid gap-2 rounded-lg border border-slate-200 bg-white p-3 shadow-sm">
              <div class="flex flex-wrap items-center justify-between gap-2">
                <p class="text-sm font-semibold text-slate-900">申訴 {maskIdentifier(dispute.disputeId)}</p>
                <StateTag
                  label={friendlyDisputeStatus(dispute.status)}
                  tone={disputeStatusTone(dispute.status)}
                />
              </div>
              <p class="text-xs text-slate-600">
                負責人：{maskIdentifier(dispute.ownerActorId)} · 開立：{dispute.openedAt} · 更新：{dispute.updatedAt}
              </p>
              {#if dispute.trace.length > 0}
                <div class="grid gap-1">
                  {#each dispute.trace as event, index (`${dispute.disputeId}:${index}`)}
                    <div class="rounded border border-slate-200 bg-slate-50 px-2 py-1 text-[11px] text-slate-700">
                      <p>
                        {event.occurredAt} · {maskIdentifier(event.actorId)} · {friendlyDisputeStatus(event.status)}
                      </p>
                      {#if event.note}
                        <p class="text-slate-600">備註：{event.note}</p>
                      {/if}
                    </div>
                  {/each}
                </div>
              {/if}
            </article>
          {/each}
        </div>
      {/if}
    </Card>
  {/if}
</PlantGuard>
