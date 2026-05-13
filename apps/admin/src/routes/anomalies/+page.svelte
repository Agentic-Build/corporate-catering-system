<script lang="ts">
  import { Card, StateTag } from "@tbite/ui";

  let { data, form } = $props();

  const statusFilters = [
    { id: "open", label: "未處理" },
    { id: "triaged", label: "處理中" },
    { id: "closed", label: "已關閉" },
    { id: "", label: "全部" },
  ];
  const severityFilters = [
    { id: "", label: "全部嚴重度" },
    { id: "critical", label: "critical" },
    { id: "high", label: "high" },
    { id: "medium", label: "medium" },
    { id: "low", label: "low" },
  ];

  const severityTone = {
    low: "neutral",
    medium: "info",
    high: "warning",
    critical: "danger",
  } as Record<string, "info" | "neutral" | "warning" | "danger" | "success">;
  const statusTone = {
    open: "warning",
    triaged: "info",
    closed: "neutral",
  } as Record<string, "info" | "neutral" | "warning" | "danger" | "success">;
  const statusLabel = {
    open: "未處理",
    triaged: "處理中",
    closed: "已關閉",
  } as Record<string, string>;

  function hrefWith(key: string, value: string) {
    const sp = new URLSearchParams();
    if (key === "status" ? value : data.status) sp.set("status", key === "status" ? value : data.status);
    if (key === "severity" ? value : data.severity) sp.set("severity", key === "severity" ? value : data.severity);
    const qs = sp.toString();
    return qs ? `?${qs}` : "?";
  }

  function preview(p: Record<string, unknown> | null | undefined): string {
    if (!p) return "{}";
    try {
      const s = JSON.stringify(p);
      return s.length > 200 ? s.slice(0, 200) + "…" : s;
    } catch {
      return "{}";
    }
  }
</script>

<section class="space-y-4">
  <header>
    <h1 class="text-2xl font-black text-tb-slate-900">告警</h1>
    <p class="mt-1 text-sm text-tb-slate-500">文件到期、商家準時率下降等系統異常</p>
  </header>

  {#if form?.error}
    <p class="rounded-lg bg-tb-rose-50 px-3 py-2 text-sm text-tb-rose-700">{form.error}</p>
  {/if}

  <div class="flex flex-wrap gap-2">
    <div class="flex flex-wrap gap-1 rounded-full bg-tb-slate-100 p-1">
      {#each statusFilters as f}
        <a
          href={hrefWith("status", f.id)}
          class="rounded-full px-3 py-1 text-xs font-semibold {data.status === f.id
            ? 'bg-tb-slate-900 text-white'
            : 'text-tb-slate-700'}"
        >
          {f.label}
        </a>
      {/each}
    </div>
    <div class="flex flex-wrap gap-1 rounded-full bg-tb-slate-100 p-1">
      {#each severityFilters as f}
        <a
          href={hrefWith("severity", f.id)}
          class="rounded-full px-3 py-1 text-xs font-semibold {data.severity === f.id
            ? 'bg-tb-slate-900 text-white'
            : 'text-tb-slate-700'}"
        >
          {f.label}
        </a>
      {/each}
    </div>
  </div>

  {#if data.anomalies.length === 0}
    <p class="rounded-tb-2xl border border-tb-slate-200 bg-white p-6 text-center text-sm text-tb-slate-500">
      無告警
    </p>
  {:else}
    <div class="space-y-3">
      {#each data.anomalies as a (a.id)}
        <Card>
          <div class="flex flex-wrap items-center justify-between gap-2">
            <div class="flex items-center gap-2">
              <StateTag tone={severityTone[a.severity] ?? "neutral"}>{a.severity}</StateTag>
              <StateTag tone={statusTone[a.status] ?? "neutral"}>{statusLabel[a.status] ?? a.status}</StateTag>
              <span class="font-jetbrains-mono text-xs text-tb-slate-500">{a.kind}</span>
            </div>
            <span class="font-jetbrains-mono text-xs text-tb-slate-500">{a.created_at.slice(0, 19).replace("T", " ")}</span>
          </div>
          <p class="mt-2 text-sm">
            <span class="text-tb-slate-500">目標：</span>
            <span class="font-jetbrains-mono text-tb-slate-900">{a.target_kind}/{a.target_id.slice(0, 8)}</span>
          </p>
          {#if a.payload}
            <pre class="mt-2 overflow-x-auto rounded-lg bg-tb-slate-50 p-2 text-xs font-jetbrains-mono text-tb-slate-700">{preview(a.payload)}</pre>
          {/if}
          {#if (a.evidence_uri ?? []).length > 0}
            <div class="mt-2 flex flex-wrap gap-2">
              {#each a.evidence_uri as uri}
                <a href={uri} target="_blank" rel="noopener" class="text-xs text-tb-red-600 hover:text-tb-red-700 font-jetbrains-mono break-all">{uri}</a>
              {/each}
            </div>
          {/if}
          {#if a.notes}
            <p class="mt-2 rounded-lg bg-tb-slate-50 p-2 text-xs text-tb-slate-700">
              <span class="font-semibold">備註：</span>{a.notes}
            </p>
          {/if}

          {#if a.status !== "closed"}
            <div class="mt-3 flex flex-wrap gap-2 border-t border-tb-slate-100 pt-3">
              {#if a.status === "open"}
                <form method="POST" action="?/triage" class="flex flex-wrap items-center gap-2">
                  <input type="hidden" name="id" value={a.id} />
                  <input
                    type="text"
                    name="notes"
                    placeholder="標記為處理中的備註"
                    class="rounded-lg border border-tb-slate-300 px-2 py-1 text-xs"
                  />
                  <button class="rounded-lg border border-tb-slate-300 px-3 py-1 text-xs font-semibold text-tb-slate-800 hover:border-tb-slate-500">標記處理中</button>
                </form>
              {/if}
              <form method="POST" action="?/close" class="flex flex-wrap items-center gap-2">
                <input type="hidden" name="id" value={a.id} />
                <input
                  type="text"
                  name="notes"
                  placeholder="關閉備註"
                  class="rounded-lg border border-tb-slate-300 px-2 py-1 text-xs"
                />
                <button class="rounded-lg bg-tb-red-600 px-3 py-1 text-xs font-semibold text-white hover:bg-tb-red-700">關閉</button>
              </form>
            </div>
          {/if}
        </Card>
      {/each}
    </div>
  {/if}
</section>
