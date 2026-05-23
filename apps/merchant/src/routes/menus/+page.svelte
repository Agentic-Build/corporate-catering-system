<script lang="ts">
  import { StateTag, Button, PageHeader, Icon, EmptyState } from "@tbite/ui";
  let { data } = $props();

  const statusMeta = {
    active: { tone: "success", label: "上架中" },
    draft: { tone: "neutral", label: "草稿" },
    archived: { tone: "warning", label: "已封存" },
  } as Record<string, { tone: "success" | "neutral" | "warning"; label: string }>;
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

<div class="mb-4 flex gap-2 text-xs">
  <a
    href="/menus"
    class="rounded-full px-3 py-1 font-semibold {!data.includeArchived
      ? 'bg-tb-slate-900 text-white'
      : 'bg-tb-slate-100 text-tb-slate-700 hover:text-tb-slate-900'}"
  >
    上架中
  </a>
  <a
    href="/menus?archived=1"
    class="rounded-full px-3 py-1 font-semibold {data.includeArchived
      ? 'bg-tb-slate-900 text-white'
      : 'bg-tb-slate-100 text-tb-slate-700 hover:text-tb-slate-900'}"
  >
    含已封存
  </a>
</div>

{#if data.items.length === 0}
  <EmptyState icon="doc" title="尚未建立任何餐點" hint="點「新增餐點」建立第一道菜色。" />
{:else}
  <!-- Mobile: stacked cards -->
  <div class="space-y-3 md:hidden">
    {#each data.items as item (item.id)}
      {@const meta = statusMeta[item.status] ?? { tone: "neutral", label: item.status }}
      <div class="rounded-tb-2xl border border-tb-slate-200 bg-white p-4 shadow-tb-sm">
        <div class="mb-2 flex items-start justify-between gap-3">
          <div class="font-semibold text-tb-slate-900">{item.name}</div>
          <div class="font-jetbrains-mono tabular-nums font-bold text-tb-slate-900">
            ${item.price_minor.toLocaleString()}
          </div>
        </div>
        <div class="flex items-center justify-between">
          <StateTag tone={meta.tone}>{meta.label}</StateTag>
          <div class="flex items-center gap-3">
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
          </div>
        </div>
      </div>
    {/each}
  </div>

  <!-- Desktop: table -->
  <div
    class="hidden overflow-hidden rounded-tb-2xl border border-tb-slate-200 bg-white shadow-tb-sm md:block"
  >
    <table class="w-full text-sm">
      <thead
        class="bg-tb-slate-50/60 text-left text-[11px] font-bold uppercase tracking-eyebrow text-tb-slate-500"
      >
        <tr>
          <th class="px-5 py-3">名稱</th>
          <th class="px-3 py-3 text-right">價格</th>
          <th class="px-3 py-3">狀態</th>
          <th class="px-5 py-3"></th>
        </tr>
      </thead>
      <tbody class="divide-y divide-tb-slate-100">
        {#each data.items as item (item.id)}
          {@const meta = statusMeta[item.status] ?? { tone: "neutral", label: item.status }}
          <tr class="hover:bg-tb-slate-50/60">
            <td class="px-5 py-3 font-semibold text-tb-slate-900">{item.name}</td>
            <td class="px-3 py-3 text-right font-jetbrains-mono tabular-nums text-tb-slate-900">
              ${item.price_minor.toLocaleString()}
            </td>
            <td class="px-3 py-3">
              <StateTag tone={meta.tone}>{meta.label}</StateTag>
            </td>
            <td class="px-5 py-3 text-right">
              <div class="flex items-center justify-end gap-3">
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
              </div>
            </td>
          </tr>
        {/each}
      </tbody>
    </table>
  </div>
{/if}
