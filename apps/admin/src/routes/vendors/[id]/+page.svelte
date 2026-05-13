<script lang="ts">
  import { StateTag, Card } from "@tbite/ui";
  let { data, form } = $props();
  const v = data.vendor;
  const currentPlants = new Set<string>(v.plants ?? []);
</script>

<section class="max-w-2xl space-y-4">
  <header>
    <a href="/vendors" class="text-xs text-tb-slate-500 hover:text-tb-slate-700">← 返回列表</a>
    <h1 class="mt-1 text-2xl font-black text-tb-slate-900">{v.display_name}</h1>
    <p class="mt-1 text-sm text-tb-slate-500 font-jetbrains-mono">{v.contact_email}</p>
    <div class="mt-2 flex items-center gap-3">
      {#if v.status === "approved"}<StateTag tone="success">已核准</StateTag>
      {:else if v.status === "pending"}<StateTag tone="warning">待審</StateTag>
      {:else if v.status === "suspended"}<StateTag tone="danger">停權</StateTag>
      {:else}<StateTag tone="neutral">{v.status}</StateTag>{/if}
      <a href="/vendors/{v.id}/documents" class="text-sm text-tb-red-600 hover:text-tb-red-700">→ 合規文件</a>
    </div>
  </header>

  {#if form?.error}
    <p class="rounded-lg bg-tb-rose-50 px-3 py-2 text-sm text-tb-rose-700">{form.error}</p>
  {/if}
  {#if form?.invite}
    <Card tone="info">
      <p class="text-sm font-semibold">已產生邀請碼</p>
      <p class="mt-2 break-all rounded-lg bg-white p-2 font-jetbrains-mono text-sm">{form.invite}</p>
      <p class="mt-2 text-xs text-tb-slate-500">把連結 <code class="font-jetbrains-mono">/onboard?invite={form.invite}</code> 寄給商家窗口；商家在 7 天內以 Google/GitHub 完成入駐。</p>
    </Card>
  {/if}

  {#if v.status === "pending" || v.status === "suspended"}
    <Card>
      <h2 class="text-sm font-bold text-tb-slate-900">{v.status === "pending" ? "核准入駐" : "復權"}</h2>
      <form method="POST" action="?/approve" class="mt-3 space-y-3">
        <fieldset>
          <legend class="text-xs font-semibold uppercase tracking-eyebrow text-tb-slate-500">服務廠區</legend>
          <div class="mt-2 flex flex-wrap gap-2">
            {#each data.knownPlants as p}
              <label class="inline-flex items-center gap-1 rounded-full border border-tb-slate-300 px-3 py-1 text-xs">
                <input type="checkbox" name="plants" value={p} checked={currentPlants.has(p)} />
                {p}
              </label>
            {/each}
          </div>
        </fieldset>
        <button class="rounded-lg bg-tb-red-600 px-3.5 py-2 text-sm font-semibold text-white hover:bg-tb-red-700">
          {v.status === "pending" ? "核准並設定廠區" : "復權"}
        </button>
      </form>
    </Card>
  {/if}

  {#if v.status === "approved"}
    <Card>
      <h2 class="text-sm font-bold text-tb-slate-900">操作</h2>
      <div class="mt-3 flex flex-wrap gap-2">
        <form method="POST" action="?/suspend"><button class="rounded-lg border border-tb-rose-300 bg-tb-rose-50 px-3.5 py-2 text-sm font-semibold text-tb-rose-700">停權</button></form>
        <form method="POST" action="?/invite"><button class="rounded-lg border border-tb-slate-300 px-3.5 py-2 text-sm font-semibold text-tb-slate-800 hover:border-tb-slate-500">產生邀請碼</button></form>
      </div>
    </Card>
  {/if}
</section>
