<script lang="ts">
  import { StateTag, Button } from "@tbite/ui";
  let { data } = $props();
</script>

<section class="space-y-4">
  <header class="flex items-center justify-between">
    <h1 class="text-2xl font-black text-tb-slate-900">菜單管理</h1>
    <a href="/menus/new" class="rounded-lg bg-tb-red-600 px-3.5 py-2 text-sm font-semibold text-white hover:bg-tb-red-700">新增餐點</a>
  </header>

  <div class="flex gap-2 text-xs">
    <a href="/menus"
       class="rounded-full px-3 py-1 font-semibold {!data.includeArchived ? 'bg-tb-slate-900 text-white' : 'bg-tb-slate-100 text-tb-slate-700'}">
      上架中
    </a>
    <a href="/menus?archived=1"
       class="rounded-full px-3 py-1 font-semibold {data.includeArchived ? 'bg-tb-slate-900 text-white' : 'bg-tb-slate-100 text-tb-slate-700'}">
      含已封存
    </a>
  </div>

  {#if data.items.length === 0}
    <div class="rounded-tb-2xl border border-tb-slate-200 bg-white p-6 text-center text-sm text-tb-slate-500">
      尚未建立任何餐點
    </div>
  {:else}
    <div class="overflow-hidden rounded-tb-2xl border border-tb-slate-200 bg-white shadow-tb-sm">
      <table class="w-full text-sm">
        <thead class="bg-tb-slate-50 text-left text-xs uppercase tracking-eyebrow text-tb-slate-500">
          <tr><th class="px-4 py-2">名稱</th><th class="px-4 py-2">價格</th><th class="px-4 py-2">狀態</th><th class="px-4 py-2"></th></tr>
        </thead>
        <tbody>
          {#each data.items as item (item.id)}
            <tr class="border-t border-tb-slate-100">
              <td class="px-4 py-3 font-semibold text-tb-slate-900">{item.name}</td>
              <td class="px-4 py-3 font-jetbrains-mono tabular-nums">${item.price_minor.toLocaleString()}</td>
              <td class="px-4 py-3">
                {#if item.status === "active"}<StateTag tone="success">上架中</StateTag>
                {:else if item.status === "draft"}<StateTag tone="neutral">草稿</StateTag>
                {:else}<StateTag tone="warning">已封存</StateTag>{/if}
              </td>
              <td class="px-4 py-3 text-right">
                <a href="/menus/{item.id}" class="text-tb-red-600 hover:text-tb-red-700">編輯</a>
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  {/if}
</section>
