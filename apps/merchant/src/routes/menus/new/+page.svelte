<script lang="ts">
  let { form } = $props();
</script>

<section class="max-w-xl space-y-4">
  <h1 class="text-2xl font-black text-tb-slate-900">新增餐點</h1>

  <form method="POST" class="space-y-3 rounded-tb-2xl border border-tb-slate-200 bg-white p-5 shadow-tb-sm">
    <label class="block text-sm font-semibold">名稱
      <input name="name" required class="mt-1 w-full rounded-lg border border-tb-slate-300 px-3 py-2 text-sm" />
    </label>
    <label class="block text-sm font-semibold">敘述
      <textarea name="description" rows="2" class="mt-1 w-full rounded-lg border border-tb-slate-300 px-3 py-2 text-sm"></textarea>
    </label>
    <label class="block text-sm font-semibold">價格（NTD）
      <input name="price" type="number" min="0" required class="mt-1 w-full rounded-lg border border-tb-slate-300 px-3 py-2 font-jetbrains-mono tabular-nums text-sm" />
    </label>
    <label class="block text-sm font-semibold">標籤（逗號分隔）
      <input name="tags" placeholder="hot, healthy" class="mt-1 w-full rounded-lg border border-tb-slate-300 px-3 py-2 text-sm" />
    </label>
    <label class="block text-sm font-semibold">徽章（逗號分隔）
      <input name="badges" placeholder="可薪資代扣, 低於 500 kcal" class="mt-1 w-full rounded-lg border border-tb-slate-300 px-3 py-2 text-sm" />
    </label>

    {#if form?.error}
      <p class="rounded-lg bg-tb-rose-50 px-3 py-2 text-sm text-tb-rose-700">{form.error}</p>
    {/if}

    <div class="flex gap-2">
      <button type="submit" class="rounded-lg bg-tb-red-600 px-3.5 py-2 text-sm font-semibold text-white hover:bg-tb-red-700">建立</button>
      <a href="/menus" class="rounded-lg border border-tb-slate-300 px-3.5 py-2 text-sm font-semibold text-tb-slate-800 hover:border-tb-slate-500">取消</a>
    </div>
  </form>
</section>
