<script lang="ts">
  import { PageHeader, Card, Button } from "@tbite/ui";

  let { data, form } = $props();
</script>

<PageHeader
  eyebrow="Settings · 營運設定"
  title="營運設定"
  subtitle="設定截單時間與預購開放天數。截單時間用於計算每筆訂單可下單／修改的截止時刻。"
/>

<div class="max-w-xl">
  <Card>
    {#if form?.error}
      <p class="mb-3 rounded-tb-xl bg-tb-rose-50 px-3 py-2 text-sm text-tb-rose-700">
        {form.error}
      </p>
    {/if}
    {#if form?.ok}
      <p class="mb-3 rounded-tb-xl bg-tb-emerald-50 px-3 py-2 text-sm text-tb-emerald-700">
        設定已儲存。
      </p>
    {/if}
    <form method="POST" action="?/save" class="space-y-4">
      <label class="flex flex-col gap-1.5 text-sm">
        <span class="text-[11px] font-bold uppercase tracking-eyebrow text-tb-slate-500">
          截單時間（取餐日前一天的整點，0–23）
        </span>
        <input
          type="number"
          name="cutoff_hour"
          min="0"
          max="23"
          required
          value={data.settings.cutoff_hour}
          class="w-32 rounded-tb-lg border border-tb-slate-300 px-3 py-2 text-sm transition focus:border-tb-red-500 focus:outline-none focus:ring-4 focus:ring-tb-red-100"
        />
        <span class="text-xs text-tb-slate-500"> 例如填 17，表示取餐日前一天 17:00 截單。 </span>
      </label>
      <label class="flex flex-col gap-1.5 text-sm">
        <span class="text-[11px] font-bold uppercase tracking-eyebrow text-tb-slate-500">
          預購開放天數（1–30）
        </span>
        <input
          type="number"
          name="preorder_window_days"
          min="1"
          max="30"
          required
          value={data.settings.preorder_window_days}
          class="w-32 rounded-tb-lg border border-tb-slate-300 px-3 py-2 text-sm transition focus:border-tb-red-500 focus:outline-none focus:ring-4 focus:ring-tb-red-100"
        />
        <span class="text-xs text-tb-slate-500"> 員工最多可提前幾天向本商家預訂。 </span>
      </label>
      <Button variant="primary" size="md" type="submit">儲存設定</Button>
    </form>
  </Card>
</div>
