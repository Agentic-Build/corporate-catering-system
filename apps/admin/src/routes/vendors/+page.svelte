<script lang="ts">
  import { PageHeader, Card, StateTag, Button, Icon } from "@tbite/ui";
  let { data, form } = $props();

  const filters = [
    { id: "", label: "全部" },
    { id: "pending", label: "待審" },
    { id: "approved", label: "已核准" },
    { id: "suspended", label: "停權中" },
  ];

  const statusTone = {
    approved: "success",
    pending: "warning",
    suspended: "danger",
    terminated: "neutral",
  } as Record<string, "info" | "neutral" | "warning" | "danger" | "success">;
  const statusLabel = {
    approved: "已核准",
    pending: "待審",
    suspended: "停權",
    terminated: "已終止",
  } as Record<string, string>;

  const fmtPlants = (ids: string[]) => ids.join(" · ");
</script>

<PageHeader
  eyebrow="商家治理"
  title="商家管理"
  subtitle="入駐審核、廠區配對與停復權 · 操作會自動寫入稽核日誌"
/>

<div
  class="no-scrollbar flex items-center gap-1 overflow-x-auto rounded-full bg-tb-slate-100 p-1 md:flex-wrap"
>
  {#each filters as f}
    <a
      href={f.id ? `?status=${f.id}` : "?"}
      class="flex-shrink-0 whitespace-nowrap rounded-full px-3.5 py-1.5 text-xs font-semibold transition {data.status ===
      f.id
        ? 'bg-tb-slate-900 text-white'
        : 'text-tb-slate-700 hover:bg-tb-slate-200'}"
    >
      {f.label}
    </a>
  {/each}
</div>

<details class="mt-4 rounded-tb-2xl border border-tb-slate-200 bg-white p-4 shadow-tb-sm md:p-5">
  <summary class="cursor-pointer text-sm font-semibold text-tb-slate-900">
    新增 Pending 商家
  </summary>
  <form method="POST" action="?/create" class="mt-3 grid grid-cols-1 gap-2 sm:grid-cols-3">
    <input
      name="display_name"
      placeholder="顯示名稱"
      aria-label="顯示名稱"
      required
      class="rounded-lg border border-tb-slate-300 px-3 py-2 text-sm focus:border-tb-slate-500 focus:outline-none focus:ring-2 focus:ring-tb-slate-300"
    />
    <input
      name="legal_name"
      placeholder="法人名稱"
      aria-label="法人名稱"
      required
      class="rounded-lg border border-tb-slate-300 px-3 py-2 text-sm focus:border-tb-slate-500 focus:outline-none focus:ring-2 focus:ring-tb-slate-300"
    />
    <input
      name="contact_email"
      placeholder="contact@example.com"
      aria-label="聯絡 Email"
      type="email"
      required
      class="rounded-lg border border-tb-slate-300 px-3 py-2 text-sm font-jetbrains-mono focus:border-tb-slate-500 focus:outline-none focus:ring-2 focus:ring-tb-slate-300"
    />
    <div class="sm:col-span-3">
      <Button variant="primary" size="md" type="submit">
        <Icon name="plus" class="h-3.5 w-3.5" />建立商家
      </Button>
    </div>
  </form>
  {#if form?.error}<p class="mt-2 text-xs text-tb-rose-700">{form.error}</p>{/if}
</details>

<div class="mt-4">
  {#if data.vendors.length === 0}
    <p
      class="rounded-tb-2xl border border-dashed border-tb-slate-300 bg-tb-slate-50/60 p-8 text-center text-sm text-tb-slate-500"
    >
      尚無符合條件的商家
    </p>
  {:else}
    <Card>
      <!-- Mobile: card list (mockup style). -->
      <div class="divide-y divide-tb-slate-100 md:hidden">
        {#each data.vendors as v (v.id)}
          <div class="py-3 first:pt-0 last:pb-0">
            <div class="flex items-start justify-between gap-2">
              <div class="font-semibold text-tb-slate-900">{v.display_name}</div>
              <a
                href="/vendors/{v.id}"
                class="flex-shrink-0 text-sm font-semibold text-tb-red-600 hover:text-tb-red-700"
                >詳細</a
              >
            </div>
            <div class="mt-1 break-all font-jetbrains-mono text-xs text-tb-slate-500">
              {v.contact_email}
            </div>
            <div class="mt-2 flex items-center justify-between gap-2">
              <StateTag tone={statusTone[v.status] ?? "neutral"}>
                {statusLabel[v.status] ?? v.status}
              </StateTag>
              <div class="text-right text-xs text-tb-slate-500">
                {fmtPlants(v.plants ?? []) || "—"}
              </div>
            </div>
          </div>
        {/each}
      </div>

      <!-- Desktop: table (unchanged). -->
      <div class="hidden overflow-hidden rounded-xl border border-tb-slate-200 md:block">
        <table class="w-full text-sm">
          <thead
            class="bg-tb-slate-50/60 text-left text-[11px] font-bold uppercase tracking-wider text-tb-slate-500"
          >
            <tr>
              <th scope="col" class="px-4 py-2.5">商家</th>
              <th scope="col" class="px-4 py-2.5">Email</th>
              <th scope="col" class="px-4 py-2.5">狀態</th>
              <th scope="col" class="px-4 py-2.5">服務廠區</th>
              <th scope="col" class="px-4 py-2.5"></th>
            </tr>
          </thead>
          <tbody class="divide-y divide-tb-slate-100">
            {#each data.vendors as v (v.id)}
              <tr class="hover:bg-tb-slate-50/60">
                <td class="px-4 py-3 font-semibold text-tb-slate-900">{v.display_name}</td>
                <td class="px-4 py-3 font-jetbrains-mono text-xs text-tb-slate-500">
                  {v.contact_email}
                </td>
                <td class="px-4 py-3">
                  <StateTag tone={statusTone[v.status] ?? "neutral"}>
                    {statusLabel[v.status] ?? v.status}
                  </StateTag>
                </td>
                <td class="px-4 py-3 text-xs text-tb-slate-500">
                  {fmtPlants(v.plants ?? []) || "—"}
                </td>
                <td class="px-4 py-3 text-right">
                  <a
                    href="/vendors/{v.id}"
                    class="text-sm font-semibold text-tb-red-600 hover:text-tb-red-700">詳細</a
                  >
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    </Card>
  {/if}
</div>
