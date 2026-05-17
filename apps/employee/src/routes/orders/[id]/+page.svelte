<script lang="ts">
  // 訂單詳情 — design-language pass. PageHeader + Card + StateTag, with the
  // detail card mirroring the reference OrderCard footer. For picked_up
  // orders this page also hosts the F1 員工回饋 forms: a meal rating and a
  // 回報問題 (complaint) form.
  import { PageHeader, Card, StateTag, Button, Icon } from "@tbite/ui";

  let { data, form } = $props();
  const o = $derived(data.order);

  const statusTone: Record<string, "info" | "neutral" | "warning" | "danger" | "success"> = {
    draft: "neutral",
    placed: "info",
    cutoff: "warning",
    cancelled: "neutral",
    ready: "success",
    picked_up: "success",
    no_show: "danger",
    refunded: "warning",
  };
  const statusLabel: Record<string, string> = {
    draft: "草稿",
    placed: "已預訂",
    cutoff: "已截單",
    cancelled: "已取消",
    ready: "備餐完成",
    picked_up: "已領取",
    no_show: "未領取",
    refunded: "已退款",
  };

  // ── F1 feedback labels ──
  const complaintCategories = [
    { id: "wrong_item", label: "送錯餐點" },
    { id: "missing_item", label: "餐點短缺" },
    { id: "quality", label: "品質不佳" },
    { id: "portion", label: "份量不足" },
    { id: "hygiene", label: "衛生問題" },
    { id: "other", label: "其他問題" },
  ];
  const complaintCategoryLabel: Record<string, string> = Object.fromEntries(
    complaintCategories.map((c) => [c.id, c.label]),
  );
  const complaintStatusTone: Record<string, "info" | "neutral" | "warning" | "danger" | "success"> =
    {
      open: "warning",
      vendor_responded: "info",
      escalated: "danger",
      resolved: "success",
    };
  const complaintStatusLabel: Record<string, string> = {
    open: "處理中",
    vendor_responded: "商家已回覆",
    escalated: "已升級福委會",
    resolved: "已結案",
  };

  function fmt(iso: string | undefined): string {
    return iso ? iso.slice(0, 16).replace("T", " ") : "-";
  }

  // ── existing / freshly-submitted complaint ──
  const complaint = $derived(form?.complaint ?? data.complaint);

  // ── rating form local state ──
  let starValue = $state(0);
  const submittedRating = $derived(form?.rating);
</script>

<a
  href="/orders"
  class="mb-3 inline-flex items-center gap-1 text-xs font-semibold text-tb-slate-500 hover:text-tb-slate-900"
>
  <Icon name="chevron" class="h-3.5 w-3.5 rotate-90" />返回訂單列表
</a>

<PageHeader eyebrow="Order · 訂單詳情" title={o.supply_date}>
  {#snippet actions()}
    <StateTag tone={statusTone[o.status] ?? "neutral"}>
      {statusLabel[o.status] ?? o.status}
    </StateTag>
  {/snippet}
</PageHeader>

<div class="max-w-xl space-y-4">
  {#if form?.error}
    <p class="rounded-tb-xl bg-tb-rose-50 px-3 py-2 text-sm text-tb-rose-700">{form.error}</p>
  {/if}

  <Card>
    <p class="font-jetbrains-mono text-[11px] text-tb-slate-500">{o.id}</p>
    <dl class="mt-3 grid grid-cols-2 gap-y-2.5 text-sm">
      <dt class="text-tb-slate-500">取餐日</dt>
      <dd class="font-jetbrains-mono">{o.supply_date}</dd>
      <dt class="text-tb-slate-500">取餐區</dt>
      <dd class="flex items-center gap-1.5">
        <Icon name="pin" class="h-3.5 w-3.5 text-tb-red-600" />{o.plant}
      </dd>
      <dt class="text-tb-slate-500">截單時間</dt>
      <dd class="font-jetbrains-mono">{fmt(o.cutoff_at)}</dd>
    </dl>
    <div class="mt-3 flex items-end justify-between border-t border-tb-slate-100 pt-3">
      <span class="text-sm text-tb-slate-600">合計（薪資代扣）</span>
      <span class="font-jetbrains-mono text-2xl font-black tabular-nums text-tb-slate-900">
        ${o.total_price_minor.toLocaleString()}
      </span>
    </div>
  </Card>

  <Card title="訂購項目">
    <ul class="divide-y divide-tb-slate-100 text-sm">
      {#each o.items as it (it.id)}
        <li class="flex items-center justify-between gap-3 py-3">
          <span class="font-jetbrains-mono text-xs text-tb-slate-600">
            {it.menu_item_id.slice(0, 8)}
          </span>
          <span class="font-jetbrains-mono tabular-nums">
            × {it.qty} · ${(it.unit_price_minor * it.qty).toLocaleString()}
          </span>
        </li>
      {/each}
    </ul>
  </Card>

  <div class="flex flex-wrap items-center gap-2">
    {#if o.status === "ready"}
      <a
        href={`/orders/${o.id}/pickup`}
        class="inline-flex items-center gap-2 rounded-tb-lg bg-tb-red-600 px-3.5 py-2 text-sm font-semibold text-white transition hover:bg-tb-red-700"
      >
        <Icon name="qr" class="h-4 w-4" />出示領餐碼
      </a>
    {/if}
    {#if o.status === "placed"}
      <form method="POST" action="?/cancel">
        <Button variant="danger" size="md" type="submit">取消訂單</Button>
      </form>
    {/if}
    {#if o.status === "picked_up" || o.status === "no_show"}
      <a
        href={`/orders/${o.id}/dispute`}
        class="inline-flex items-center gap-2 rounded-tb-lg border border-tb-slate-300 px-3.5 py-2 text-sm font-semibold text-tb-slate-800 transition hover:border-tb-slate-500"
      >
        <Icon name="alert" class="h-4 w-4 text-tb-amber-600" />提出申訴
      </a>
    {/if}
  </div>

  <!-- ── F1 員工回饋 — only for picked_up orders ── -->
  {#if o.status === "picked_up"}
    <!-- Meal rating -->
    <Card title="餐點評分" description="為這份餐點打個分數，協助我們追蹤商家品質。">
      {#if submittedRating}
        <div class="rounded-tb-xl bg-tb-emerald-50 p-3 text-sm text-tb-emerald-700">
          <p class="font-semibold">已完成評分</p>
          <p class="mt-1 text-lg" aria-label={`${submittedRating.score} 顆星`}>
            {"★".repeat(submittedRating.score)}<span class="text-tb-slate-300"
              >{"★".repeat(5 - submittedRating.score)}</span
            >
          </p>
          {#if submittedRating.comment}
            <p class="mt-1 text-tb-slate-700">{submittedRating.comment}</p>
          {/if}
        </div>
      {:else}
        {#if form?.ratingError}
          <p class="mb-3 rounded-tb-xl bg-tb-rose-50 px-3 py-2 text-sm text-tb-rose-700">
            {form.ratingError}
          </p>
        {/if}
        <form method="POST" action="?/rate" class="space-y-3">
          <input type="hidden" name="score" value={starValue} />
          <div class="flex flex-col gap-1.5">
            <span class="text-[11px] font-bold uppercase tracking-eyebrow text-tb-slate-500">
              星等評分
            </span>
            <div class="flex items-center gap-1" role="radiogroup" aria-label="星等評分">
              {#each [1, 2, 3, 4, 5] as star (star)}
                <button
                  type="button"
                  role="radio"
                  aria-checked={starValue === star}
                  aria-label={`${star} 顆星`}
                  onclick={() => (starValue = star)}
                  class="text-3xl leading-none transition hover:scale-110 {star <= starValue
                    ? 'text-tb-amber-400'
                    : 'text-tb-slate-300'}"
                >
                  ★
                </button>
              {/each}
            </div>
          </div>
          <label class="flex flex-col gap-1.5 text-sm">
            <span class="text-[11px] font-bold uppercase tracking-eyebrow text-tb-slate-500">
              留言（選填，最多 500 字）
            </span>
            <textarea
              name="comment"
              rows="3"
              maxlength="500"
              placeholder="想對這份餐點說的話…"
              class="rounded-tb-lg border border-tb-slate-300 px-3 py-2 text-sm transition focus:border-tb-red-500 focus:outline-none focus:ring-4 focus:ring-tb-red-100"
            ></textarea>
          </label>
          <Button variant="primary" size="md" type="submit" disabled={starValue === 0}>
            送出評分
          </Button>
        </form>
      {/if}
    </Card>

    <!-- Complaint -->
    {#if complaint}
      <Card title="回報問題">
        <div class="flex flex-wrap items-center justify-between gap-2">
          <span class="text-sm font-semibold text-tb-slate-900">
            {complaintCategoryLabel[complaint.category] ?? complaint.category}
          </span>
          <StateTag tone={complaintStatusTone[complaint.status] ?? "neutral"}>
            {complaintStatusLabel[complaint.status] ?? complaint.status}
          </StateTag>
        </div>
        <p class="mt-2 text-sm text-tb-slate-700">{complaint.description}</p>
        {#if complaint.vendor_response}
          <div class="mt-3 rounded-tb-xl bg-tb-slate-50 p-3 text-xs text-tb-slate-700">
            <span class="font-semibold">商家回覆：</span>{complaint.vendor_response}
          </div>
        {/if}
        <a
          href="/complaints"
          class="mt-3 inline-block text-sm font-semibold text-tb-red-600 hover:text-tb-red-700"
        >
          查看客訴進度 →
        </a>
      </Card>
    {:else}
      <Card title="回報問題" description="餐點有問題嗎？回報後商家會收到並回覆。">
        {#if form?.complaintError}
          <p class="mb-3 rounded-tb-xl bg-tb-rose-50 px-3 py-2 text-sm text-tb-rose-700">
            {form.complaintError}
          </p>
        {/if}
        <form method="POST" action="?/complain" class="space-y-3">
          <label class="flex flex-col gap-1.5 text-sm">
            <span class="text-[11px] font-bold uppercase tracking-eyebrow text-tb-slate-500">
              問題類型
            </span>
            <select
              name="category"
              required
              class="rounded-tb-lg border border-tb-slate-300 px-3 py-2 text-sm transition focus:border-tb-red-500 focus:outline-none focus:ring-4 focus:ring-tb-red-100"
            >
              <option value="" disabled selected>請選擇問題類型</option>
              {#each complaintCategories as c (c.id)}
                <option value={c.id}>{c.label}</option>
              {/each}
            </select>
          </label>
          <label class="flex flex-col gap-1.5 text-sm">
            <span class="text-[11px] font-bold uppercase tracking-eyebrow text-tb-slate-500">
              問題描述（5–1000 字）
            </span>
            <textarea
              name="description"
              rows="4"
              required
              minlength="5"
              maxlength="1000"
              placeholder="請描述遇到的問題，例如送錯了什麼、品質如何…"
              class="rounded-tb-lg border border-tb-slate-300 px-3 py-2 text-sm transition focus:border-tb-red-500 focus:outline-none focus:ring-4 focus:ring-tb-red-100"
            ></textarea>
          </label>
          <Button variant="primary" size="md" type="submit">送出回報</Button>
        </form>
      </Card>
    {/if}
  {/if}
</div>
