<script lang="ts">
  import { onMount } from "svelte";

  import {
    PageHeader,
    Card,
    Button,
    EmptyState,
    StateTag,
    MoneyAmount,
    CountdownBadge,
    FormField,
    DateInput,
    Stepper,
    toasts
  } from "$lib/components/ui";
  import PlantGuard from "$lib/components/employee/plant-guard.svelte";
  import {
    configureEmployeeApi,
    describeApiError,
    todayTaipeiIsoDate,
    addDaysIsoDate,
    type MenuDiscoveryItem
  } from "$lib/employee/api";
  import { taipeiDateMinuteToEpochMs } from "$lib/employee/portal";
  import { friendlyMenuType, friendlyHealthTag } from "$lib/platform/labels";
  import { apiClient } from "$lib/platform/api";

  import type { PageData } from "./$types";

  let { data }: { data: PageData } = $props();

  const actor = $derived(data.actor);
  const plantId = $derived(actor?.scope.plantIds[0] ?? null);
  const role = $derived(actor?.role ?? null);

  const initialDate = todayTaipeiIsoDate();

  let menuView = $state<"week" | "calendar">("week");
  let menuAnchorDate = $state(initialDate);
  let menuFromDate = $state(initialDate);
  let menuToDate = $state(addDaysIsoDate(initialDate, 13));

  let items = $state<MenuDiscoveryItem[]>([]);
  let loading = $state(false);
  let loadError = $state<string | null>(null);

  let quantities = $state<Record<string, number>>({});
  let placingByItemId = $state<Record<string, boolean>>({});

  // Confirm dialog per-order state.
  let confirmOpen = $state(false);
  let confirmItem = $state<MenuDiscoveryItem | null>(null);
  let confirmQuantity = $state(1);
  let confirmNote = $state("");

  onMount(() => {
    if (role === "employee" && plantId) {
      void refreshMenus(plantId, data.auth.apiBearerToken);
    }
  });

  async function refreshMenus(resolvedPlantId: string, bearerToken: string | null) {
    loading = true;
    loadError = null;
    try {
      configureEmployeeApi(resolvedPlantId, bearerToken);
      const page = await apiClient.employee.listEmployeeMenus(
        resolvedPlantId,
        menuView,
        menuView === "week" ? menuAnchorDate : undefined,
        menuView === "calendar" ? menuFromDate : undefined,
        menuView === "calendar" ? menuToDate : undefined,
        1,
        200,
        "deliveryDate",
        "asc"
      );
      items = page.items;
    } catch (error) {
      loadError = describeApiError(error);
    } finally {
      loading = false;
    }
  }

  function onFilterChange() {
    if (plantId) void refreshMenus(plantId, data.auth.apiBearerToken);
  }

  function currentQuantity(item: MenuDiscoveryItem): number {
    const draft = quantities[item.menuItemId];
    const upper = Math.max(1, Math.min(20, item.remainingQuantity || 1));
    if (draft !== undefined) {
      return Math.max(1, Math.min(draft, upper));
    }
    if (item.remainingQuantity === 0) return 0;
    return 1;
  }

  function setQuantity(menuItemId: string, next: number, remainingQuantity: number) {
    const upper = Math.max(1, Math.min(20, remainingQuantity));
    const clamped = Math.max(1, Math.min(upper, next));
    quantities = { ...quantities, [menuItemId]: clamped };
  }

  function openConfirm(item: MenuDiscoveryItem) {
    const qty = currentQuantity(item);
    if (qty < 1 || qty > item.remainingQuantity) {
      toasts.error("下單數量超過可用庫存，請調整後再試。");
      return;
    }
    confirmItem = item;
    confirmQuantity = qty;
    confirmNote = "";
    confirmOpen = true;
  }

  async function performPlaceOrder() {
    if (!plantId || !confirmItem || placingByItemId[confirmItem.menuItemId]) return;
    const item = confirmItem;
    const quantity = confirmQuantity;
    placingByItemId = { ...placingByItemId, [item.menuItemId]: true };
    try {
      const note = confirmNote.trim();
      await apiClient.employee.createEmployeeOrder({
        plantId,
        deliveryDate: item.deliveryDate,
        lineItems: [
          {
            menuItemId: item.menuItemId,
            quantity,
            specialRequests: []
          }
        ],
        employeeNote: note.length === 0 ? undefined : note
      });
      toasts.success(`已建立訂單：${item.name} x ${quantity}`);
      quantities = { ...quantities, [item.menuItemId]: 1 };
      confirmOpen = false;
      confirmItem = null;
      if (plantId) void refreshMenus(plantId, data.auth.apiBearerToken);
    } catch (error) {
      toasts.error(describeApiError(error));
    } finally {
      placingByItemId = { ...placingByItemId, [item.menuItemId]: false };
    }
  }

  function cutoffEpochMs(item: MenuDiscoveryItem): number {
    return taipeiDateMinuteToEpochMs(item.cutoffDate, item.modifyCancelCutoffMinuteOfDay);
  }

  const confirmTotalMinor = $derived(
    confirmItem ? confirmItem.price.amountMinor * confirmQuantity : 0
  );
  const confirmCurrency = $derived(confirmItem?.price.currency ?? "TWD");
</script>

<PlantGuard role={role} plantId={plantId}>
  <PageHeader
    eyebrow="員工入口"
    title="瀏覽菜單並下單"
    description="週檢視適合一次規劃一整週；日曆檢視適合指定日期範圍。"
    breadcrumbs={data.breadcrumbs}
  />

  <Card title="檢視與條件" description="切換檢視或調整日期，結果會即時更新。">
    {#snippet actions()}
      <Button
        variant={menuView === "week" ? "primary" : "secondary"}
        size="sm"
        onclick={() => {
          menuView = "week";
          onFilterChange();
        }}
      >
        週檢視
      </Button>
      <Button
        variant={menuView === "calendar" ? "primary" : "secondary"}
        size="sm"
        onclick={() => {
          menuView = "calendar";
          onFilterChange();
        }}
      >
        日曆檢視
      </Button>
    {/snippet}

    <div class="grid gap-3 md:grid-cols-2">
      {#if menuView === "week"}
        <FormField label="週起始日（台北）">
          <DateInput bind:value={menuAnchorDate} onchange={onFilterChange} />
        </FormField>
      {:else}
        <FormField label="起始日">
          <DateInput bind:value={menuFromDate} onchange={onFilterChange} />
        </FormField>
        <FormField label="結束日">
          <DateInput bind:value={menuToDate} onchange={onFilterChange} />
        </FormField>
      {/if}
    </div>
  </Card>

  {#if loading}
    <Card title="同步中">
      <p class="text-sm text-slate-600">菜單載入中...</p>
    </Card>
  {:else if loadError}
    <Card variant="danger" title="載入失敗">
      <p class="text-sm text-rose-900">{loadError}</p>
    </Card>
  {:else if items.length === 0}
    <Card title="菜單">
      <EmptyState
        title="指定條件內沒有符合的菜單"
        description="調整日期範圍或切換檢視模式再試一次。"
      />
    </Card>
  {:else}
    <div class="grid gap-3 md:grid-cols-2">
      {#each items as item (item.menuItemId)}
        <article class="grid gap-3 rounded-2xl border border-slate-200 bg-white p-4 shadow-sm">
          {#if item.imageUrl}
            <img
              class="h-32 w-full rounded-lg object-cover"
              src={item.imageUrl}
              alt={item.name}
            />
          {/if}
          <div class="flex items-start justify-between gap-2">
            <div>
              <h3 class="text-base font-semibold text-slate-900">{item.name}</h3>
              <p class="text-xs text-slate-600">{item.description}</p>
              <p class="mt-1 text-xs text-slate-500">
                配送日：{item.deliveryDate} · {friendlyMenuType(item.menuType)}
              </p>
              {#if item.healthTags.length > 0}
                <div class="mt-1 flex flex-wrap gap-1">
                  {#each item.healthTags as tag (tag)}
                    <span class="rounded-full bg-emerald-50 px-2 py-0.5 text-[11px] text-emerald-700">
                      {friendlyHealthTag(tag)}
                    </span>
                  {/each}
                </div>
              {/if}
            </div>
            <MoneyAmount amountMinor={item.price.amountMinor} currency={item.price.currency} />
          </div>
          <div class="flex flex-wrap items-center gap-2">
            <StateTag
              label={`剩 ${item.remainingQuantity}`}
              tone={item.remainingQuantity === 0 ? "danger" : "info"}
            />
            <StateTag
              label={item.preorderOpen ? "可下單" : "已關閉"}
              tone={item.preorderOpen ? "success" : "pending"}
            />
            <CountdownBadge deadlineEpochMs={cutoffEpochMs(item)} prefix="截單剩" />
          </div>
          <div class="flex items-center gap-2">
            <Stepper
              value={currentQuantity(item)}
              min={1}
              max={Math.max(1, Math.min(20, item.remainingQuantity))}
              disabled={
                !item.preorderOpen ||
                item.remainingQuantity === 0 ||
                placingByItemId[item.menuItemId] === true
              }
              onchange={(next) => setQuantity(item.menuItemId, next, item.remainingQuantity)}
              aria-label={`${item.name} 數量`}
            />
            <Button
              variant="primary"
              disabled={!item.preorderOpen || item.remainingQuantity === 0}
              loading={placingByItemId[item.menuItemId] === true}
              onclick={() => openConfirm(item)}
            >
              立即下單
            </Button>
          </div>
        </article>
      {/each}
    </div>
  {/if}

  {#if confirmOpen && confirmItem}
    <div
      class="fixed inset-0 z-50 flex items-center justify-center bg-slate-900/40 px-4"
      role="dialog"
      aria-modal="true"
    >
      <div class="w-full max-w-md rounded-2xl bg-white p-5 shadow-xl">
        <h3 class="text-lg font-semibold text-slate-900">確認下單</h3>
        <p class="mt-2 text-sm text-slate-600">
          {confirmItem.name} × {confirmQuantity} 份 · 配送日 {confirmItem.deliveryDate}
        </p>
        <div class="mt-3 flex items-center justify-between rounded-lg bg-slate-50 px-3 py-2">
          <span class="text-sm text-slate-700">合計</span>
          <MoneyAmount amountMinor={confirmTotalMinor} currency={confirmCurrency} />
        </div>
        <div class="mt-3">
          <FormField label="訂單備註（可選）" hint="例如：午休晚 10 分鐘取餐，最多 200 字">
            <input
              type="text"
              class="rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm"
              maxlength={200}
              placeholder="留白表示沒有備註"
              bind:value={confirmNote}
            />
          </FormField>
        </div>
        <div class="mt-5 flex justify-end gap-2">
          <Button
            variant="ghost"
            disabled={placingByItemId[confirmItem.menuItemId] === true}
            onclick={() => {
              confirmOpen = false;
              confirmItem = null;
            }}
          >
            再想一下
          </Button>
          <Button
            variant="primary"
            loading={placingByItemId[confirmItem.menuItemId] === true}
            onclick={performPlaceOrder}
          >
            確定下單
          </Button>
        </div>
      </div>
    </div>
  {/if}
</PlantGuard>
