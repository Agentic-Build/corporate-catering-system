<script lang="ts">
  // 訂單詳情 — design-language pass. PageHeader + Card + StateTag, with the
  // detail card mirroring the reference OrderCard footer. For picked_up
  // orders this page also hosts the F1 員工回饋 forms: a meal rating and a
  // 回報問題 (complaint) form.
  import { PageHeader, Card, StateTag, Button, Icon } from "@tbite/ui";

  let { data, form } = $props();
  const o = $derived(data.order);
  // OrderDTO.items is nullable in the contract; the API always sends an array.
  const items = $derived(o.items ?? []);

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

  // ── modify (edit order items) state ──
  let editing = $state(false);
  let draft = $state<Record<string, number>>({});

  // qty this order currently holds, keyed by menu_item_id.
  const origQty = $derived(Object.fromEntries(items.map((it) => [it.menu_item_id, it.qty])));

  // Rows for the edit form: the vendor's menu on the supply date, plus any
  // item already on the order that is no longer listed that day.
  const editRows = $derived.by(() => {
    const rows = new Map<string, { id: string; name: string; price: number; remain: number }>();
    for (const m of data.menu ?? []) {
      rows.set(m.id, { id: m.id, name: m.name, price: m.price_minor, remain: m.remain });
    }
    for (const it of items) {
      if (!rows.has(it.menu_item_id)) {
        rows.set(it.menu_item_id, {
          id: it.menu_item_id,
          name: it.menu_item_id.slice(0, 8),
          price: it.unit_price_minor,
          remain: 0,
        });
      }
    }
    return [...rows.values()];
  });

  // Effective max for a row = quota still free + qty this order already holds.
  function maxQty(row: { id: string; remain: number }): number {
    return row.remain + (origQty[row.id] ?? 0);
  }

  const draftItems = $derived(
    Object.entries(draft)
      .filter(([, q]) => q > 0)
      .map(([menu_item_id, qty]) => ({ menu_item_id, qty })),
  );
  const draftTotal = $derived(editRows.reduce((sum, r) => sum + r.price * (draft[r.id] ?? 0), 0));

  function startEdit() {
    draft = Object.fromEntries(items.map((it) => [it.menu_item_id, it.qty]));
    editing = true;
  }
  function setQty(id: string, n: number) {
    draft = { ...draft, [id]: Math.max(0, n) };
  }
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
      {#if o.notes}
        <dt class="text-tb-slate-500">特殊需求</dt>
        <dd class="text-tb-slate-700">{o.notes}</dd>
      {/if}
    </dl>
    <div class="mt-3 flex items-end justify-between border-t border-tb-slate-100 pt-3">
      <span class="text-sm text-tb-slate-600">合計（薪資代扣）</span>
      <span class="font-jetbrains-mono text-2xl font-black tabular-nums text-tb-slate-900">
        ${o.total_price_minor.toLocaleString()}
      </span>
    </div>
  </Card>

  {#if o.status === "placed" && editing}
    <Card title="編輯訂單" description="調整數量或加點同商家的其他餐點，截單前皆可修改。">
      {#if form?.modifyError}
        <p class="mb-3 rounded-tb-xl bg-tb-rose-50 px-3 py-2 text-sm text-tb-rose-700">
          {form.modifyError}
        </p>
      {/if}
      <form method="POST" action="?/modify" class="space-y-3">
        <input type="hidden" name="items" value={JSON.stringify(draftItems)} />
        <ul class="divide-y divide-tb-slate-100 text-sm">
          {#each editRows as row (row.id)}
            <li class="flex items-center justify-between gap-3 py-2.5">
              <div class="min-w-0">
                <p class="truncate font-semibold text-tb-slate-900">{row.name}</p>
                <p class="font-jetbrains-mono text-xs text-tb-slate-500">
                  ${row.price.toLocaleString()} · 可訂 {maxQty(row)}
                </p>
              </div>
              <div class="flex items-center gap-2">
                <button
                  type="button"
                  onclick={() => setQty(row.id, (draft[row.id] ?? 0) - 1)}
                  disabled={(draft[row.id] ?? 0) <= 0}
                  aria-label={`減少 ${row.name}`}
                  class="h-7 w-7 rounded-tb-lg border border-tb-slate-300 text-lg leading-none text-tb-slate-700 transition hover:border-tb-slate-500 disabled:opacity-30"
                >
                  −
                </button>
                <span class="w-6 text-center font-jetbrains-mono tabular-nums">
                  {draft[row.id] ?? 0}
                </span>
                <button
                  type="button"
                  onclick={() => setQty(row.id, (draft[row.id] ?? 0) + 1)}
                  disabled={(draft[row.id] ?? 0) >= maxQty(row)}
                  aria-label={`增加 ${row.name}`}
                  class="h-7 w-7 rounded-tb-lg border border-tb-slate-300 text-lg leading-none text-tb-slate-700 transition hover:border-tb-slate-500 disabled:opacity-30"
                >
                  +
                </button>
              </div>
            </li>
          {/each}
        </ul>
        <label class="flex flex-col gap-1.5 text-sm">
          <span class="text-[11px] font-bold uppercase tracking-eyebrow text-tb-slate-500">
            特殊需求備註（選填，最多 500 字）
          </span>
          <textarea
            name="notes"
            rows="2"
            maxlength="500"
            placeholder="例如：不要辣、過敏原、餐具需求…"
            class="rounded-tb-lg border border-tb-slate-300 px-3 py-2 text-sm transition focus:border-tb-red-500 focus:outline-none focus:ring-4 focus:ring-tb-red-100"
            >{o.notes}</textarea
          >
        </label>
        <div class="flex items-center justify-between border-t border-tb-slate-100 pt-3">
          <span class="text-sm text-tb-slate-600">修改後合計</span>
          <span class="font-jetbrains-mono text-xl font-black tabular-nums text-tb-slate-900">
            ${draftTotal.toLocaleString()}
          </span>
        </div>
        <div class="flex gap-2">
          <Button variant="primary" size="md" type="submit" disabled={draftItems.length === 0}>
            儲存修改
          </Button>
          <button
            type="button"
            onclick={() => (editing = false)}
            class="rounded-tb-lg border border-tb-slate-300 px-3.5 py-2 text-sm font-semibold text-tb-slate-700 transition hover:border-tb-slate-500"
          >
            取消編輯
          </button>
        </div>
      </form>
    </Card>
  {:else}
    <Card title="訂購項目">
      <ul class="divide-y divide-tb-slate-100 text-sm">
        {#each items as it (it.id)}
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
  {/if}

  <div class="flex flex-wrap items-center gap-2">
    {#if o.status === "ready"}
      <a
        href="/scan"
        class="inline-flex items-center gap-2 rounded-tb-lg bg-tb-red-600 px-3.5 py-2 text-sm font-semibold text-white transition hover:bg-tb-red-700"
      >
        <Icon name="qr" class="h-4 w-4" />掃描領餐
      </a>
    {/if}
    {#if o.status === "placed" && !editing}
      <button
        type="button"
        onclick={startEdit}
        class="inline-flex items-center gap-2 rounded-tb-lg border border-tb-slate-300 px-3.5 py-2 text-sm font-semibold text-tb-slate-800 transition hover:border-tb-slate-500"
      >
        <Icon name="doc" class="h-4 w-4" />編輯訂單
      </button>
      <form method="POST" action="?/cancel">
        <Button variant="danger" size="md" type="submit">取消訂單</Button>
      </form>
    {/if}
    {#if o.status === "ready" || o.status === "picked_up" || o.status === "no_show"}
      <a
        href={`/orders/${o.id}/dispute`}
        class="inline-flex items-center gap-2 rounded-tb-lg border border-tb-slate-300 px-3.5 py-2 text-sm font-semibold text-tb-slate-800 transition hover:border-tb-slate-500"
      >
        <Icon name="alert" class="h-4 w-4 text-tb-amber-600" />{o.status === "ready"
          ? "找不到餐？提出申訴"
          : "提出申訴"}
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
