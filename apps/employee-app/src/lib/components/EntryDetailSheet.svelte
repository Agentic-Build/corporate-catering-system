<script lang="ts">
  // Payroll-entry detail drawer with two modes:
  //  ⭐ rating  — 5-star + tag chips, posts to /orders/{id}/rating
  //  📣 dispute — tag chips + description, posts to /orders/{id}/complaint
  import { fileComplaint, rateOrder, type PayrollLine } from "$lib/api";
  import { BAD_TAGS, GOOD_TAGS, money } from "$lib/sample";
  import AppIcon from "./AppIcon.svelte";
  import BottomSheet from "./BottomSheet.svelte";

  interface Props {
    entry: PayrollLine | null;
    onClose: () => void;
    /** Called after a rating succeeds so the list can mark the row rated. */
    onRated: (orderId: string) => void;
  }
  let { entry, onClose, onRated }: Props = $props();

  let mode = $state<"rating" | "dispute">("rating");
  let rating = $state(0);
  let tags = $state<string[]>([]);
  let note = $state("");
  let submitting = $state(false);
  let submitted = $state(false);
  let error = $state<string | null>(null);

  // Reset whenever a new entry is opened.
  $effect(() => {
    if (entry) {
      mode = "rating";
      rating = 0;
      tags = [];
      note = "";
      submitted = false;
      submitting = false;
      error = null;
    }
  });

  const open = $derived(entry != null);
  const isCharged = $derived(entry?.status === "charged");
  const tagPool = $derived(
    mode === "dispute" ? BAD_TAGS : rating >= 4 || rating === 0 ? GOOD_TAGS : BAD_TAGS,
  );
  const disabled = $derived(
    mode === "rating"
      ? rating === 0 || !isCharged || (entry?.rated ?? false)
      : tags.length === 0 && note.trim().length < 5,
  );

  function toggleTag(t: string) {
    tags = tags.includes(t) ? tags.filter((x) => x !== t) : [...tags, t];
  }

  async function submit() {
    if (!entry || disabled || submitting) return;
    submitting = true;
    error = null;
    try {
      if (mode === "rating") {
        await rateOrder(entry.order_id, rating, tags, note.trim());
        onRated(entry.order_id);
      } else {
        await fileComplaint(entry.order_id, tags, note.trim());
      }
      submitted = true;
    } catch (e) {
      error = e instanceof Error ? e.message : "送出失敗";
    } finally {
      submitting = false;
    }
  }
</script>

<BottomSheet {open} {onClose}>
  {#if entry}
    {#if submitted}
      <div class="flex flex-col items-center justify-center gap-4 px-6 py-12 text-center">
        <div class="grid h-14 w-14 place-items-center rounded-full bg-tb-emerald-500 text-white">
          <AppIcon name="check" class="h-7 w-7" />
        </div>
        <h3 class="text-lg font-black text-tb-slate-900">
          已送出{mode === "rating" ? "評分" : "客訴"}!
        </h3>
        <p class="text-sm text-tb-slate-500">
          {mode === "rating"
            ? "感謝你的評分,幫助其他同事選餐。"
            : "已通知商家,24 小時內會回覆。"}
        </p>
        <button
          type="button"
          onclick={onClose}
          class="rounded-2xl bg-tb-slate-900 px-6 py-3 text-sm font-bold text-white"
        >
          關閉
        </button>
      </div>
    {:else}
      <div class="no-scroll flex-1 overflow-y-auto px-5 py-4">
        <div class="mb-4 rounded-2xl bg-tb-slate-50 p-3.5">
          <div class="text-xs font-bold text-tb-slate-900">{entry.vendor_name}</div>
          <div class="mt-0.5 text-[11px] text-tb-slate-500">
            {entry.items_summary} · {entry.supply_date} · {money(entry.amount_minor)}
          </div>
        </div>

        <div class="mb-4 flex gap-1 rounded-full bg-tb-slate-100 p-1">
          <button
            type="button"
            disabled={!isCharged}
            onclick={() => {
              mode = "rating";
              tags = [];
            }}
            class="flex-1 rounded-full py-2 text-xs font-bold disabled:opacity-40 {mode ===
            'rating'
              ? 'bg-white text-tb-slate-900 shadow-sm'
              : 'text-tb-slate-500'}"
          >
            ⭐ 評分{entry.rated ? " ✓" : ""}
          </button>
          <button
            type="button"
            onclick={() => {
              mode = "dispute";
              tags = [];
            }}
            class="flex-1 rounded-full py-2 text-xs font-bold {mode === 'dispute'
              ? 'bg-white text-tb-slate-900 shadow-sm'
              : 'text-tb-slate-500'}"
          >
            📣 {isCharged ? "客訴" : "申訴扣款"}
          </button>
        </div>

        {#if mode === "rating"}
          <div class="mb-4 flex justify-center gap-2">
            {#each [1, 2, 3, 4, 5] as n (n)}
              <button
                type="button"
                aria-label={`${n} 星`}
                onclick={() => (rating = n)}
                class="text-4xl transition {rating >= n
                  ? 'scale-110 text-tb-amber-400'
                  : 'text-tb-slate-200'}"
              >
                ★
              </button>
            {/each}
          </div>
        {/if}

        <div class="mb-4 flex flex-wrap gap-1.5">
          {#each tagPool as t (t)}
            {@const on = tags.includes(t)}
            <button
              type="button"
              onclick={() => toggleTag(t)}
              class="rounded-full px-3 py-1.5 text-[11px] font-bold {on
                ? mode === 'rating' && rating >= 4
                  ? 'bg-tb-emerald-600 text-white'
                  : 'bg-tb-rose-600 text-white'
                : 'bg-tb-slate-100 text-tb-slate-700'}"
            >
              {t}
            </button>
          {/each}
        </div>

        <textarea
          bind:value={note}
          maxlength="200"
          placeholder={mode === "rating" ? "其他想說的…" : "請描述問題狀況…"}
          class="mb-4 h-20 w-full resize-none rounded-2xl bg-tb-slate-50 px-3 py-2.5 text-sm outline-none ring-1 ring-tb-slate-200 focus:ring-tb-slate-400"
        ></textarea>

        {#if error}
          <p class="mb-3 rounded-lg bg-tb-rose-50 px-3 py-2 text-xs text-tb-rose-700">{error}</p>
        {/if}

        <button
          type="button"
          {disabled}
          onclick={submit}
          class="mb-2 w-full rounded-2xl bg-tb-red-600 py-4 text-sm font-extrabold text-white disabled:bg-tb-slate-200 disabled:text-tb-slate-400"
        >
          {#if submitting}
            送出中…
          {:else if mode === "rating"}
            {entry.rated ? "已評分" : "送出評分"}
          {:else}
            送出客訴
          {/if}
        </button>
      </div>
    {/if}
  {/if}
</BottomSheet>
