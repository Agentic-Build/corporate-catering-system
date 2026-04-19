<script lang="ts">
  import { onMount } from "svelte";

  import {
    PageHeader,
    Card,
    Button,
    DataTable,
    FormField,
    StateTag,
    toasts
  } from "$lib/components/ui";
  import {
    formatTaipeiDateTime,
    toTaipeiDateTime,
    todayTaipeiIsoDate
  } from "$lib/admin/portal";
  import { apiClient } from "$lib/platform/api";
  import {
    configureAdminApi,
    describeApiError,
    MAPPING_EFFECT_OPTIONS,
    type MappingEffect,
    type MappingView
  } from "$lib/admin/api";

  import type { PageData } from "./$types";

  let { data }: { data: PageData } = $props();

  const vendorId = $derived(data.vendorId);

  let loading = $state(true);
  let loadError = $state<string | null>(null);
  let mappings = $state<MappingView[]>([]);

  const today = todayTaipeiIsoDate();

  let draft = $state({
    mappingId: "",
    plantId: "",
    effect: "ALLOW" as MappingEffect,
    precedence: 100,
    serviceWindowStartsAtLocal: `${today}T10:00`,
    serviceWindowEndsAtLocal: `${today}T14:00`
  });
  let saving = $state(false);
  let deleting = $state<Record<string, boolean>>({});

  const columns = [
    { id: "mappingId", label: "mappingId", width: "16%" },
    { id: "plantId", label: "plantId", width: "12%" },
    { id: "effect", label: "effect", width: "10%" },
    { id: "precedence", label: "precedence", width: "10%" },
    { id: "window", label: "服務時段", width: "36%" },
    { id: "action", label: "動作", width: "16%" }
  ];

  onMount(() => {
    void refresh();
  });

  async function refresh() {
    loading = true;
    loadError = null;
    try {
      configureAdminApi(data.auth.apiBearerToken);
      const page = await apiClient.admin.listVendorPlantDeliveryMappings(
        vendorId,
        undefined,
        undefined,
        1,
        200
      );
      mappings = page.items;
    } catch (error) {
      loadError = describeApiError(error);
    } finally {
      loading = false;
    }
  }

  async function submit(event: SubmitEvent) {
    event.preventDefault();
    const mappingId = draft.mappingId.trim();
    const plantId = draft.plantId.trim();
    if (mappingId.length === 0 || plantId.length === 0) {
      toasts.error("mappingId 與 plantId 皆為必填。");
      return;
    }

    let startsAt: string;
    let endsAt: string;
    try {
      startsAt = toTaipeiDateTime(draft.serviceWindowStartsAtLocal);
      endsAt = toTaipeiDateTime(draft.serviceWindowEndsAtLocal);
    } catch (error) {
      toasts.error(error instanceof Error ? error.message : "服務時段格式無效");
      return;
    }

    if (Date.parse(startsAt) >= Date.parse(endsAt)) {
      toasts.error("服務時段結束時間必須晚於開始時間。");
      return;
    }

    if (!Number.isInteger(draft.precedence) || draft.precedence < 0 || draft.precedence > 65535) {
      toasts.error("precedence 必須是 0 到 65535 的整數。");
      return;
    }

    saving = true;
    try {
      await apiClient.admin.upsertVendorPlantDeliveryMapping(vendorId, mappingId, {
        plantId,
        effect: draft.effect,
        precedence: draft.precedence,
        serviceWindow: { startsAt, endsAt }
      });
      toasts.success(`映射 ${mappingId} 已儲存。`);
      draft = {
        mappingId: "",
        plantId: "",
        effect: "ALLOW",
        precedence: 100,
        serviceWindowStartsAtLocal: `${today}T10:00`,
        serviceWindowEndsAtLocal: `${today}T14:00`
      };
      await refresh();
    } catch (error) {
      toasts.error(describeApiError(error));
    } finally {
      saving = false;
    }
  }

  async function removeMapping(mappingId: string) {
    if (deleting[mappingId]) return;
    deleting = { ...deleting, [mappingId]: true };
    try {
      await apiClient.admin.deleteVendorPlantDeliveryMapping(vendorId, mappingId);
      toasts.success(`映射 ${mappingId} 已刪除。`);
      await refresh();
    } catch (error) {
      toasts.error(describeApiError(error));
    } finally {
      deleting = { ...deleting, [mappingId]: false };
    }
  }

  function editMapping(mapping: MappingView) {
    draft = {
      mappingId: mapping.mappingId,
      plantId: mapping.plantId,
      effect: mapping.effect,
      precedence: mapping.precedence,
      serviceWindowStartsAtLocal: mapping.serviceWindow.startsAt.slice(0, 16),
      serviceWindowEndsAtLocal: mapping.serviceWindow.endsAt.slice(0, 16)
    };
  }
</script>

<PageHeader
  eyebrow="商家廠區映射"
  title="映射清單"
  description="ALLOW / DENY 規則；優先級越小越先生效。"
  breadcrumbs={data.breadcrumbs}
>
  {#snippet actions()}
    <Button variant="secondary" href={`/admin/vendors/${vendorId}`}>返回商家</Button>
  {/snippet}
</PageHeader>

{#if loadError}
  <Card variant="danger" title="載入失敗">
    <p class="text-sm text-rose-900">{loadError}</p>
  </Card>
{:else}
  <Card
    title={`${vendorId} 的映射`}
    description="編輯時會帶入表單；送出相同 mappingId 視為更新。"
  >
    <DataTable
      rows={mappings}
      {columns}
      emptyLabel={loading ? "載入中..." : "尚無映射資料"}
    >
      {#snippet row(mapping: MappingView)}
        <tr class="hover:bg-slate-50">
          <td class="px-3 py-2 font-mono text-xs">{mapping.mappingId}</td>
          <td class="px-3 py-2 text-xs">{mapping.plantId}</td>
          <td class="px-3 py-2">
            <StateTag
              label={mapping.effect}
              tone={mapping.effect === "ALLOW" ? "success" : "danger"}
            />
          </td>
          <td class="px-3 py-2 text-xs">{mapping.precedence}</td>
          <td class="px-3 py-2 text-xs text-slate-700">
            {formatTaipeiDateTime(mapping.serviceWindow.startsAt)} ~
            {formatTaipeiDateTime(mapping.serviceWindow.endsAt)}
          </td>
          <td class="px-3 py-2">
            <div class="flex flex-wrap gap-1">
              <Button variant="ghost" size="sm" onclick={() => editMapping(mapping)}>編輯</Button>
              <Button
                variant="danger"
                size="sm"
                loading={deleting[mapping.mappingId] === true}
                onclick={() => void removeMapping(mapping.mappingId)}
              >
                刪除
              </Button>
            </div>
          </td>
        </tr>
      {/snippet}
    </DataTable>
  </Card>
{/if}

<Card title="新增 / 更新映射" description="填入 mappingId 後送出即可建立或覆寫。">
  <form class="grid gap-3" onsubmit={submit}>
    <div class="grid gap-3 md:grid-cols-3">
      <FormField label="mappingId" required>
        <input
          class="rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm"
          bind:value={draft.mappingId}
        />
      </FormField>
      <FormField label="plantId" required>
        <input
          class="rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm"
          bind:value={draft.plantId}
        />
      </FormField>
      <FormField label="effect" required>
        <select
          class="rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm"
          bind:value={draft.effect}
        >
          {#each MAPPING_EFFECT_OPTIONS as option}
            <option value={option}>{option}</option>
          {/each}
        </select>
      </FormField>
      <FormField label="precedence (0..65535)" required>
        <input
          type="number"
          min="0"
          max="65535"
          class="rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm"
          bind:value={draft.precedence}
        />
      </FormField>
      <FormField label="服務開始（台北）" required>
        <input
          type="datetime-local"
          class="rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm"
          bind:value={draft.serviceWindowStartsAtLocal}
        />
      </FormField>
      <FormField label="服務結束（台北）" required>
        <input
          type="datetime-local"
          class="rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm"
          bind:value={draft.serviceWindowEndsAtLocal}
        />
      </FormField>
    </div>
    <div class="flex gap-2">
      <Button type="submit" variant="primary" loading={saving}>儲存映射</Button>
      <Button variant="ghost" href={`/admin/vendors/${vendorId}`}>取消</Button>
    </div>
  </form>
</Card>
