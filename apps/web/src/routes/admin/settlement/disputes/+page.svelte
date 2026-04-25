<script lang="ts">
  import { onMount, untrack } from "svelte";

  import {
    PageHeader,
    Card,
    Button,
    DataTable,
    EmptyState,
    FormField,
    StateTag
  } from "$lib/components/ui";
  import { configureAdminApi, describeApiError } from "$lib/admin/api";
  import { apiClient } from "$lib/platform/api";
  import {
    friendlyDisputeStatus,
    disputeStatusTone,
    maskIdentifier
  } from "$lib/platform/labels";

  import type { PageData } from "./$types";

  let { data }: { data: PageData } = $props();

  type PayrollDispute = Awaited<
    ReturnType<typeof apiClient.admin.listPayrollDisputes>
  >["items"][number];
  type PayrollDisputeStatus = PayrollDispute["status"];

  const statusTabs: Array<{ value: "ALL" | PayrollDisputeStatus; label: string }> = [
    { value: "ALL", label: "全部" },
    { value: "OPEN", label: "已提交" },
    { value: "IN_REVIEW", label: "審查中" },
    { value: "RESOLVED_REFUND_APPROVED", label: "已退款" },
    { value: "RESOLVED_REJECTED", label: "已駁回" }
  ];

  let disputes = $state<PayrollDispute[]>([]);
  let totalItems = $state(0);
  let loading = $state(true);
  let loadError = $state<string | null>(null);
  let statusFilter = $state<"ALL" | PayrollDisputeStatus>("ALL");
  let manualDisputeId = $state("");

  const columns = [
    { id: "disputeId", label: "爭議 ID", width: "22%" },
    { id: "orderId", label: "訂單", width: "18%" },
    { id: "status", label: "狀態", width: "14%" },
    { id: "owner", label: "負責人", width: "18%" },
    { id: "openedAt", label: "提交時間", width: "18%" },
    { id: "action", label: "動作", width: "10%" }
  ];

  onMount(() => {
    void refresh();
  });

  // Live refetch on status filter change.
  $effect(() => {
    void statusFilter;
    untrack(() => {
      void refresh();
    });
  });

  async function refresh() {
    loading = true;
    loadError = null;
    try {
      configureAdminApi(data.auth.apiBearerToken);
      const response = await apiClient.admin.listPayrollDisputes(
        statusFilter === "ALL" ? undefined : statusFilter,
        1,
        100
      );
      disputes = [...response.items];
      totalItems = response.totalItems;
    } catch (error) {
      loadError = describeApiError(error);
    } finally {
      loading = false;
    }
  }

  function buildHref(disputeId: string): string {
    return `/admin/settlement/disputes/${encodeURIComponent(disputeId)}`;
  }
</script>

<PageHeader
  eyebrow="月結作業"
  title="爭議列表"
  description="所有待辦 / 進行中 / 已結案爭議，伺服器為權威來源。"
  breadcrumbs={data.breadcrumbs}
/>

<Card title="狀態">
  <div class="flex flex-wrap gap-1" role="tablist" aria-label="爭議狀態">
    {#each statusTabs as tab}
      {@const active = statusFilter === tab.value}
      <button
        type="button"
        role="tab"
        aria-selected={active}
        class={`inline-flex items-center gap-1.5 rounded-full border px-3 py-1.5 text-sm font-medium transition ${active ? "border-cyan-700 bg-cyan-50 text-cyan-900" : "border-slate-200 bg-white text-slate-700 hover:border-slate-400"}`}
        onclick={() => (statusFilter = tab.value)}
      >
        {tab.label}
      </button>
    {/each}
  </div>
</Card>

<Card title="查找特定爭議">
  <form
    class="grid gap-3 md:grid-cols-[1fr_auto]"
    onsubmit={(event) => {
      event.preventDefault();
      const id = manualDisputeId.trim();
      if (id.length > 0) {
        window.location.href = buildHref(id);
      }
    }}
  >
    <FormField label="disputeId">
      <input
        class="rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm"
        bind:value={manualDisputeId}
        placeholder="例：dsp-0123456789abcdef"
      />
    </FormField>
    <div class="flex items-end">
      <Button type="submit" variant="primary">進入處理</Button>
    </div>
  </form>
</Card>

{#if loadError}
  <Card variant="danger" title="載入失敗">
    <p class="text-sm text-rose-900">{loadError}</p>
  </Card>
{:else if loading}
  <Card title="同步中">
    <p class="text-sm text-slate-600">載入爭議中...</p>
  </Card>
{:else if disputes.length === 0}
  <Card title="爭議列表">
    <EmptyState
      title="尚無符合條件的爭議"
      description={statusFilter === "ALL"
        ? "目前系統中沒有已提交的爭議；員工在訂單詳情提交申訴後會在此出現。"
        : "此狀態下沒有爭議。切換其他狀態或選『全部』看完整清單。"}
    />
  </Card>
{:else}
  <Card title={`爭議列表（共 ${totalItems} 筆）`}>
    <DataTable rows={disputes} {columns}>
      {#snippet row(dispute: PayrollDispute)}
        <tr class="hover:bg-slate-50">
          <td class="px-3 py-2 font-mono text-xs" title={dispute.disputeId}>
            <a
              class="text-cyan-700 hover:text-cyan-900"
              href={buildHref(dispute.disputeId)}
            >
              {maskIdentifier(dispute.disputeId, 10)}
            </a>
          </td>
          <td class="px-3 py-2 font-mono text-xs">{maskIdentifier(dispute.orderId, 8)}</td>
          <td class="px-3 py-2">
            <StateTag
              label={friendlyDisputeStatus(dispute.status)}
              tone={disputeStatusTone(dispute.status)}
            />
          </td>
          <td class="px-3 py-2 text-xs text-slate-600">{maskIdentifier(dispute.ownerActorId, 6)}</td>
          <td class="px-3 py-2 text-xs text-slate-600">{dispute.openedAt}</td>
          <td class="px-3 py-2">
            <Button variant="secondary" size="sm" href={buildHref(dispute.disputeId)}>
              處理
            </Button>
          </td>
        </tr>
      {/snippet}
    </DataTable>
  </Card>
{/if}
