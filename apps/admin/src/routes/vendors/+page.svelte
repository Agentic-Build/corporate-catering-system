<script lang="ts">
  import { StateTag } from "@tbite/ui";
  let { data, form } = $props();

  const filters = [
    { id: "", label: "全部" },
    { id: "pending", label: "待審" },
    { id: "approved", label: "已核准" },
    { id: "suspended", label: "停權中" },
  ];
</script>

<section class="space-y-4">
  <header class="flex items-center justify-between">
    <h1 class="text-2xl font-black text-tb-slate-900">商家管理</h1>
  </header>

  <div class="flex flex-wrap gap-1 rounded-full bg-tb-slate-100 p-1">
    {#each filters as f}
      <a href={f.id ? `?status=${f.id}` : "?"}
         class="rounded-full px-3 py-1 text-xs font-semibold {data.status === f.id ? 'bg-tb-slate-900 text-white' : 'text-tb-slate-700'}">
        {f.label}
      </a>
    {/each}
  </div>

  <details class="rounded-tb-2xl border border-tb-slate-200 bg-white p-4 shadow-tb-sm">
    <summary class="cursor-pointer text-sm font-semibold">+ 新增 Pending 商家</summary>
    <form method="POST" action="?/create" class="mt-3 grid grid-cols-1 gap-2 sm:grid-cols-3">
      <input name="display_name" placeholder="顯示名稱" required class="rounded-lg border border-tb-slate-300 px-3 py-2 text-sm" />
      <input name="legal_name"   placeholder="法人名稱" required class="rounded-lg border border-tb-slate-300 px-3 py-2 text-sm" />
      <input name="contact_email" placeholder="contact@example.com" type="email" required class="rounded-lg border border-tb-slate-300 px-3 py-2 text-sm" />
      <button class="sm:col-span-3 rounded-lg bg-tb-slate-900 px-3.5 py-2 text-sm font-semibold text-white">建立</button>
    </form>
    {#if form?.error}<p class="mt-2 text-xs text-tb-rose-700">{form.error}</p>{/if}
  </details>

  {#if data.vendors.length === 0}
    <p class="rounded-tb-2xl border border-tb-slate-200 bg-white p-6 text-center text-sm text-tb-slate-500">
      尚無商家
    </p>
  {:else}
    <div class="overflow-hidden rounded-tb-2xl border border-tb-slate-200 bg-white shadow-tb-sm">
      <table class="w-full text-sm">
        <thead class="bg-tb-slate-50 text-left text-xs uppercase tracking-eyebrow text-tb-slate-500">
          <tr><th class="px-4 py-2">商家</th><th class="px-4 py-2">Email</th><th class="px-4 py-2">狀態</th><th class="px-4 py-2">廠區</th><th class="px-4 py-2"></th></tr>
        </thead>
        <tbody>
          {#each data.vendors as v (v.id)}
            <tr class="border-t border-tb-slate-100">
              <td class="px-4 py-3 font-semibold text-tb-slate-900">{v.display_name}</td>
              <td class="px-4 py-3 text-tb-slate-500 font-jetbrains-mono">{v.contact_email}</td>
              <td class="px-4 py-3">
                {#if v.status === "approved"}<StateTag tone="success">已核准</StateTag>
                {:else if v.status === "pending"}<StateTag tone="warning">待審</StateTag>
                {:else if v.status === "suspended"}<StateTag tone="danger">停權</StateTag>
                {:else}<StateTag tone="neutral">{v.status}</StateTag>{/if}
              </td>
              <td class="px-4 py-3 text-xs text-tb-slate-500">{(v.plants ?? []).join(", ") || "-"}</td>
              <td class="px-4 py-3 text-right">
                <a href="/vendors/{v.id}" class="text-tb-red-600 hover:text-tb-red-700">詳細</a>
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  {/if}
</section>
