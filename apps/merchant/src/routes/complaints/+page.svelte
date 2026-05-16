<script lang="ts">
  import { PageHeader, Card, Button, StateTag, EmptyState, Icon } from "@tbite/ui";

  let { data, form } = $props();

  const statusMeta = {
    open: { tone: "warning", label: "待回覆" },
    vendor_responded: { tone: "info", label: "已回覆" },
    escalated: { tone: "danger", label: "已升級福委會" },
    resolved: { tone: "success", label: "已結案" },
  } as Record<
    string,
    { tone: "success" | "warning" | "danger" | "info" | "neutral"; label: string }
  >;

  const categoryLabel = {
    wrong_item: "送錯餐點",
    missing_item: "餐點漏送",
    quality: "餐點品質",
    portion: "份量不足",
    hygiene: "衛生問題",
    other: "其他",
  } as Record<string, string>;

  const filters = [
    { id: "", label: "全部" },
    { id: "open", label: "待回覆" },
    { id: "vendor_responded", label: "已回覆" },
    { id: "escalated", label: "已升級" },
    { id: "resolved", label: "已結案" },
  ];

  function fmtDate(s: string | undefined | null): string {
    if (!s) return "—";
    return new Date(s).toLocaleString("zh-TW", {
      year: "numeric",
      month: "2-digit",
      day: "2-digit",
      hour: "2-digit",
      minute: "2-digit",
    });
  }

  // Which complaint's reply box is open.
  let replyingID = $state("");
</script>

<PageHeader
  eyebrow="Complaints · 客訴收件匣"
  title="客訴收件匣"
  subtitle="處理員工對餐點的客訴。回覆後客訴狀態將更新為「已回覆」，員工可選擇結案或升級福委會。"
/>

<div class="mb-4 flex flex-wrap gap-2 text-xs">
  {#each filters as f (f.id)}
    <a
      href={f.id ? `/complaints?status=${f.id}` : "/complaints"}
      class="rounded-full px-3 py-1 font-semibold {data.status === f.id
        ? 'bg-tb-slate-900 text-white'
        : 'bg-tb-slate-100 text-tb-slate-700 hover:text-tb-slate-900'}"
    >
      {f.label}
    </a>
  {/each}
</div>

{#if form?.error}
  <p class="mb-4 rounded-lg bg-tb-rose-50 px-3 py-2 text-sm text-tb-rose-700">{form.error}</p>
{/if}
{#if form?.success && form?.respondedID}
  <p class="mb-4 rounded-lg bg-tb-emerald-50 px-3 py-2 text-sm text-tb-emerald-700">已送出回覆</p>
{/if}

{#if data.items.length === 0}
  <EmptyState
    icon="bell"
    title="目前沒有客訴"
    hint={data.status ? "此狀態下沒有客訴，試試其他篩選。" : "員工回報餐點問題後會顯示於此。"}
  />
{:else}
  <div class="space-y-3">
    {#each data.items as c (c.id)}
      {@const meta = statusMeta[c.status] ?? { tone: "neutral", label: c.status }}
      {@const canRespond = c.status === "open"}
      <Card>
        <div class="flex flex-wrap items-start justify-between gap-3">
          <div class="min-w-0">
            <div class="flex flex-wrap items-center gap-2">
              <StateTag tone={meta.tone}>{meta.label}</StateTag>
              <span class="text-sm font-bold text-tb-slate-900">
                {categoryLabel[c.category] ?? c.category}
              </span>
              <span class="font-jetbrains-mono text-xs text-tb-slate-400">
                訂單 {String(c.order_id ?? "").slice(0, 8)}…
              </span>
            </div>
            <p class="mt-2 whitespace-pre-wrap text-sm text-tb-slate-700">{c.description}</p>
            <p class="mt-1.5 text-xs text-tb-slate-400">回報於 {fmtDate(c.created_at)}</p>
          </div>
          {#if canRespond}
            <Button
              variant="secondary"
              size="sm"
              onclick={() => (replyingID = replyingID === c.id ? "" : c.id)}
            >
              <Icon name="doc" class="h-3.5 w-3.5" />回覆
            </Button>
          {/if}
        </div>

        {#if c.vendor_response}
          <div class="mt-3 rounded-xl bg-tb-slate-50 px-3 py-2.5">
            <p class="text-[11px] font-bold uppercase tracking-eyebrow text-tb-slate-500">
              商家回覆 · {fmtDate(c.vendor_responded_at)}
            </p>
            <p class="mt-1 whitespace-pre-wrap text-sm text-tb-slate-700">{c.vendor_response}</p>
          </div>
        {/if}

        {#if c.resolution}
          <div class="mt-3 rounded-xl bg-tb-emerald-50 px-3 py-2.5">
            <p class="text-[11px] font-bold uppercase tracking-eyebrow text-tb-emerald-700">
              結案說明 · {fmtDate(c.resolved_at)}
            </p>
            <p class="mt-1 whitespace-pre-wrap text-sm text-tb-emerald-800">{c.resolution}</p>
          </div>
        {/if}

        {#if canRespond && replyingID === c.id}
          <form method="POST" action="?/respond" class="mt-3 space-y-2">
            <input type="hidden" name="complaint_id" value={c.id} />
            <label class="block text-sm font-semibold text-tb-slate-800">
              回覆內容（至少 5 個字）
              <textarea
                name="response"
                rows="3"
                required
                minlength="5"
                class="mt-1 w-full rounded-lg border border-tb-slate-300 px-3 py-2 text-sm focus:border-tb-red-500 focus:outline-none focus:ring-4 focus:ring-tb-red-100"
              ></textarea>
            </label>
            <div class="flex justify-end gap-2">
              <Button variant="ghost" size="sm" onclick={() => (replyingID = "")}>取消</Button>
              <Button variant="primary" size="sm" type="submit">送出回覆</Button>
            </div>
          </form>
        {/if}
      </Card>
    {/each}
  </div>
{/if}
