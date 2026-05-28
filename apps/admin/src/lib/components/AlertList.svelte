<script lang="ts">
  // Severity-toned alert rows bound to AnomalyDTO.
  import { Card, Button } from "@tbite/ui";

  interface Anomaly {
    id: string;
    kind: string;
    severity: string;
    status: string;
    notes?: string;
    target_kind: string;
    target_id: string;
    created_at: string;
  }
  interface Props {
    anomalies: Anomaly[];
  }
  let { anomalies }: Props = $props();

  const toneMap: Record<string, { dot: string; border: string; bg: string }> = {
    danger: { dot: "bg-tb-rose-500", border: "border-tb-rose-200", bg: "bg-tb-rose-50/60" },
    warning: { dot: "bg-tb-amber-500", border: "border-tb-amber-200", bg: "bg-tb-amber-50/60" },
    info: { dot: "bg-tb-emerald-500", border: "border-tb-emerald-200", bg: "bg-tb-emerald-50/60" },
  };
  function toneFor(severity: string) {
    if (severity === "critical" || severity === "high") return toneMap.danger;
    if (severity === "medium") return toneMap.warning;
    return toneMap.info;
  }
  const kindLabel: Record<string, string> = {
    late_delivery: "配送延遲",
    low_ontime_rate: "準時率下降",
    document_expiring: "文件即將到期",
    document_expired: "文件已過期",
    no_show_spike: "未取餐異常上升",
    refund_spike: "退款異常上升",
  };
  function fmtTime(iso: string): string {
    return (iso ?? "").slice(0, 16).replace("T", " ");
  }
</script>

<Card title="異常治理告警" description="近 7 日 · 自動偵測商家與廠區異常">
  {#if anomalies.length === 0}
    <p
      class="rounded-xl border border-dashed border-tb-slate-300 bg-tb-slate-50/60 px-4 py-6 text-center text-sm text-tb-slate-500"
    >
      近 7 日無待處理告警
    </p>
  {:else}
    <div class="grid gap-3">
      {#each anomalies as a (a.id)}
        {@const t = toneFor(a.severity)}
        <div class="flex flex-wrap items-start gap-3 rounded-xl border {t.border} {t.bg} p-4">
          <span class="mt-1.5 h-2 w-2 flex-shrink-0 rounded-full {t.dot}" aria-hidden="true"></span>
          <div class="min-w-0 flex-1">
            <div class="text-sm font-bold text-tb-slate-900">
              {kindLabel[a.kind] ?? a.kind}
              <span class="ml-1 font-normal text-tb-slate-400">· {a.severity}</span>
            </div>
            <p class="mt-1 text-xs leading-relaxed text-tb-slate-600">
              {a.notes || `目標 ${a.target_kind}/${a.target_id.slice(0, 8)}`}
              <span class="text-tb-slate-400"> · {fmtTime(a.created_at)}</span>
            </p>
          </div>
          <a href="/anomalies?status={a.status}">
            <Button variant="secondary" size="sm">前往處理</Button>
          </a>
        </div>
      {/each}
    </div>
  {/if}
</Card>
