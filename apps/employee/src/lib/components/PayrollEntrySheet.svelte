<script lang="ts">
  // 薪資逐筆明細互動 sheet: ⭐評分 / 📣客訴 modes (?/rate, ?/complain actions).
  import { Modal, Button } from "@tbite/ui";
  import { enhance } from "$app/forms";
  import { invalidateAll } from "$app/navigation";

  export interface PayrollLine {
    order_id: string;
    supply_date: string;
    vendor_name: string;
    items_summary: string;
    amount_minor: number;
    status: string;
    rated: boolean;
    complaint_id?: string;
  }

  interface Props {
    open: boolean;
    line: PayrollLine | null;
    onClose: () => void;
  }
  let { open, line, onClose }: Props = $props();

  const complaintCategories = [
    { id: "wrong_item", label: "送錯餐點" },
    { id: "missing_item", label: "餐點短缺" },
    { id: "quality", label: "品質不佳" },
    { id: "portion", label: "份量不足" },
    { id: "hygiene", label: "衛生問題" },
    { id: "other", label: "其他問題" },
  ];

  let mode = $state<"rate" | "complain">("rate");
  let starValue = $state(0);
  let ratingError = $state<string | null>(null);
  let complaintError = $state<string | null>(null);
  let done = $state<"rated" | "complained" | null>(null);

  $effect(() => {
    if (open) {
      mode = line?.rated ? "complain" : "rate";
      starValue = 0;
      ratingError = null;
      complaintError = null;
      done = null;
    }
  });

  const alreadyComplained = $derived(Boolean(line?.complaint_id));
</script>

<Modal {open} {onClose} title="餐點回饋" width="max-w-sm">
  {#if line}
    <div class="mb-3 rounded-tb-xl bg-tb-slate-50 p-3">
      <p class="text-sm font-bold text-tb-slate-900">{line.vendor_name}</p>
      <p class="mt-0.5 text-xs text-tb-slate-500">{line.items_summary}</p>
      <p class="mt-1 font-jetbrains-mono text-xs text-tb-slate-500">{line.supply_date}</p>
    </div>

    <div class="mb-4 flex gap-2">
      <button
        type="button"
        onclick={() => (mode = "rate")}
        class="flex-1 rounded-tb-lg px-3 py-1.5 text-sm font-semibold transition {mode === 'rate'
          ? 'bg-tb-slate-900 text-white'
          : 'bg-tb-slate-100 text-tb-slate-700 hover:text-tb-slate-900'}"
      >
        ⭐ 評分
      </button>
      <button
        type="button"
        onclick={() => (mode = "complain")}
        class="flex-1 rounded-tb-lg px-3 py-1.5 text-sm font-semibold transition {mode ===
        'complain'
          ? 'bg-tb-slate-900 text-white'
          : 'bg-tb-slate-100 text-tb-slate-700 hover:text-tb-slate-900'}"
      >
        📣 客訴
      </button>
    </div>

    {#if mode === "rate"}
      {#if done === "rated"}
        <div class="rounded-tb-xl bg-tb-emerald-50 p-3 text-sm text-tb-emerald-700">
          <p class="font-semibold">已完成評分，感謝你的回饋。</p>
        </div>
      {:else if line.rated}
        <div class="rounded-tb-xl bg-tb-slate-50 p-3 text-sm text-tb-slate-600">
          此訂單已評分過了。
        </div>
      {:else}
        {#if ratingError}
          <p class="mb-3 rounded-tb-xl bg-tb-rose-50 px-3 py-2 text-sm text-tb-rose-700">
            {ratingError}
          </p>
        {/if}
        <form
          method="POST"
          action="/payroll?/rate"
          class="space-y-3"
          use:enhance={() => {
            ratingError = null;
            return async ({ result }) => {
              if (result.type === "success" && result.data?.ratingOk) {
                done = "rated";
                await invalidateAll();
              } else if (result.type === "failure") {
                ratingError = String(result.data?.ratingError ?? "送出評分失敗，請稍後再試。");
              }
            };
          }}
        >
          <input type="hidden" name="order_id" value={line.order_id} />
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
          <Button variant="primary" size="md" type="submit" fullWidth disabled={starValue === 0}>
            送出評分
          </Button>
        </form>
      {/if}
    {:else if done === "complained"}
      <div class="rounded-tb-xl bg-tb-emerald-50 p-3 text-sm text-tb-emerald-700">
        <p class="font-semibold">已送出回報，商家會收到並回覆。</p>
        <a
          href="/complaints"
          class="mt-1 inline-block font-semibold text-tb-red-600 hover:text-tb-red-700"
        >
          查看客訴進度 →
        </a>
      </div>
    {:else if alreadyComplained}
      <div class="rounded-tb-xl bg-tb-slate-50 p-3 text-sm text-tb-slate-600">
        此訂單已有客訴紀錄。
        <a href="/complaints" class="ml-1 font-semibold text-tb-red-600 hover:text-tb-red-700">
          查看進度 →
        </a>
      </div>
    {:else}
      {#if complaintError}
        <p class="mb-3 rounded-tb-xl bg-tb-rose-50 px-3 py-2 text-sm text-tb-rose-700">
          {complaintError}
        </p>
      {/if}
      <form
        method="POST"
        action="/payroll?/complain"
        class="space-y-3"
        use:enhance={() => {
          complaintError = null;
          return async ({ result }) => {
            if (result.type === "success" && result.data?.complaintOk) {
              done = "complained";
              await invalidateAll();
            } else if (result.type === "failure") {
              complaintError = String(result.data?.complaintError ?? "送出客訴失敗，請稍後再試。");
            }
          };
        }}
      >
        <input type="hidden" name="order_id" value={line.order_id} />
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
        <Button variant="primary" size="md" type="submit" fullWidth>送出回報</Button>
      </form>
    {/if}
  {/if}
</Modal>
