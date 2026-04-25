<script lang="ts">
  import { onMount } from "svelte";

  import {
    PageHeader,
    Card,
    Button,
    DataTable,
    FormField,
    DateInput
  } from "$lib/components/ui";
  import { formatTaipeiDateTime } from "$lib/admin/portal";
  import { apiClient } from "$lib/platform/api";
  import {
    configureAdminApi,
    describeApiError,
    AUDIT_ACTION_OPTIONS,
    AUDIT_ENTITY_TYPE_OPTIONS,
    normalizeOptional,
    type AuditAction,
    type AuditEntityType,
    type AuditEvidenceView
  } from "$lib/admin/api";
  import { isoDateToEpochDay } from "$lib/platform/time-formats";

  import type { PageData } from "./$types";

  let { data }: { data: PageData } = $props();

  let loading = $state(true);
  let loadError = $state<string | null>(null);
  let items = $state<AuditEvidenceView[]>([]);

  let filters = $state({
    actorId: "",
    action: "ALL" as "ALL" | AuditAction,
    entityType: "ALL" as "ALL" | AuditEntityType,
    entityId: "",
    correlationId: "",
    occurredFromDate: "",
    occurredToDate: ""
  });

  const columns = [
    { id: "occurredAt", label: "發生時間", width: "16%" },
    { id: "actor", label: "操作者", width: "14%" },
    { id: "action", label: "動作", width: "18%" },
    { id: "entity", label: "目標", width: "22%" },
    { id: "reason", label: "原因", width: "20%" },
    { id: "correlationId", label: "相關", width: "10%" }
  ];

  onMount(() => {
    void refresh();
  });

  async function refresh() {
    loading = true;
    loadError = null;
    try {
      configureAdminApi(data.auth.apiBearerToken);
      const fromEpochDay = filters.occurredFromDate.length > 0
        ? isoDateToEpochDay(filters.occurredFromDate)
        : undefined;
      const toEpochDay = filters.occurredToDate.length > 0
        ? isoDateToEpochDay(filters.occurredToDate)
        : undefined;

      const response = await apiClient.admin.queryAuditInvestigations(
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

  // ---------------------------------------------------------------------------
  // Facet aggregation (client-side over current result set). Gives admins a
  // quick pivot to narrow the search: click any facet to filter by that value.
  // ---------------------------------------------------------------------------

  function topFacets(
    pick: (entry: AuditEvidenceView) => string | undefined | null,
    limit = 8
  ): Array<{ label: string; count: number }> {
    const counts = new Map<string, number>();
    for (const entry of items) {
      const value = pick(entry);
      if (!value) continue;
      counts.set(value, (counts.get(value) ?? 0) + 1);
    }
    return [...counts.entries()]
      .map(([label, count]) => ({ label, count }))
      .sort((a, b) => b.count - a.count)
      .slice(0, limit);
  }

  const actionFacets = $derived(topFacets((e) => e.action));
  const entityTypeFacets = $derived(topFacets((e) => e.entityType));
  const actorFacets = $derived(topFacets((e) => e.actorId));

  function escapeCsvField(value: string): string {
    if (/[",\n]/.test(value)) {
      return `"${value.replace(/"/g, '""')}"`;
    }
    return value;
  }

  function exportCsv() {
    if (items.length === 0) return;
    const header = [
      "occurredAt",
      "actorId",
      "actorRole",
      "authenticationSource",
      "action",
      "entityType",
      "entityId",
      "reason",
      "correlationId"
    ];
    const rows = items.map((e) =>
      [
        e.occurredAt ?? "",
        e.actorId ?? "",
        e.actorRole ?? "",
        e.authenticationSource ?? "",
        e.action ?? "",
        e.entityType ?? "",
        e.entityId ?? "",
        e.reason ?? "",
        e.correlationId ?? ""
      ]
        .map((v) => escapeCsvField(String(v)))
        .join(",")
    );
    const csv = [header.join(","), ...rows].join("\n");
    const blob = new Blob([csv], { type: "text/csv;charset=utf-8;" });
    const url = URL.createObjectURL(blob);
    const link = document.createElement("a");
    link.href = url;
    link.download = `audit-investigations-${new Date().toISOString().slice(0, 10)}.csv`;
    document.body.appendChild(link);
    link.click();
    document.body.removeChild(link);
    URL.revokeObjectURL(url);
  }
</script>

<PageHeader
  eyebrow="稽核查詢"
  title="稽核留痕"
  description="查詢 append-only 稽核事件；需要時可切換到責任歸屬視圖。"
  breadcrumbs={data.breadcrumbs}
>
  {#snippet actions()}
    <Button variant="secondary" href="/admin/audit/responsibilities">責任歸屬視圖</Button>
    <Button variant="primary" onclick={exportCsv} disabled={items.length === 0}>
      匯出 CSV
    </Button>
  {/snippet}
</PageHeader>

<Card title="篩選">
  <div class="grid gap-3 md:grid-cols-4">
    <FormField label="操作者 ID">
      <input
        class="rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm"
        bind:value={filters.actorId}
      />
    </FormField>
    <FormField label="動作">
      <select
        class="rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm"
        bind:value={filters.action}
      >
        <option value="ALL">全部</option>
        {#each AUDIT_ACTION_OPTIONS as option}
          <option value={option}>{option}</option>
        {/each}
      </select>
    </FormField>
    <FormField label="實體類型">
      <select
        class="rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm"
        bind:value={filters.entityType}
      >
        <option value="ALL">全部</option>
        {#each AUDIT_ENTITY_TYPE_OPTIONS as option}
          <option value={option}>{option}</option>
        {/each}
      </select>
    </FormField>
    <FormField label="實體 ID">
      <input
        class="rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm"
        bind:value={filters.entityId}
      />
    </FormField>
    <FormField label="關聯 ID">
      <input
        class="rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm"
        bind:value={filters.correlationId}
      />
    </FormField>
    <FormField label="起始日">
      <DateInput bind:value={filters.occurredFromDate} />
    </FormField>
    <FormField label="結束日">
      <DateInput bind:value={filters.occurredToDate} />
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
<div class="grid gap-4 lg:grid-cols-[260px_1fr]">
  <aside class="grid gap-3">
    <Card title="動作分佈" description={actionFacets.length === 0 ? "當前結果沒有資料" : `Top ${actionFacets.length}`}>
      <ul class="grid gap-1">
        {#each actionFacets as facet (facet.label)}
          <li>
            <button
              type="button"
              class={`flex w-full items-center justify-between gap-2 rounded-md px-2 py-1.5 text-left text-xs hover:bg-slate-100 ${filters.action === facet.label ? "bg-cyan-50 font-semibold text-cyan-800" : "text-slate-700"}`}
              onclick={() => {
                filters.action = facet.label as AuditAction;
                void refresh();
              }}
              title={facet.label}
            >
              <span class="truncate">{facet.label}</span>
              <span class="shrink-0 rounded-full bg-slate-200 px-1.5 text-[10px] font-semibold text-slate-700">{facet.count}</span>
            </button>
          </li>
        {/each}
      </ul>
    </Card>
    <Card title="實體類型分佈">
      <ul class="grid gap-1">
        {#each entityTypeFacets as facet (facet.label)}
          <li>
            <button
              type="button"
              class={`flex w-full items-center justify-between gap-2 rounded-md px-2 py-1.5 text-left text-xs hover:bg-slate-100 ${filters.entityType === facet.label ? "bg-cyan-50 font-semibold text-cyan-800" : "text-slate-700"}`}
              onclick={() => {
                filters.entityType = facet.label as AuditEntityType;
                void refresh();
              }}
            >
              <span class="truncate">{facet.label}</span>
              <span class="shrink-0 rounded-full bg-slate-200 px-1.5 text-[10px] font-semibold text-slate-700">{facet.count}</span>
            </button>
          </li>
        {/each}
      </ul>
    </Card>
    <Card title="Top Actor">
      <ul class="grid gap-1">
        {#each actorFacets as facet (facet.label)}
          <li>
            <button
              type="button"
              class={`flex w-full items-center justify-between gap-2 rounded-md px-2 py-1.5 text-left text-xs hover:bg-slate-100 ${filters.actorId === facet.label ? "bg-cyan-50 font-semibold text-cyan-800" : "text-slate-700"}`}
              onclick={() => {
                filters.actorId = facet.label;
                void refresh();
              }}
              title={facet.label}
            >
              <span class="truncate font-mono">{facet.label}</span>
              <span class="shrink-0 rounded-full bg-slate-200 px-1.5 text-[10px] font-semibold text-slate-700">{facet.count}</span>
            </button>
          </li>
        {/each}
      </ul>
    </Card>
    {#if filters.action !== "ALL" || filters.entityType !== "ALL" || filters.actorId !== ""}
      <Button
        variant="ghost"
        size="sm"
        onclick={() => {
          filters.action = "ALL";
          filters.entityType = "ALL";
          filters.actorId = "";
          void refresh();
        }}
      >
        清除 facet 篩選
      </Button>
    {/if}
  </aside>
  <Card title="稽核事件" description={loading ? "載入中..." : `共 ${items.length} 筆`}>
    <DataTable
      rows={items}
      {columns}
      emptyLabel={loading ? "載入中..." : "沒有符合條件的事件"}
    >
      {#snippet row(event: AuditEvidenceView)}
        <tr class="hover:bg-slate-50">
          <td class="px-3 py-2 text-xs text-slate-700">
            {formatTaipeiDateTime(event.occurredAt)}
          </td>
          <td class="px-3 py-2 font-mono text-[11px]">
            {event.actorId}
            <p class="text-[10px] text-slate-500">
              {event.actorRole} · {event.authenticationSource}
            </p>
          </td>
          <td class="px-3 py-2 text-xs font-semibold text-slate-800">{event.action}</td>
          <td class="px-3 py-2 text-xs">
            {event.entityType}
            <p class="font-mono text-[10px] text-slate-500">{event.entityId}</p>
          </td>
          <td class="px-3 py-2 text-xs text-slate-700">{event.reason}</td>
          <td class="px-3 py-2 font-mono text-[10px] text-slate-500">{event.correlationId}</td>
        </tr>
      {/snippet}
    </DataTable>
  </Card>
</div>
{/if}
