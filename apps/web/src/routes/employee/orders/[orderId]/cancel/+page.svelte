<script lang="ts">
  import { onMount } from "svelte";
  import { goto } from "$app/navigation";

  import {
    PageHeader,
    Card,
    Button,
    FormField,
    ConfirmDialog,
    toasts
  } from "$lib/components/ui";
  import PlantGuard from "$lib/components/employee/plant-guard.svelte";
  import {
    configureEmployeeApi,
    describeApiError,
    findEmployeeOrderById,
    type EmployeeOrderView
  } from "$lib/employee/api";
  import { isEmployeeOrderEditable } from "$lib/employee/portal";
  import { friendlyOrderStatus, maskIdentifier } from "$lib/platform/labels";
  import { apiClient } from "$lib/platform/api";

  import type { PageData } from "./$types";

  let { data }: { data: PageData } = $props();

  const actor = $derived(data.actor);
  const plantId = $derived(actor?.scope.plantIds[0] ?? null);
  const role = $derived(actor?.role ?? null);
  const orderId = $derived(data.orderId);

  const REASON_PRESETS = [
    { value: "當天被派外出", label: "當天被派外出" },
    { value: "臨時改約", label: "臨時改約" },
    { value: "訂錯品項", label: "訂錯品項" },
    { value: "OTHER", label: "其他（請補充）" }
  ];

  let order = $state<EmployeeOrderView | null>(null);
  let reasonPreset = $state<string>(REASON_PRESETS[0].value);
  let otherText = $state("");
  let confirmOpen = $state(false);
  let loading = $state(true);
  let loadError = $state<string | null>(null);
  let submitting = $state(false);

  const finalReason = $derived(
    reasonPreset === "OTHER" ? otherText.trim() : reasonPreset
  );
  const canSubmit = $derived(finalReason.length >= 2);

  onMount(() => {
    if (role === "employee" && plantId) {
      void loadOrder(plantId, data.auth.apiBearerToken);
    }
  });

  async function loadOrder(resolvedPlantId: string, bearerToken: string | null) {
    loading = true;
    loadError = null;
    try {
      configureEmployeeApi(resolvedPlantId, bearerToken);
      order = await findEmployeeOrderById(orderId, { plantId: resolvedPlantId });
      if (!order) {
        loadError = `找不到訂單 ${orderId}。可能已超過查詢範圍。`;
      }
    } catch (error) {
      loadError = describeApiError(error);
    } finally {
      loading = false;
    }
  }

  function onClickCancel() {
    if (!canSubmit) {
      toasts.error("請選擇取消原因。");
      return;
    }
    confirmOpen = true;
  }

  async function performCancel() {
    if (!order) return;
    submitting = true;
    try {
      await apiClient.employee.updateEmployeeOrder(order.orderId, {
        operation: "CANCEL",
        cancelReason: finalReason
      });
      toasts.success(`訂單已取消。`);
      confirmOpen = false;
      await goto(`/employee/orders/${order.orderId}`);
    } catch (error) {
      toasts.error(describeApiError(error));
    } finally {
      submitting = false;
    }
  }
</script>

<PlantGuard role={role} plantId={plantId}>
  <PageHeader
    eyebrow={`取消訂單 ${maskIdentifier(orderId)}`}
    title="取消並退款"
    description="截單前可取消訂單，取消後系統會產生對應退款流水。"
    breadcrumbs={data.breadcrumbs}
  >
    {#snippet actions()}
      <Button href={`/employee/orders/${orderId}`} variant="ghost">返回訂單</Button>
    {/snippet}
  </PageHeader>

  {#if loading}
    <Card title="同步中">
      <p class="text-sm text-slate-600">訂單載入中...</p>
    </Card>
  {:else if loadError || !order}
    <Card variant="danger" title="無法取消">
      <p class="text-sm text-rose-900">{loadError ?? "訂單不存在"}</p>
    </Card>
  {:else if !isEmployeeOrderEditable(order.status)}
    <Card variant="warning" title="目前狀態不可取消">
      <p class="text-sm text-slate-700">
        訂單狀態為 {friendlyOrderStatus(order.status)}，只有待處理 / 已修改的訂單可取消。
      </p>
      <div>
        <Button href={`/employee/orders/${order.orderId}`} variant="secondary">返回訂單詳情</Button>
      </div>
    </Card>
  {:else}
    <Card title="取消原因" description="原因會記錄到稽核軌跡。">
      <FormField label="選擇原因" required>
        <div class="grid gap-2">
          {#each REASON_PRESETS as preset (preset.value)}
            <label class="flex cursor-pointer items-center gap-2 rounded-lg border border-slate-200 bg-white px-3 py-2 text-sm">
              <input
                type="radio"
                name="cancel-reason"
                value={preset.value}
                bind:group={reasonPreset}
              />
              <span class="text-slate-700">{preset.label}</span>
            </label>
          {/each}
        </div>
      </FormField>
      {#if reasonPreset === "OTHER"}
        <FormField label="補充說明" required hint="至少 2 個字">
          <textarea
            class="min-h-24 rounded-lg border border-slate-300 px-3 py-2 text-sm"
            maxlength={200}
            placeholder="例如：當天臨時被派外出，需取消預購。"
            bind:value={otherText}
          ></textarea>
        </FormField>
      {/if}
      <div class="flex flex-wrap gap-2">
        <Button variant="danger" disabled={!canSubmit} onclick={onClickCancel}>
          確認取消
        </Button>
        <Button href={`/employee/orders/${order.orderId}`} variant="ghost">
          返回訂單
        </Button>
      </div>
    </Card>

    <ConfirmDialog
      open={confirmOpen}
      title="確定要取消此訂單？"
      description="取消後狀態會變更為已取消，且無法還原。"
      confirmLabel="確定取消"
      cancelLabel="不要取消"
      tone="danger"
      loading={submitting}
      onConfirm={performCancel}
      onCancel={() => {
        confirmOpen = false;
      }}
    />
  {/if}
</PlantGuard>
