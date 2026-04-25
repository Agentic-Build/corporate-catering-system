<script lang="ts">
  import { onMount } from "svelte";

  import {
    PageHeader,
    Card,
    Button,
    DataTable,
    FormField
  } from "$lib/components/ui";
  import { parseOptionalEpochDay } from "$lib/admin/portal";
  import { apiClient } from "$lib/platform/api";
  import {
    configureAdminApi,
    describeApiError,
    AUDIT_ACTION_OPTIONS,
    AUDIT_ENTITY_TYPE_OPTIONS,
    normalizeOptional,
    type AuditAction,
    type AuditEntityType,
    type AuditResponsibilityView
  } from "$lib/admin/api";

  import type { PageData } from "./$types";

  let { data }: { data: PageData } = $props();

  let loading = $state(true);
  let loadError = $state<string | null>(null);
  let items = $state<AuditResponsibilityView[]>([]);

  let filters = $state({
    actorId: "",
    action: "ALL" as "ALL" | AuditAction,
    entityType: "ALL" as "ALL" | AuditEntityType,
    entityId: "",
    correlationId: "",
    occurredFromEpochDay: "",
    occurredToEpochDay: ""
  });

  const columns = [
    { id: "actor", label: "actor", width: "22%" },
    { id: "role", label: "role", width: "12%" },
    { id: "events", label: "eventCount", width: "10%" },
    { id: "actions", label: "actions 集合", width: "28%" },
    { id: "entities", label: "entities 集合", width: "28%" }
  ];

  onMount(() => {
    void refresh();
  });

  async function refresh() {
    loading = true;
    loadError = null;
    try {
      configureAdminApi(data.auth.apiBearerToken);
      let fromEpochDay: number | undefined;
      let toEpochDay: number | undefined;
      try {
        fromEpochDay = parseOptionalEpochDay(filters.occurredFromEpochDay);
        toEpochDay = parseOptionalEpochDay(filters.occurredToEpochDay);
      } catch (error) {
        loadError = error instanceof Error ? error.message : "日期參數無效";
        loading = false;
        return;
      }

      const response = await apiClient.admin.queryAuditResponsibilities(
        normalizeOptional(filters.actorId),
        filters.action === "ALL" ? undefined : filters.action,
        filters.entityType === "ALL" ? undefined : filters.entityType,
        normalizeOptional(filters.entityId),
        fromEpochDay,
        toEpochDay,
        normalizeOptional(filters.correlationId)
      );
      items = response.items;
    } catch (error) {
      loadError = describeApiError(error);
    } finally {
      loading = false;
    }
  }
</script>

<PageHeader
  eyebrow="稽核查詢"
  title="責任歸屬"
  description="同樣的篩選條件，按 actor 彙總事件數、action 集合與 entity 集合。"
  breadcrumbs={data.breadcrumbs}
>
  {#snippet actions()}
    <Button variant="secondary" href="/admin/audit">稽核事件視圖</Button>
  {/snippet}
</PageHeader>

<Card title="篩選">
  <div class="grid gap-3 md:grid-cols-4">
    <FormField label="actorId">
      <input
        class="rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm"
        bind:value={filters.actorId}
      />
    </FormField>
    <FormField label="action">
      <select
        class="rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm"
        bind:value={filters.action}
      >
        <option value="ALL">ALL</option>
        {#each AUDIT_ACTION_OPTIONS as option}
          <option value={option}>{option}</option>
        {/each}
      </select>
    </FormField>
    <FormField label="entityType">
      <select
        class="rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm"
        bind:value={filters.entityType}
      >
        <option value="ALL">ALL</option>
        {#each AUDIT_ENTITY_TYPE_OPTIONS as option}
          <option value={option}>{option}</option>
        {/each}
      </select>
    </FormField>
    <FormField label="entityId">
      <input
        class="rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm"
        bind:value={filters.entityId}
      />
    </FormField>
    <FormField label="correlationId">
      <input
        class="rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm"
        bind:value={filters.correlationId}
      />
    </FormField>
    <FormField label="occurredFromEpochDay">
      <input
        type="number"
        min="1"
        class="rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm"
        bind:value={filters.occurredFromEpochDay}
      />
    </FormField>
    <FormField label="occurredToEpochDay">
      <input
        type="number"
        min="1"
        class="rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm"
        bind:value={filters.occurredToEpochDay}
      />
    </FormField>
    <div class="flex items-end">
      <Button variant="primary" loading={loading} onclick={() => void refresh()}>套用</Button>
    </div>
  </div>
</Card>

{#if loadError}
  <Card variant="danger" title="載入失敗">
    <p class="text-sm text-rose-900">{loadError}</p>
  </Card>
{:else}
  <Card title="按 actor 歸屬">
    <DataTable
      rows={items}
      {columns}
      emptyLabel={loading ? "載入中..." : "沒有符合條件的紀錄"}
    >
      {#snippet row(entry: AuditResponsibilityView)}
        <tr class="hover:bg-slate-50 align-top">
          <td class="px-3 py-2 font-mono text-[11px]">{entry.actorId}</td>
          <td class="px-3 py-2 text-xs">{entry.role}</td>
          <td class="px-3 py-2 text-xs font-semibold">{entry.eventCount}</td>
          <td class="px-3 py-2 text-[11px] text-slate-700">{entry.actions.join(", ")}</td>
          <td class="px-3 py-2 text-[11px] text-slate-700">
            {#each entry.entities as entity}
              <div>{entity.entityType} · <span class="font-mono">{entity.entityId}</span></div>
            {/each}
          </td>
        </tr>
      {/snippet}
    </DataTable>
  </Card>
{/if}
