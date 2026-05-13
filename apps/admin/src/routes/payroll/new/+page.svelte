<script lang="ts">
  import { Card } from "@tbite/ui";
  let { data, form } = $props();
</script>

<section class="max-w-xl space-y-4">
  <header>
    <a href="/payroll" class="text-xs text-tb-slate-500 hover:text-tb-slate-700">← 返回列表</a>
    <h1 class="mt-1 text-2xl font-black text-tb-slate-900">建立月結批次</h1>
    <p class="mt-1 text-sm text-tb-slate-500">指定期間後，系統會聚合該期間內 picked_up 與 no_show 的訂單為草稿批次。</p>
  </header>

  {#if form?.error}
    <p class="rounded-lg bg-tb-rose-50 px-3 py-2 text-sm text-tb-rose-700">{form.error}</p>
  {/if}

  <Card>
    <form method="POST" class="space-y-3">
      <div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
        <label class="flex flex-col gap-1 text-xs font-semibold uppercase tracking-eyebrow text-tb-slate-500">
          起始日
          <input
            name="period_start"
            type="date"
            required
            value={data.defaultStart}
            class="rounded-lg border border-tb-slate-300 px-3 py-2 text-sm font-normal normal-case tracking-normal text-tb-slate-900"
          />
        </label>
        <label class="flex flex-col gap-1 text-xs font-semibold uppercase tracking-eyebrow text-tb-slate-500">
          結束日
          <input
            name="period_end"
            type="date"
            required
            value={data.defaultEnd}
            class="rounded-lg border border-tb-slate-300 px-3 py-2 text-sm font-normal normal-case tracking-normal text-tb-slate-900"
          />
        </label>
      </div>
      <button class="rounded-lg bg-tb-red-600 px-3.5 py-2 text-sm font-semibold text-white hover:bg-tb-red-700">
        建立草稿
      </button>
    </form>
  </Card>
</section>
