<script lang="ts">
  import { PageHeader, Card, Button, Icon } from "@tbite/ui";
  let { data, form } = $props();

  let editing = $state<string | null>(null);
</script>

<PageHeader
  eyebrow="廠區登錄表"
  title="廠區管理"
  subtitle="管理廠區清單，員工與商家依此清單選擇領餐地點。"
/>

{#if form?.error}
  <p class="mb-4 rounded-tb-xl bg-tb-rose-50 px-3 py-2 text-sm text-tb-rose-700">{form.error}</p>
{/if}
{#if form?.ok}
  <p class="mb-4 rounded-tb-xl bg-tb-emerald-50 px-3 py-2 text-sm text-tb-emerald-700">
    操作成功。
  </p>
{/if}

<details class="mb-6 rounded-tb-2xl border border-tb-slate-200 bg-white p-4 shadow-tb-sm md:p-5">
  <summary class="cursor-pointer text-sm font-semibold text-tb-slate-900"> 新增廠區 </summary>
  <form method="POST" action="?/create" class="mt-3 grid grid-cols-1 gap-2 sm:grid-cols-4">
    <input
      name="code"
      placeholder="代碼 (如 tn-a)"
      required
      class="rounded-lg border border-tb-slate-300 px-3 py-2 text-sm font-jetbrains-mono focus:border-tb-slate-500 focus:outline-none focus:ring-2 focus:ring-tb-slate-300"
    />
    <input
      name="label"
      placeholder="名稱 (如 台南廠 A 區)"
      required
      class="rounded-lg border border-tb-slate-300 px-3 py-2 text-sm focus:border-tb-slate-500 focus:outline-none focus:ring-2 focus:ring-tb-slate-300"
    />
    <input
      name="address"
      placeholder="地址"
      class="rounded-lg border border-tb-slate-300 px-3 py-2 text-sm focus:border-tb-slate-500 focus:outline-none focus:ring-2 focus:ring-tb-slate-300"
    />
    <input
      name="sort_order"
      type="number"
      placeholder="排序"
      value="0"
      class="rounded-lg border border-tb-slate-300 px-3 py-2 text-sm focus:border-tb-slate-500 focus:outline-none focus:ring-2 focus:ring-tb-slate-300"
    />
    <div class="sm:col-span-4">
      <Button variant="primary" size="md" type="submit">
        <Icon name="plus" class="h-3.5 w-3.5" />建立廠區
      </Button>
    </div>
  </form>
</details>

<Card>
  <div class="overflow-x-auto rounded-xl border border-tb-slate-200">
    <table class="w-full min-w-[40rem] text-sm">
      <thead
        class="bg-tb-slate-50 text-left text-[11px] font-bold uppercase tracking-wider text-tb-slate-500"
      >
        <tr>
          <th class="px-4 py-2.5">代碼</th>
          <th class="px-4 py-2.5">名稱</th>
          <th class="px-4 py-2.5">地址</th>
          <th class="px-4 py-2.5">排序</th>
          <th class="px-4 py-2.5">狀態</th>
          <th class="px-4 py-2.5"></th>
        </tr>
      </thead>
      <tbody class="divide-y divide-tb-slate-100">
        {#each data.plants as p (p.code)}
          {#if editing === p.code}
            <tr class="bg-tb-slate-50/60">
              <td class="px-4 py-3 font-jetbrains-mono text-xs text-tb-slate-700">{p.code}</td>
              <td colspan="5" class="px-4 py-3">
                <form method="POST" action="?/update" class="flex flex-wrap items-center gap-2">
                  <input type="hidden" name="code" value={p.code} />
                  <input
                    name="label"
                    value={p.label}
                    required
                    class="w-36 rounded-lg border border-tb-slate-300 px-2 py-1.5 text-sm focus:border-tb-red-500 focus:outline-none focus:ring-4 focus:ring-tb-red-100"
                  />
                  <input
                    name="address"
                    value={p.address}
                    placeholder="地址"
                    class="flex-1 min-w-32 rounded-lg border border-tb-slate-300 px-2 py-1.5 text-sm focus:border-tb-red-500 focus:outline-none focus:ring-4 focus:ring-tb-red-100"
                  />
                  <input
                    name="sort_order"
                    type="number"
                    value={p.sort_order}
                    class="w-20 rounded-lg border border-tb-slate-300 px-2 py-1.5 text-sm focus:border-tb-red-500 focus:outline-none focus:ring-4 focus:ring-tb-red-100"
                  />
                  <select
                    name="active"
                    class="rounded-lg border border-tb-slate-300 px-2 py-1.5 text-sm focus:border-tb-red-500 focus:outline-none"
                  >
                    <option value="true" selected={p.active}>啟用</option>
                    <option value="false" selected={!p.active}>停用</option>
                  </select>
                  <Button variant="primary" size="sm" type="submit">儲存</Button>
                  <button
                    type="button"
                    onclick={() => (editing = null)}
                    class="text-xs text-tb-slate-500 hover:text-tb-slate-700"
                  >
                    取消
                  </button>
                </form>
              </td>
            </tr>
          {:else}
            <tr class="hover:bg-tb-slate-50/60">
              <td class="px-4 py-3 font-jetbrains-mono text-xs text-tb-slate-700">{p.code}</td>
              <td class="px-4 py-3 font-semibold text-tb-slate-900">{p.label}</td>
              <td class="px-4 py-3 text-tb-slate-600">{p.address || "—"}</td>
              <td class="px-4 py-3 text-tb-slate-500">{p.sort_order}</td>
              <td class="px-4 py-3">
                {#if p.active}
                  <span
                    class="rounded-full bg-tb-emerald-100 px-2 py-0.5 text-xs font-semibold text-tb-emerald-700"
                    >啟用</span
                  >
                {:else}
                  <span
                    class="rounded-full bg-tb-slate-200 px-2 py-0.5 text-xs font-semibold text-tb-slate-600"
                    >停用</span
                  >
                {/if}
              </td>
              <td class="px-4 py-3 text-right">
                <button
                  type="button"
                  onclick={() => (editing = p.code)}
                  class="text-sm font-semibold text-tb-red-600 hover:text-tb-red-700"
                >
                  編輯
                </button>
              </td>
            </tr>
          {/if}
        {:else}
          <tr>
            <td colspan="6" class="px-4 py-6 text-sm text-tb-slate-500">尚無廠區</td>
          </tr>
        {/each}
      </tbody>
    </table>
  </div>
</Card>
