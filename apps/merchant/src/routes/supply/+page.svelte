<script lang="ts">
  let { data, form } = $props();
  const supplyByItem = $derived(Object.fromEntries(data.supplies.map((s: any) => [s.menu_item_id, s])));
</script>

<section class="space-y-4">
  <header class="flex items-end justify-between gap-3">
    <div>
      <h1 class="text-2xl font-black text-tb-slate-900">每日份數</h1>
      <p class="mt-1 text-sm text-tb-slate-500">設定 {data.date} 的供應量</p>
    </div>
    <div class="flex flex-wrap gap-1 rounded-full bg-tb-slate-100 p-1">
      {#each data.days as d}
        <a href="?date={d.id}" class="rounded-full px-3 py-1 text-xs font-semibold {data.date === d.id ? 'bg-tb-slate-900 text-white' : 'text-tb-slate-700'}">{d.label}</a>
      {/each}
    </div>
  </header>

  {#if form?.error}<p class="rounded-lg bg-tb-rose-50 px-3 py-2 text-sm text-tb-rose-700">{form.error}</p>{/if}
  {#if form?.success}<p class="rounded-lg bg-emerald-50 px-3 py-2 text-sm text-emerald-700">已更新</p>{/if}

  {#if data.items.length === 0}
    <p class="rounded-tb-2xl border border-tb-slate-200 bg-white p-6 text-center text-sm text-tb-slate-500">
      尚無上架中的餐點。先到「菜單管理」建立並上架。
    </p>
  {:else}
    <div class="overflow-hidden rounded-tb-2xl border border-tb-slate-200 bg-white shadow-tb-sm">
      <table class="w-full text-sm">
        <thead class="bg-tb-slate-50 text-left text-xs uppercase tracking-eyebrow text-tb-slate-500">
          <tr><th class="px-4 py-2">餐點</th><th class="px-4 py-2 text-right">已賣 / 容量</th><th class="px-4 py-2 text-right">領餐區間</th><th class="px-4 py-2"></th></tr>
        </thead>
        <tbody>
          {#each data.items as item (item.id)}
            {@const s = supplyByItem[item.id]}
            <tr class="border-t border-tb-slate-100">
              <td class="px-4 py-3 font-semibold text-tb-slate-900">{item.name}</td>
              <td class="px-4 py-3 text-right font-jetbrains-mono tabular-nums">
                {s ? `${s.capacity - s.remain} / ${s.capacity}` : "-"}
              </td>
              <td class="px-4 py-3 text-right">{s?.pickup_window ?? "11:50-12:10"}</td>
              <td class="px-4 py-3">
                <form method="POST" action="?/set" class="flex items-center gap-2 justify-end">
                  <input type="hidden" name="item_id" value={item.id} />
                  <input type="hidden" name="date" value={data.date} />
                  <input type="number" name="capacity" min="0" value={s?.capacity ?? 80}
                    class="w-20 rounded-lg border border-tb-slate-300 px-2 py-1 font-jetbrains-mono tabular-nums text-right text-sm" />
                  <input type="hidden" name="pickup_window" value={s?.pickup_window ?? "11:50-12:10"} />
                  <input type="hidden" name="cutoff_at" value={`${data.date}T17:00:00Z`} />
                  <button class="rounded-lg bg-tb-slate-900 px-3 py-1 text-xs font-semibold text-white">儲存</button>
                </form>
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  {/if}
</section>
