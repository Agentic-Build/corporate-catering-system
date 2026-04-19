<script lang="ts">
  import { onMount } from "svelte";

  import {
    Button,
    Card,
    DataTable,
    FormField,
    MoneyAmount,
    PageHeader,
    StateTag,
    toasts
  } from "$lib/components/ui";
  import { zhTW } from "$lib/i18n/zh-tw";
  import { apiClient, ensureApiClientConfigured } from "$lib/platform/api";
  import { normalizeApiFailure } from "$lib/platform/api/failure";
  import {
    friendlyMenuItemStatus,
    menuItemStatusTone
  } from "$lib/platform/labels";
  import {
    MENU_STATUS_OPTIONS,
    addDaysIsoDate,
    todayTaipeiIsoDate
  } from "$lib/vendor/helpers";
  import type { VendorMenuItemStatus } from "../../../../../../contract/generated/ts-client";

  import type { PageData } from "./$types";

  let { data }: { data: PageData } = $props();

  type MenuPage = Awaited<ReturnType<typeof apiClient.vendor.listVendorMenuItems>>;
  type MenuItem = MenuPage["items"][number];
  type MenuStatus = MenuItem["status"];

  const initialDate = todayTaipeiIsoDate();

  let fromDate = $state(initialDate);
  let toDate = $state(addDaysIsoDate(initialDate, 14));
  let statusFilter = $state<"ALL" | MenuStatus>("ALL");
  let items = $state<MenuItem[]>([]);
  let pageMeta = $state<MenuPage["page"] | null>(null);
  let loading = $state(false);
  let errorMessage = $state<string | null>(null);

  let openStatusMenuFor = $state<string | null>(null);
  let statusUpdatingFor = $state<string | null>(null);

  onMount(async () => {
    try {
      ensureApiClientConfigured(data.auth.apiBearerToken);
    } catch (error) {
      errorMessage = normalizeApiFailure(error).localizedMessage;
      return;
    }
    await refresh();
  });

  async function refresh() {
    if (loading) return;
    loading = true;
    errorMessage = null;
    try {
      const result = await apiClient.vendor.listVendorMenuItems(
        fromDate,
        toDate,
        statusFilter === "ALL" ? undefined : statusFilter,
        1,
        200,
        "asc"
      );
      items = result.items;
      pageMeta = result.page;
    } catch (error) {
      const failure = normalizeApiFailure(error);
      errorMessage = failure.localizedMessage;
      toasts.error(failure.localizedMessage);
    } finally {
      loading = false;
    }
  }

  async function updateStatus(item: MenuItem, next: VendorMenuItemStatus) {
    if (item.status === next) {
      openStatusMenuFor = null;
      return;
    }
    statusUpdatingFor = item.menuItemId;
    try {
      await apiClient.vendor.updateVendorMenuItemStatus(item.menuItemId, { status: next });
      toasts.success(`${item.name} 已切換為 ${friendlyMenuItemStatus(next)}`);
      items = items.map((entry) =>
        entry.menuItemId === item.menuItemId ? { ...entry, status: next } : entry
      );
    } catch (error) {
      toasts.error(normalizeApiFailure(error).localizedMessage);
    } finally {
      statusUpdatingFor = null;
      openStatusMenuFor = null;
    }
  }

  async function copyId(menuItemId: string) {
    try {
      await navigator.clipboard.writeText(menuItemId);
      toasts.success(`已複製：${menuItemId}`);
    } catch {
      toasts.error("瀏覽器不支援自動複製");
    }
  }

  const columns = [
    { id: "thumb", label: "" },
    { id: "name", label: "名稱" },
    { id: "date", label: "配送日" },
    { id: "price", label: "價格" },
    { id: "remain", label: "剩餘 / 上限" },
    { id: "status", label: "狀態" },
    { id: "actions", label: "" }
  ];
</script>

<PageHeader
  title={zhTW.vendor.menu.listTitle}
  description={zhTW.vendor.menu.listDescription}
  eyebrow={data.actor?.displayName ?? ""}
  breadcrumbs={data.breadcrumbs}
>
  {#snippet actions()}
    <Button href="/vendor/menu/new" variant="primary">新增菜單</Button>
  {/snippet}
</PageHeader>

{#if errorMessage}
  <div class="mb-4 rounded-lg border border-rose-200 bg-rose-50 px-3 py-2 text-sm text-rose-900">
    {errorMessage}
  </div>
{/if}

<Card title="篩選條件">
  <div class="grid gap-3 md:grid-cols-4">
    <FormField label="起始日">
      <input type="date" class="rounded border border-slate-300 bg-white px-2 py-1.5" bind:value={fromDate} />
    </FormField>
    <FormField label="結束日">
      <input type="date" class="rounded border border-slate-300 bg-white px-2 py-1.5" bind:value={toDate} />
    </FormField>
    <FormField label="狀態">
      <select class="rounded border border-slate-300 bg-white px-2 py-1.5" bind:value={statusFilter}>
        <option value="ALL">全部</option>
        {#each MENU_STATUS_OPTIONS as status}
          <option value={status}>{friendlyMenuItemStatus(status)}</option>
        {/each}
      </select>
    </FormField>
    <div class="flex items-end">
      <Button variant="primary" onclick={refresh} loading={loading}>套用篩選</Button>
    </div>
  </div>
  <p class="text-xs text-slate-600">共 {pageMeta?.totalItems ?? 0} 筆</p>
</Card>

<section class="mt-4">
  <DataTable {columns} rows={items} emptyLabel="目前篩選下沒有菜單資料。">
    {#snippet row(item: MenuItem)}
      <tr class="group hover:bg-slate-50">
        <td class="px-3 py-2">
          {#if item.imageUrl}
            <img
              src={item.imageUrl}
              alt={item.name}
              class="h-10 w-10 rounded border border-slate-200 object-cover"
            />
          {:else}
            <div class="flex h-10 w-10 items-center justify-center rounded border border-dashed border-slate-300 bg-slate-50 text-[10px] text-slate-400">
              無圖
            </div>
          {/if}
        </td>
        <td class="px-3 py-2">
          <a class="font-semibold text-slate-900 hover:text-cyan-800" href={`/vendor/menu/${item.menuItemId}`}>
            {item.name}
          </a>
          <p class="text-xs text-slate-600 line-clamp-1">{item.description}</p>
        </td>
        <td class="px-3 py-2 tabular-nums">{item.deliveryDate}</td>
        <td class="px-3 py-2">
          <MoneyAmount amountMinor={item.price.amountMinor} currency={item.price.currency} />
        </td>
        <td class="px-3 py-2 tabular-nums text-xs">
          <span class="font-semibold text-slate-800">{item.remainingQuantity}</span>
          <span class="text-slate-500"> / {item.maxDailyQuantity}</span>
        </td>
        <td class="px-3 py-2">
          <div class="relative inline-block">
            <button
              type="button"
              class="inline-flex items-center gap-1 rounded-full border border-slate-300 bg-white px-2 py-0.5 hover:border-cyan-500"
              onclick={() =>
                (openStatusMenuFor = openStatusMenuFor === item.menuItemId ? null : item.menuItemId)}
              disabled={statusUpdatingFor === item.menuItemId}
            >
              <StateTag
                label={friendlyMenuItemStatus(item.status)}
                tone={menuItemStatusTone(item.status)}
              />
              <span class="text-[10px] text-slate-500">▼</span>
            </button>
            {#if openStatusMenuFor === item.menuItemId}
              <div class="absolute right-0 z-20 mt-1 grid min-w-36 gap-0.5 rounded-lg border border-slate-200 bg-white p-1 shadow-lg">
                {#each MENU_STATUS_OPTIONS as status}
                  <button
                    type="button"
                    class={`flex items-center gap-2 rounded px-2 py-1.5 text-left text-xs ${
                      item.status === status
                        ? "bg-cyan-50 text-cyan-900"
                        : "text-slate-700 hover:bg-slate-100"
                    }`}
                    onclick={() => updateStatus(item, status)}
                  >
                    <StateTag
                      label={friendlyMenuItemStatus(status)}
                      tone={menuItemStatusTone(status)}
                    />
                    {#if item.status === status}
                      <span class="text-[10px] text-cyan-700">✓</span>
                    {/if}
                  </button>
                {/each}
              </div>
            {/if}
          </div>
        </td>
        <td class="px-3 py-2 text-right">
          <button
            type="button"
            class="rounded border border-slate-200 bg-white px-2 py-0.5 text-[11px] text-slate-600 opacity-0 transition hover:border-cyan-400 hover:text-cyan-800 group-hover:opacity-100"
            title={item.menuItemId}
            onclick={() => copyId(item.menuItemId)}
          >
            複製 ID
          </button>
        </td>
      </tr>
    {/snippet}
  </DataTable>
</section>
