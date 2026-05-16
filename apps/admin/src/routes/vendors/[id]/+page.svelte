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
    <a href="/vendors/{v.id}/documents">
      <Button variant="secondary" size="sm">
        <Icon name="doc" class="h-3.5 w-3.5" />合規文件
      </Button>
    </a>
  {/snippet}
</PageHeader>

<div class="grid max-w-2xl gap-6">
  {#if form?.error}
    <p class="rounded-lg bg-tb-rose-50 px-3 py-2 text-sm text-tb-rose-700">{form.error}</p>
  {/if}
  {#if form?.invite}
    <Card tone="info" title="已產生邀請碼">
      <p class="break-all rounded-lg bg-white p-2 font-jetbrains-mono text-sm">{form.invite}</p>
      <p class="mt-2 text-xs text-tb-slate-500">
        把連結 <code class="font-jetbrains-mono">/onboard?invite={form.invite}</code>
        寄給商家窗口；商家在 7 天內以 Google／GitHub 完成入駐。
      </p>
    </Card>
  {/if}

  {#if v.status === "pending" || v.status === "suspended"}
    <Card title={v.status === "pending" ? "核准入駐" : "復權"}>
      <form method="POST" action="?/approve" class="space-y-3">
        <fieldset>
          <legend
            class="text-[11px] font-bold uppercase tracking-eyebrow text-tb-slate-500"
          >
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
          {v.status === "pending" ? "核准並設定廠區" : "復權"}
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
        <form method="POST" action="?/invite">
          <Button variant="secondary" size="md" type="submit">產生邀請碼</Button>
        </form>
      </div>
    </Card>
  {/if}
</div>
