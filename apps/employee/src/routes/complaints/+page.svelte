<script lang="ts">
  // 我的客訴 — F1 員工回饋. Lists the employee's complaints with status,
  // vendor response and resolution. The 升級福委會 button only enables once
  // 24h have passed since filing (backend also enforces this); the 結案
  // button lets the employee close a complaint they are satisfied with.
  import { PageHeader, Card, StateTag, EmptyState, Button } from "@tbite/ui";
  import type { MealComplaint } from "$lib/server/feedback";

  let { data, form } = $props();

  const complaints = $derived((data.complaints as MealComplaint[]) ?? []);

  const categoryLabel: Record<string, string> = {
    wrong_item: "送錯餐點",
    missing_item: "餐點短缺",
    quality: "品質不佳",
    portion: "份量不足",
    hygiene: "衛生問題",
    other: "其他問題",
  };
  const statusTone: Record<string, "info" | "neutral" | "warning" | "danger" | "success"> = {
    open: "warning",
    vendor_responded: "info",
    escalated: "danger",
    resolved: "success",
  };
  const statusLabel: Record<string, string> = {
    open: "處理中",
    vendor_responded: "商家已回覆",
    escalated: "已升級福委會",
    resolved: "已結案",
  };

  const DAY_MS = 24 * 60 * 60 * 1000;

  function fmt(iso: string | undefined | null): string {
    return iso ? iso.slice(0, 16).replace("T", " ") : "-";
  }

  // 24h gate — the escalate button stays disabled until a full day has
  // passed since the complaint was filed.
  function canEscalate(c: MealComplaint): boolean {
    if (c.status !== "open" && c.status !== "vendor_responded") return false;
    return Date.now() - new Date(c.created_at).getTime() >= DAY_MS;
  }
  function hoursUntilEscalatable(c: MealComplaint): number {
    const elapsed = Date.now() - new Date(c.created_at).getTime();
    return Math.max(0, Math.ceil((DAY_MS - elapsed) / (60 * 60 * 1000)));
  }
  function canResolve(c: MealComplaint): boolean {
    return c.status === "open" || c.status === "vendor_responded";
  }
</script>

<PageHeader
  eyebrow="Complaints · 我的客訴"
  title="餐點問題回報"
  subtitle="回報後商家會收到並回覆；逾 24 小時未獲妥善處理，可升級福委會。"
/>

{#if form?.error}
  <p class="mb-4 rounded-tb-xl bg-tb-rose-50 px-3 py-2 text-sm text-tb-rose-700">{form.error}</p>
{/if}

{#if data.error}
  <div
    class="mb-4 rounded-tb-2xl border border-tb-rose-300 bg-tb-rose-50/60 p-4 text-sm text-tb-rose-700"
  >
    載入失敗：{data.error}
  </div>
{/if}

{#if complaints.length === 0}
  <EmptyState
    icon="alert"
    title="尚無客訴記錄"
    hint="領餐後若餐點有問題，可從訂單詳情頁的「回報問題」提出。"
  />
{:else}
  <div class="grid gap-3">
    {#each complaints as c (c.id)}
      <Card>
        <div class="flex flex-wrap items-center justify-between gap-3">
          <div class="flex items-center gap-2">
            <span class="text-sm font-bold text-tb-slate-900">
              {categoryLabel[c.category] ?? c.category}
            </span>
            <a
              href={`/orders/${c.order_id}`}
              class="font-jetbrains-mono text-xs text-tb-slate-500 hover:text-tb-slate-900"
            >
              訂單 {c.order_id.slice(0, 8)}
            </a>
          </div>
          <StateTag tone={statusTone[c.status] ?? "neutral"}>
            {statusLabel[c.status] ?? c.status}
          </StateTag>
        </div>

        <p class="mt-2 text-sm text-tb-slate-900">{c.description}</p>
        <p class="mt-1 text-[11px] text-tb-slate-400">回報於 {fmt(c.created_at)}</p>

        {#if c.vendor_response}
          <div class="mt-3 rounded-tb-xl bg-tb-slate-50 p-3 text-xs text-tb-slate-700">
            <span class="font-semibold">商家回覆：</span>{c.vendor_response}
            {#if c.vendor_responded_at}
              <span class="ml-1 text-tb-slate-400">· {fmt(c.vendor_responded_at)}</span>
            {/if}
          </div>
        {/if}

        {#if c.resolution}
          <div class="mt-2 rounded-tb-xl bg-tb-emerald-50 p-3 text-xs text-tb-emerald-700">
            <span class="font-semibold">結案說明：</span>{c.resolution}
          </div>
        {/if}

        {#if c.status !== "resolved"}
          <div class="mt-3 flex flex-wrap items-center gap-2 border-t border-tb-slate-100 pt-3">
            {#if c.status === "open" || c.status === "vendor_responded"}
              <form method="POST" action="?/escalate">
                <input type="hidden" name="id" value={c.id} />
                <Button variant="secondary" size="sm" type="submit" disabled={!canEscalate(c)}>
                  升級福委會
                </Button>
              </form>
              {#if !canEscalate(c)}
                <span class="text-[11px] text-tb-slate-400">
                  滿 24 小時後可升級（約還需 {hoursUntilEscalatable(c)} 小時）
                </span>
              {/if}
            {/if}
            {#if canResolve(c)}
              <form method="POST" action="?/resolve">
                <input type="hidden" name="id" value={c.id} />
                <Button variant="primary" size="sm" type="submit">我已滿意，結案</Button>
              </form>
            {/if}
            {#if c.status === "escalated"}
              <span class="text-[11px] text-tb-slate-500">已轉交福委會處理，請等待結果。</span>
            {/if}
          </div>
        {/if}
      </Card>
    {/each}
  </div>
{/if}
