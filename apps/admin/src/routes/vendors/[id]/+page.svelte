<script lang="ts">
  import { PageHeader, Card, StateTag, Button, Icon } from "@tbite/ui";
  let { data, form } = $props();
  const v = $derived(data.vendor);
  const currentPlants = $derived(new Set<string>(v.plants ?? []));

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
  const operatorLabel = {
    active: "啟用",
    suspended: "停用",
    vendor_suspended: "商家停權",
  } as Record<string, string>;
</script>

<a
  href="/vendors"
  class="-mb-2 inline-flex items-center gap-1 text-xs font-semibold text-tb-slate-500 hover:text-tb-slate-700"
>
  <Icon name="chevron" class="h-3.5 w-3.5 rotate-90" />返回商家列表
</a>

<PageHeader eyebrow="商家治理" title={v.display_name} subtitle={v.contact_email}>
  {#snippet actions()}
    <StateTag tone={statusTone[v.status] ?? "neutral"}>
      {statusLabel[v.status] ?? v.status}
    </StateTag>
    <a href={`/vendors/${v.id}/documents`}>
      <Button variant="secondary" size="sm">
        <Icon name="doc" class="h-3.5 w-3.5" />合規文件
      </Button>
    </a>
  {/snippet}
</PageHeader>

<div class="grid max-w-4xl gap-6">
  {#if form?.error}
    <p class="rounded-lg bg-tb-rose-50 px-3 py-2 text-sm text-tb-rose-700">{form.error}</p>
  {/if}
  {#if form?.setupUrl}
    <Card tone="info" title="Authentik 設定連結">
      <p class="break-all rounded-lg bg-white p-2 font-jetbrains-mono text-sm">{form.setupUrl}</p>
    </Card>
  {/if}

  {#if v.status === "pending"}
    <Card title="核准入駐">
      <form method="POST" action="?/approve" class="space-y-3">
        <fieldset>
          <legend class="text-[11px] font-bold uppercase tracking-eyebrow text-tb-slate-500">
            服務廠區
          </legend>
          <div class="mt-2 flex flex-wrap gap-2">
            {#each data.knownPlants as p}
              <label
                class="inline-flex cursor-pointer items-center gap-1.5 rounded-full border border-tb-slate-300 px-3 py-1 text-xs font-semibold text-tb-slate-700 hover:border-tb-slate-500"
              >
                <input type="checkbox" name="plants" value={p} checked={currentPlants.has(p)} />
                {p}
              </label>
            {/each}
          </div>
        </fieldset>
        <Button variant="primary" size="md" type="submit">
          <Icon name="check" class="h-3.5 w-3.5" />
          核准並設定廠區
        </Button>
      </form>
    </Card>
  {:else if v.status === "suspended"}
    <Card title="復權">
      <form method="POST" action="?/reinstate" class="space-y-3">
        <div>
          <div class="text-[11px] font-bold uppercase tracking-eyebrow text-tb-slate-500">
            服務廠區
          </div>
          <div class="mt-2 flex flex-wrap gap-2">
            {#each v.plants ?? [] as p}
              <span
                class="inline-flex items-center rounded-full border border-tb-slate-300 px-3 py-1 text-xs font-semibold text-tb-slate-700"
              >
                {p}
              </span>
            {:else}
              <span class="text-sm text-tb-slate-500">未設定廠區</span>
            {/each}
          </div>
        </div>
        <Button variant="primary" size="md" type="submit">
          <Icon name="check" class="h-3.5 w-3.5" />
          復權
        </Button>
      </form>
    </Card>
  {/if}

  {#if v.status === "approved"}
    <Card title="商家操作">
      <div class="flex flex-wrap gap-2">
        <form method="POST" action="?/suspend">
          <Button variant="danger" size="md" type="submit">停權</Button>
        </form>
      </div>
    </Card>
  {/if}

  {#if v.status === "approved" || v.status === "suspended"}
    <Card title="商家操作員">
      {#if v.status === "approved"}
        <form
          method="POST"
          action="?/createOperator"
          class="mb-4 grid grid-cols-1 gap-2 md:grid-cols-[1fr_1fr_auto]"
        >
          <input
            name="email"
            type="email"
            placeholder="operator@vendor.tw"
            required
            class="rounded-lg border border-tb-slate-300 px-3 py-2 text-sm font-jetbrains-mono focus:border-tb-slate-500 focus:outline-none focus:ring-2 focus:ring-tb-slate-300"
          />
          <input
            name="display_name"
            placeholder="操作員姓名"
            required
            class="rounded-lg border border-tb-slate-300 px-3 py-2 text-sm focus:border-tb-slate-500 focus:outline-none focus:ring-2 focus:ring-tb-slate-300"
          />
          <Button variant="primary" size="md" type="submit">
            <Icon name="plus" class="h-3.5 w-3.5" />新增
          </Button>
        </form>
      {/if}

      <div class="overflow-hidden rounded-xl border border-tb-slate-200">
        <table class="w-full text-sm">
          <thead
            class="bg-tb-slate-50 text-left text-[11px] font-bold uppercase tracking-wider text-tb-slate-500"
          >
            <tr>
              <th class="px-4 py-2.5">操作員</th>
              <th class="px-4 py-2.5">Provider</th>
              <th class="px-4 py-2.5">狀態</th>
              <th class="px-4 py-2.5"></th>
            </tr>
          </thead>
          <tbody class="divide-y divide-tb-slate-100">
            {#each data.operators as op (op.id)}
              <tr>
                <td class="px-4 py-3">
                  <div class="font-semibold text-tb-slate-900">{op.display_name}</div>
                  <div class="font-jetbrains-mono text-xs text-tb-slate-500">{op.email}</div>
                </td>
                <td class="px-4 py-3 font-jetbrains-mono text-xs text-tb-slate-500"
                  >{op.provider}</td
                >
                <td class="px-4 py-3 text-xs text-tb-slate-600"
                  >{operatorLabel[op.status] ?? op.status}</td
                >
                <td class="px-4 py-3 text-right">
                  {#if op.status === "active"}
                    <form method="POST" action="?/suspendOperator">
                      <input type="hidden" name="operator_id" value={op.id} />
                      <Button variant="danger" size="sm" type="submit">停用</Button>
                    </form>
                  {:else if op.status === "suspended"}
                    <form method="POST" action="?/reinstateOperator">
                      <input type="hidden" name="operator_id" value={op.id} />
                      <Button variant="secondary" size="sm" type="submit">啟用</Button>
                    </form>
                  {/if}
                </td>
              </tr>
            {:else}
              <tr>
                <td class="px-4 py-6 text-sm text-tb-slate-500" colspan="4">尚無操作員</td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    </Card>
  {/if}
</div>
