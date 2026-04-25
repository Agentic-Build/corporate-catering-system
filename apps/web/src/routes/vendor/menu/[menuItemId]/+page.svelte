<script lang="ts">
  import { onMount } from "svelte";

  import { Button, Card, PageHeader, StateTag, toasts } from "$lib/components/ui";
  import MenuForm, { type MenuDraft } from "$lib/components/vendor/menu-form.svelte";
  import { zhTW } from "$lib/i18n/zh-tw";
  import { apiClient, ensureApiClientConfigured } from "$lib/platform/api";
  import { normalizeApiFailure } from "$lib/platform/api/failure";
  import {
    MENU_STATUS_OPTIONS,
    addDaysIsoDate,
    menuStatusLabel,
    menuStatusTone,
    parseOptionalPositiveInt,
    todayTaipeiIsoDate
  } from "$lib/vendor/helpers";

  import type { PageData } from "./$types";
  import type { VendorMenuItemStatus } from "../../../../../../../contract/generated/ts-client";

  let { data }: { data: PageData } = $props();

  type MenuItem = Awaited<ReturnType<typeof apiClient.vendor.listVendorMenuItems>>["items"][number];

  let draft = $state<MenuDraft | null>(null);
  let currentStatus = $state<VendorMenuItemStatus | null>(null);
  let loading = $state(true);
  let submitting = $state(false);
  let statusSubmittingFor = $state<VendorMenuItemStatus | null>(null);
  let errorMessage = $state<string | null>(null);

  onMount(async () => {
    try {
      ensureApiClientConfigured(data.auth.apiBearerToken);
    } catch (error) {
      errorMessage = normalizeApiFailure(error).localizedMessage;
      loading = false;
      return;
    }
    await loadItem();
  });

  async function loadItem() {
    loading = true;
    errorMessage = null;
    try {
      const today = todayTaipeiIsoDate();
      const result = await apiClient.vendor.listVendorMenuItems(
        addDaysIsoDate(today, -30),
        addDaysIsoDate(today, 90),
        undefined,
        1,
        500,
        "asc"
      );
      const found = result.items.find((entry) => entry.menuItemId === data.menuItemId);
      if (!found) {
        errorMessage = `找不到菜單 ${data.menuItemId}，可能已被刪除或超出查詢範圍。`;
        return;
      }
      currentStatus = found.status;
      draft = toDraft(found);
    } catch (error) {
      errorMessage = normalizeApiFailure(error).localizedMessage;
    } finally {
      loading = false;
    }
  }

  function toDraft(item: MenuItem): MenuDraft {
    return {
      menuItemId: item.menuItemId,
      deliveryDate: item.deliveryDate,
      name: item.name,
      description: item.description,
      menuType: item.menuType,
      healthTags: [...item.healthTags],
      imageUrl: item.imageUrl ?? "",
      currency: item.price.currency,
      amountMinor: item.price.amountMinor,
      maxDailyQuantity: item.maxDailyQuantity,
      preorderOpenDaysAheadOverride: String(item.preorderOpenDaysAhead),
      modifyCancelCutoffMinuteOfDayOverride: String(item.modifyCancelCutoffMinuteOfDay)
    };
  }

  async function submit() {
    if (!draft || submitting) return;
    const name = draft.name.trim();
    const description = draft.description.trim();
    if (!name || !description) {
      toasts.error("菜單名稱與描述不可為空。");
      return;
    }
    submitting = true;
    try {
      await apiClient.vendor.upsertVendorMenuItem(draft.menuItemId, {
        deliveryDate: draft.deliveryDate,
        name,
        description,
        menuType: draft.menuType,
        healthTags: draft.healthTags,
        imageUrl: draft.imageUrl.trim() || undefined,
        price: {
          currency: draft.currency.trim().toUpperCase(),
          amountMinor: draft.amountMinor
        },
        maxDailyQuantity: draft.maxDailyQuantity,
        preorderOpenDaysAheadOverride: parseOptionalPositiveInt(draft.preorderOpenDaysAheadOverride),
        modifyCancelCutoffMinuteOfDayOverride: parseOptionalPositiveInt(
          draft.modifyCancelCutoffMinuteOfDayOverride
        )
      });
      toasts.success(`菜單 ${draft.menuItemId} 已更新。`);
      await loadItem();
    } catch (error) {
      toasts.error(normalizeApiFailure(error).localizedMessage);
    } finally {
      submitting = false;
    }
  }

  async function updateStatus(status: VendorMenuItemStatus) {
    if (statusSubmittingFor !== null) return;
    if (currentStatus === status) {
      toasts.info(`已是 ${menuStatusLabel(status)} 狀態。`);
      return;
    }
    statusSubmittingFor = status;
    try {
      await apiClient.vendor.updateVendorMenuItemStatus(data.menuItemId, { status });
      toasts.success(`菜單 ${data.menuItemId} 已切換為 ${menuStatusLabel(status)}。`);
      currentStatus = status;
    } catch (error) {
      toasts.error(normalizeApiFailure(error).localizedMessage);
    } finally {
      statusSubmittingFor = null;
    }
  }
</script>

<PageHeader
  title={zhTW.vendor.menu.editTitle}
  description={zhTW.vendor.menu.editDescription}
  breadcrumbs={data.breadcrumbs}
>
  {#snippet actions()}
    <Button href="/vendor/menu" variant="ghost">返回列表</Button>
  {/snippet}
</PageHeader>

{#if errorMessage}
  <div class="mb-4 rounded-lg border border-rose-200 bg-rose-50 px-3 py-2 text-sm text-rose-900">
    {errorMessage}
  </div>
{/if}

{#if loading}
  <p class="text-sm text-slate-600">{zhTW.common.pageLoading}</p>
{:else if draft && currentStatus}
  <Card title="狀態切換">
    <div class="flex flex-wrap items-center gap-3">
      <span class="text-sm text-slate-600">目前狀態：</span>
      <StateTag label={menuStatusLabel(currentStatus)} tone={menuStatusTone(currentStatus)} />
      <div class="flex flex-wrap gap-2">
        {#each MENU_STATUS_OPTIONS as status}
          <Button
            size="sm"
            variant={currentStatus === status ? "primary" : "secondary"}
            onclick={() => updateStatus(status)}
            loading={statusSubmittingFor === status}
          >
            {menuStatusLabel(status)}
          </Button>
        {/each}
      </div>
    </div>
  </Card>
  <div class="mt-4">
    <MenuForm bind:draft submitLabel="送出菜單更新" {submitting} lockMenuItemId={true} onSubmit={submit} />
  </div>
{/if}
