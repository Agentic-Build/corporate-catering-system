<script lang="ts">
  import { PageHeader, Card, Button, Icon } from "@tbite/ui";
  let { data, form } = $props();
</script>

<a
  href="/payroll"
  class="-mb-2 inline-flex items-center gap-1 text-xs font-semibold text-tb-slate-500 hover:text-tb-slate-700"
>
  <Icon name="chevron" class="h-3.5 w-3.5 rotate-90" />返回批次列表
</a>

<PageHeader
  eyebrow="薪資代扣"
  title="建立月結批次"
  subtitle="指定期間後，系統會聚合該期間內 picked_up 與 no_show 的訂單為草稿批次"
/>

<div class="grid max-w-xl gap-6">
  {#if form?.error}
    <p class="rounded-lg bg-tb-rose-50 px-3 py-2 text-sm text-tb-rose-700">{form.error}</p>
  {/if}

  <Card title="月結期間">
    <form method="POST" class="space-y-4">
      <div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
        <label class="flex flex-col gap-1">
          <span class="text-[11px] font-bold uppercase tracking-eyebrow text-tb-slate-500"
            >起始日</span
          >
          <input
            name="period_start"
            type="date"
            required
            value={data.defaultStart}
            class="rounded-lg border border-tb-slate-300 px-3 py-2 text-sm text-tb-slate-900 focus:border-tb-slate-500 focus:outline-none focus:ring-2 focus:ring-tb-slate-300"
          />
        </label>
        <label class="flex flex-col gap-1">
          <span class="text-[11px] font-bold uppercase tracking-eyebrow text-tb-slate-500"
            >結束日</span
          >
          <input
            name="period_end"
            type="date"
            required
            value={data.defaultEnd}
            class="rounded-lg border border-tb-slate-300 px-3 py-2 text-sm text-tb-slate-900 focus:border-tb-slate-500 focus:outline-none focus:ring-2 focus:ring-tb-slate-300"
          />
        </label>
      </div>
      <Button variant="primary" size="md" type="submit">
        <Icon name="plus" class="h-3.5 w-3.5" />建立草稿批次
      </Button>
    </form>
  </Card>
</div>
