<script lang="ts">
  let { data, form } = $props();
  const item = data.item;
</script>

<section class="max-w-xl space-y-4">
  <h1 class="text-2xl font-black text-tb-slate-900">編輯餐點</h1>

  <form method="POST" action="?/update" class="space-y-3 rounded-tb-2xl border border-tb-slate-200 bg-white p-5 shadow-tb-sm">
    <label class="block text-sm font-semibold">名稱
      <input name="name" required value={item.name} class="mt-1 w-full rounded-lg border border-tb-slate-300 px-3 py-2 text-sm" />
    </label>
    <label class="block text-sm font-semibold">敘述
      <textarea name="description" rows="2" class="mt-1 w-full rounded-lg border border-tb-slate-300 px-3 py-2 text-sm">{item.description}</textarea>
    </label>
    <label class="block text-sm font-semibold">價格（NTD）
      <input name="price" type="number" min="0" required value={item.price_minor} class="mt-1 w-full rounded-lg border border-tb-slate-300 px-3 py-2 font-jetbrains-mono tabular-nums text-sm" />
    </label>
    <label class="block text-sm font-semibold">標籤（逗號分隔）
      <input name="tags" value={(item.tags ?? []).join(",")} class="mt-1 w-full rounded-lg border border-tb-slate-300 px-3 py-2 text-sm" />
    </label>
    <label class="block text-sm font-semibold">徽章（逗號分隔）
      <input name="badges" value={(item.badges ?? []).join(",")} class="mt-1 w-full rounded-lg border border-tb-slate-300 px-3 py-2 text-sm" />
    </label>
    {#if form?.error}
      <p class="rounded-lg bg-tb-rose-50 px-3 py-2 text-sm text-tb-rose-700">{form.error}</p>
    {/if}
    <button type="submit" class="rounded-lg bg-tb-red-600 px-3.5 py-2 text-sm font-semibold text-white hover:bg-tb-red-700">儲存</button>
  </form>

  <div class="flex gap-2">
    {#if item.status !== "active"}
      <form method="POST" action="?/publish"><button class="rounded-lg border border-emerald-500 bg-emerald-50 px-3.5 py-2 text-sm font-semibold text-emerald-700">上架</button></form>
    {/if}
    {#if item.status !== "archived"}
      <form method="POST" action="?/archive"><button class="rounded-lg border border-tb-rose-300 bg-tb-rose-50 px-3.5 py-2 text-sm font-semibold text-tb-rose-700">封存</button></form>
    {/if}
    <a href="/menus" class="rounded-lg border border-tb-slate-300 px-3.5 py-2 text-sm font-semibold text-tb-slate-800">返回</a>
  </div>
</section>
