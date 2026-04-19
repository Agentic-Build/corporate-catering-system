<script lang="ts">
  import { PageHeader, Card, Button, FormField, toasts } from "$lib/components/ui";
  import { todayTaipeiIsoDate } from "$lib/admin/portal";
  import { apiClient } from "$lib/platform/api";
  import { configureAdminApi, describeApiError } from "$lib/admin/api";

  import type { PageData } from "./$types";

  let { data }: { data: PageData } = $props();

  let runDate = $state(todayTaipeiIsoDate());
  let dryRun = $state(false);
  let running = $state(false);
  let result = $state<Awaited<
    ReturnType<typeof apiClient.admin.runVendorComplianceLifecycle>
  > | null>(null);

  async function submit(event: SubmitEvent) {
    event.preventDefault();
    running = true;
    try {
      configureAdminApi(data.auth.apiBearerToken);
      result = await apiClient.admin.runVendorComplianceLifecycle({
        runDate,
        dryRun
      });
      toasts.success(
        `生命週期執行完成：提醒 ${result.reminderCount}、停權 ${result.suspensionCount}、復權 ${result.reinstatementCount}。`
      );
    } catch (error) {
      toasts.error(describeApiError(error));
    } finally {
      running = false;
    }
  }
</script>

<PageHeader
  eyebrow="合規治理"
  title="執行合規生命週期"
  description="自動發送提醒、停權逾期、復權補件完成的商家。"
  breadcrumbs={data.breadcrumbs}
/>

<Card title="執行設定" description="建議在每日固定時段執行；可先用 dry run 檢查影響範圍。">
  <form class="grid gap-3" onsubmit={submit}>
    <div class="grid gap-3 md:grid-cols-3">
      <FormField label="runDate（台北日期）" required>
        <input
          type="date"
          class="rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm"
          bind:value={runDate}
        />
      </FormField>
      <div class="flex items-end">
        <label class="flex items-center gap-2 text-sm text-slate-800">
          <input type="checkbox" bind:checked={dryRun} />
          dry run
        </label>
      </div>
      <div class="flex items-end">
        <Button type="submit" variant="primary" loading={running}>執行</Button>
      </div>
    </div>
  </form>
</Card>

{#if result}
  <Card title="最近一次執行結果" variant="success">
    <dl class="grid gap-2 text-sm text-slate-700 md:grid-cols-4">
      <div>
        <dt class="text-xs text-slate-500">runDate</dt>
        <dd class="font-medium">{result.runDate}</dd>
      </div>
      <div>
        <dt class="text-xs text-slate-500">提醒數 (remindersSent)</dt>
        <dd class="font-medium">{result.reminderCount}</dd>
      </div>
      <div>
        <dt class="text-xs text-slate-500">停權 (suspended)</dt>
        <dd class="font-medium text-rose-700">{result.suspensionCount}</dd>
      </div>
      <div>
        <dt class="text-xs text-slate-500">復權 (reinstated)</dt>
        <dd class="font-medium text-emerald-700">{result.reinstatementCount}</dd>
      </div>
    </dl>
  </Card>
{/if}
