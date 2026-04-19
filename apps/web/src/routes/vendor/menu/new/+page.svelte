<script lang="ts">
  import { onMount } from "svelte";
  import { goto } from "$app/navigation";

  import { PageHeader, toasts } from "$lib/components/ui";
  import MenuForm, { type MenuDraft } from "$lib/components/vendor/menu-form.svelte";
  import { zhTW } from "$lib/i18n/zh-tw";
  import { apiClient, ensureApiClientConfigured } from "$lib/platform/api";
  import { normalizeApiFailure } from "$lib/platform/api/failure";
  import {
    generateMenuItemId,
    parseOptionalPositiveInt,
    todayTaipeiIsoDate
  } from "$lib/vendor/helpers";

  import type { PageData } from "./$types";

  let { data }: { data: PageData } = $props();

  let draft = $state<MenuDraft>({
    menuItemId: generateMenuItemId(),
    deliveryDate: todayTaipeiIsoDate(),
    name: "",
    description: "",
    menuType: "BENTO",
    healthTags: [],
    imageUrl: "",
    currency: "TWD",
    amountMinor: 12000,
    maxDailyQuantity: 30,
    preorderOpenDaysAheadOverride: "",
    modifyCancelCutoffMinuteOfDayOverride: ""
  });

  let submitting = $state(false);

  onMount(() => {
    try {
      ensureApiClientConfigured(data.auth.apiBearerToken);
    } catch (error) {
      toasts.error(normalizeApiFailure(error).localizedMessage);
    }
  });

  async function submit() {
    if (submitting) return;
    const menuItemId = draft.menuItemId.trim();
    const name = draft.name.trim();
    const description = draft.description.trim();
    if (!menuItemId) {
      toasts.error("請先填寫 menuItemId。");
      return;
    }
    if (!name || !description) {
      toasts.error("菜單名稱與描述不可為空。");
      return;
    }

    submitting = true;
    try {
      await apiClient.vendor.upsertVendorMenuItem(menuItemId, {
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
      toasts.success(`菜單 ${menuItemId} 已建立。`);
      await goto("/vendor/menu");
    } catch (error) {
      toasts.error(normalizeApiFailure(error).localizedMessage);
    } finally {
      submitting = false;
    }
  }
</script>

<PageHeader
  title={zhTW.vendor.menu.createTitle}
  description="填寫基本資訊後送出，菜單會即時生效並同步至員工端。"
  breadcrumbs={data.breadcrumbs}
/>

<MenuForm bind:draft submitLabel="建立菜單" {submitting} onSubmit={submit} />
