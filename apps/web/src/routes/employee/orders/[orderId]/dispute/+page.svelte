<script lang="ts">
  import { onMount } from "svelte";

  import {
    PageHeader,
    Card,
    Button,
    FormField,
    StateTag,
    MoneyAmount,
    EmptyState,
    toasts
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
    friendlyOrderStatus,
    orderStatusTone,
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
  let reason = $state("");
  let submitting = $state(false);

  const reasonTrimmed = $derived(reason.trim());
  const canSubmit = $derived(reasonTrimmed.length >= 5);

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
      const [foundOrder, ledgerResp] = await Promise.all([
        findEmployeeOrderById(orderId, { plantId: resolvedPlantId }),
        apiClient.employee.getEmployeeOrderPayrollLedger(orderId).catch(() => null)
      ]);
      order = foundOrder;
      ledger = ledgerResp;
      if (foundOrder) {
        const ids = foundOrder.lineItems.map((li) => li.menuItemId);
        menuItemNameMap = await loadMenuItemNameMap(resolvedPlantId, ids);
      }
    } catch (error) {
      loadError = describeApiError(error);
    } finally {
      loading = false;
    }
  }

  async function submit() {
    if (!canSubmit || submitting) return;
    submitting = true;
    try {
      await apiClient.employee.createEmployeeOrderDispute(orderId, {
        reason: reasonTrimmed
      });
      toasts.success(`已提交薪資申訴。`);
      reason = "";
      if (plantId) {
        await loadAll(plantId, data.auth.apiBearerToken);
      }
    } catch (error) {
      toasts.error(describeApiError(error));
    } finally {
      submitting = false;
    }
  }

  const orderSummary = $derived(
    order ? summarizeOrderLineItems(order.lineItems, menuItemNameMap) : "—"
  );
</script>

<PlantGuard role={role} plantId={plantId}>
  <PageHeader
    eyebrow={`提交申訴 ${maskIdentifier(orderId)}`}
    title="申訴扣款錯誤"
    description="若此訂單有扣款錯誤或異議，請附上說明。提交後福委會將追蹤處理。"
    breadcrumbs={data.breadcrumbs}
  >
    {#snippet actions()}
      <Button href={`/employee/orders/${orderId}`} variant="ghost">返回訂單</Button>
    {/snippet}
  </PageHeader>

  {#if order}
    <Card title="你正在申訴的訂單">
      <div class="grid gap-2 md:grid-cols-4">
        <div>
          <p class="text-xs text-slate-500">配送日</p>
          <p class="text-sm font-semibold text-slate-900">{order.deliveryDate}</p>
        </div>
        <div class="md:col-span-2">
          <p class="text-xs text-slate-500">品項摘要</p>
          <p class="text-sm text-slate-700">{orderSummary}</p>
        </div>
        <div>
          <p class="text-xs text-slate-500">訂單金額</p>
          <MoneyAmount
            amountMinor={order.total.amountMinor}
            currency={order.total.currency}
          />
        </div>
      </div>
      <div class="mt-2 flex flex-wrap items-center gap-2">
        <StateTag
          label={friendlyOrderStatus(order.status)}
          tone={orderStatusTone(order.status)}
        />
        <span class="text-xs text-slate-500">訂單 {maskIdentifier(order.orderId)}</span>
      </div>
    </Card>
  {/if}

  <Card title="申訴原因" description="請具體描述爭議點，以利福委會快速處理。">
    <FormField label="申訴原因" required hint="至少 5 個字">
      <textarea
        class="min-h-32 rounded-lg border border-slate-300 px-3 py-2 text-sm"
        maxlength={500}
        placeholder="例如：此訂單已取消但仍被扣款，請確認。"
        bind:value={reason}
      ></textarea>
    </FormField>
    <div class="flex flex-wrap gap-2">
      <Button
        variant="primary"
        disabled={!canSubmit}
        loading={submitting}
        onclick={submit}
      >
        送出申訴
      </Button>
    </div>
  </Card>

  <Card variant="info" title="預期回覆時間">
    <p class="text-sm text-slate-700">福委會通常於 3 個工作日內回覆，您可在下方追蹤進度。</p>
  </Card>

  <Card title="申訴追蹤">
    {#if loading}
      <p class="text-sm text-slate-600">載入申訴紀錄中...</p>
    {:else if loadError}
      <p class="text-sm text-rose-700">{loadError}</p>
    {:else if !ledger || ledger.disputes.length === 0}
      <EmptyState
        title="尚無申訴紀錄"
        description="若送出申訴，處理進度會顯示在這裡。"
      />
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
</PlantGuard>
