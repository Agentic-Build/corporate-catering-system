<script lang="ts">
  import { Button, PageHeader, Icon, EmptyState } from "@tbite/ui";
  let { data } = $props();
</script>

<PageHeader
  eyebrow="Menu Library · 菜單管理"
  title="菜單管理"
  subtitle="管理所有餐點 — 上架後即可在 7 天排程中安排供應。"
>
  {#snippet actions()}
    <a href="/menus/new">
      <Button variant="primary">
        <Icon name="plus" class="h-4 w-4" />新增餐點
      </Button>
    </a>
  {/snippet}
</PageHeader>

{#if data.items.length === 0}
  <EmptyState icon="doc" title="尚未建立任何餐點" hint="點「新增餐點」建立第一道菜色。" />
{:else}
  <div class="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
    {#each data.items as item (item.id)}
      <div
        class="flex flex-col overflow-hidden rounded-tb-2xl border border-tb-slate-200 bg-white shadow-tb-sm"
      >
        <!-- 主圖 -->
        {#if item.images?.[0]}
          <img src={item.images[0]} alt={item.name} class="aspect-video w-full object-cover" />
        {:else}
          <div class="flex aspect-video w-full items-center justify-center bg-tb-slate-100">
            <Icon name="tag" class="h-10 w-10 text-tb-slate-300" />
          </div>
        {/if}

        <!-- 名稱 + 價格 -->
        <div class="flex flex-1 flex-col gap-1 px-4 py-3">
          <div class="font-semibold text-tb-slate-900">{item.name}</div>
          <div class="font-jetbrains-mono tabular-nums text-tb-slate-700">
            ${item.price_minor.toLocaleString()}
          </div>
        </div>

        <!-- 操作列 -->
        <div class="flex items-center justify-end gap-2 border-t border-tb-slate-100 px-4 py-2">
          <form method="POST" action="?/copy">
            <input type="hidden" name="id" value={item.id} />
            <button
              type="submit"
              class="text-sm font-semibold text-tb-slate-500 hover:text-tb-slate-800"
            >
              複製
            </button>
          </form>
          <a
            href="/menus/{item.id}"
            class="text-sm font-semibold text-tb-red-600 hover:text-tb-red-700"
          >
            編輯
          </a>
          <form method="POST" action="?/delete">
            <input type="hidden" name="id" value={item.id} />
            <button
              type="submit"
              class="text-sm font-semibold text-tb-slate-400 hover:text-red-600"
            >
              刪除
            </button>
          </form>
        </div>
      </div>
    {/each}
  </div>
{/if}
