<script lang="ts">
  import { PageHeader, Card, StateTag, Button } from "@tbite/ui";

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
    if (key === "status" ? value : data.status)
      sp.set("status", key === "status" ? value : data.status);
    if (key === "severity" ? value : data.severity)
      sp.set("severity", key === "severity" ? value : data.severity);
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

<PageHeader
  eyebrow="異常治理"
  title="告警"
  subtitle="文件到期、商家準時率下降等系統自動偵測的異常"
/>

{#if form?.error}
  <p class="rounded-lg bg-tb-rose-50 px-3 py-2 text-sm text-tb-rose-700">{form.error}</p>
{/if}

<div class="flex flex-wrap gap-2">
  <div class="flex flex-wrap items-center gap-1 rounded-full bg-tb-slate-100 p-1">
    {#each statusFilters as f}
      <a
        href={hrefWith("status", f.id)}
        class="rounded-full px-3.5 py-1.5 text-xs font-semibold transition {data.status === f.id
          ? 'bg-tb-slate-900 text-white'
          : 'text-tb-slate-700 hover:bg-tb-slate-200'}"
      >
        {f.label}
      </a>
    {/each}
  </div>
  <div class="flex flex-wrap items-center gap-1 rounded-full bg-tb-slate-100 p-1">
    {#each severityFilters as f}
      <a
        href={hrefWith("severity", f.id)}
        class="rounded-full px-3.5 py-1.5 text-xs font-semibold transition {data.severity === f.id
          ? 'bg-tb-slate-900 text-white'
          : 'text-tb-slate-700 hover:bg-tb-slate-200'}"
      >
        {f.label}
      </a>
    {/each}
  </div>
</div>

<div class="mt-4">
  {#if data.anomalies.length === 0}
    <p
      class="rounded-tb-2xl border border-dashed border-tb-slate-300 bg-tb-slate-50/60 p-8 text-center text-sm text-tb-slate-500"
    >
      無符合條件的告警
    </p>
  {:else}
    <div class="grid gap-3">
      {#each data.anomalies as a (a.id)}
        <Card>
          <div class="flex flex-wrap items-center justify-between gap-2">
            <div class="flex items-center gap-2">
              <StateTag tone={severityTone[a.severity] ?? "neutral"}>{a.severity}</StateTag>
              <StateTag tone={statusTone[a.status] ?? "neutral"}>
                {statusLabel[a.status] ?? a.status}
              </StateTag>
              <span class="font-jetbrains-mono text-xs text-tb-slate-500">{a.kind}</span>
            </div>
            <span class="font-jetbrains-mono text-xs text-tb-slate-500">
              {a.created_at.slice(0, 16).replace("T", " ")}
            </span>
          </div>
          <p class="mt-2 text-sm">
            <span class="text-tb-slate-500">目標 ·</span>
            <span class="font-jetbrains-mono text-tb-slate-900"
              >{a.target_kind}/{a.target_id.slice(0, 8)}</span
            >
          </p>
          {#if a.payload}
            <pre
              class="mt-2 overflow-x-auto rounded-lg bg-tb-slate-50 p-2 font-jetbrains-mono text-xs text-tb-slate-700">{preview(
                a.payload,
              )}</pre>
          {/if}
          {#if (a.evidence_uri ?? []).length > 0}
            <div class="mt-2 flex flex-wrap gap-2">
              {#each a.evidence_uri as uri}
                <a
                  href={uri}
                  target="_blank"
                  rel="noopener"
                  class="break-all font-jetbrains-mono text-xs text-tb-red-600 hover:text-tb-red-700"
                  >{uri}</a
                >
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
                    class="rounded-lg border border-tb-slate-300 px-2 py-1 text-xs focus:border-tb-slate-500 focus:outline-none focus:ring-2 focus:ring-tb-slate-300"
                  />
                  <Button variant="secondary" size="sm" type="submit">標記處理中</Button>
                </form>
              {/if}
              <form method="POST" action="?/close" class="flex flex-wrap items-center gap-2">
                <input type="hidden" name="id" value={a.id} />
                <input
                  type="text"
                  name="notes"
                  placeholder="關閉備註"
                  class="rounded-lg border border-tb-slate-300 px-2 py-1 text-xs focus:border-tb-slate-500 focus:outline-none focus:ring-2 focus:ring-tb-slate-300"
                />
                <Button variant="primary" size="sm" type="submit">關閉告警</Button>
              </form>
            </div>
          {/if}
        </Card>
      {/each}
    </div>
  {/if}
</div>
