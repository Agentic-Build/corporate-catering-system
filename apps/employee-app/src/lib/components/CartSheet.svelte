<script lang="ts">
  // Cart bottom drawer: line items with steppers, a note field, and the
  // submit-order action. On success it swaps to a confirmation panel.
  import { goto } from "$app/navigation";
  import { placeOrder } from "$lib/api";
  import { cart } from "$lib/cart.svelte";
  import { money, PLANTS } from "$lib/sample";
  import { session } from "$lib/session.svelte";
  import AppIcon from "./AppIcon.svelte";
  import BottomSheet from "./BottomSheet.svelte";

  interface Props {
    open: boolean;
    onClose: () => void;
    /** ISO supply date for the order being placed. */
    supplyDate: string;
  }
  let { open, onClose, supplyDate }: Props = $props();

  let notes = $state("");
  let submitting = $state(false);
  let ordered = $state(false);
  let error = $state<string | null>(null);

  const items = $derived(Object.entries(cart.items));
  const plant = $derived(session.user?.plant ?? PLANTS[0].id);

  // Reset transient state whenever the sheet closes.
  $effect(() => {
    if (!open) {
      ordered = false;
      notes = "";
      error = null;
      submitting = false;
    }
  });

  async function submit() {
    if (cart.count === 0 || submitting) return;
    submitting = true;
    error = null;
    try {
      await placeOrder({
        plant,
        supply_date: supplyDate,
        notes: notes.trim(),
        items: Object.entries(cart.items).map(([id, l]) => ({
          menu_item_id: id,
          qty: l.qty,
        })),
      });
      ordered = true;
    } catch (e) {
      error = e instanceof Error ? e.message : "送出預訂失敗";
    } finally {
      submitting = false;
    }
  }

  function done() {
    cart.clear();
    onClose();
    goto("/orders");
  }
</script>

<BottomSheet {open} {onClose} maxHeight="88%">
  {#if ordered}
    <div class="flex flex-col items-center justify-center px-8 py-14 text-center">
      <div class="mb-4 grid h-16 w-16 place-items-center rounded-full bg-tb-emerald-500 text-white">
        <AppIcon name="check" class="h-8 w-8" />
      </div>
      <h2 class="text-xl font-black text-tb-slate-900">預訂成功!</h2>
      <p class="mt-1 text-sm text-tb-slate-500">
        款項將於本月薪資代扣,截單前可至訂單頁修改。
      </p>
      <button
        type="button"
        onclick={done}
        class="mt-6 rounded-2xl bg-tb-slate-900 px-6 py-3 text-sm font-bold text-white"
      >
        查看訂單
      </button>
    </div>
  {:else}
    <div class="flex items-center justify-between border-b border-tb-slate-100 px-5 py-3">
      <h2 class="text-lg font-extrabold text-tb-slate-900">購物車 · {cart.count} 份</h2>
      <button
        type="button"
        class="grid h-8 w-8 place-items-center rounded-full bg-tb-slate-100 text-lg text-tb-slate-600"
        onclick={onClose}
      >
        ✕
      </button>
    </div>

    {#if cart.vendorName}
      <div class="border-b border-tb-slate-100 bg-tb-slate-50 px-5 py-2 text-xs text-tb-slate-500">
        🏪 {cart.vendorName}
      </div>
    {/if}

    <div class="no-scroll grid flex-1 gap-3 overflow-y-auto px-5 py-3">
      {#each items as [id, line] (id)}
        <div class="flex items-center gap-3 rounded-2xl bg-tb-slate-50 p-3">
          <div class="min-w-0 flex-1">
            <div class="truncate text-sm font-bold text-tb-slate-900">{line.name}</div>
            <div class="text-xs text-tb-slate-500">{money(line.price)} 元／份</div>
          </div>
          <div class="flex items-center gap-1 rounded-full bg-white p-1 ring-1 ring-tb-slate-200">
            <button
              type="button"
              aria-label="減少"
              class="grid h-7 w-7 place-items-center rounded-full bg-tb-slate-100 text-tb-slate-700"
              onclick={() => cart.remove(id) ?? undefined}
            >
              <AppIcon name="minus" class="h-3.5 w-3.5" />
            </button>
            <span class="w-6 text-center text-sm font-black tabular-nums">{line.qty}</span>
            <button
              type="button"
              aria-label="增加"
              class="grid h-7 w-7 place-items-center rounded-full bg-tb-slate-100 text-tb-slate-700"
              onclick={() =>
                cart.set(
                  id,
                  line.qty + 1,
                  { name: line.name, price: line.price },
                  { id: cart.vendorId ?? "", name: cart.vendorName },
                )}
            >
              <AppIcon name="plus" class="h-3.5 w-3.5" />
            </button>
          </div>
          <div class="w-14 text-right text-sm font-black tabular-nums text-tb-slate-900">
            {money(line.price * line.qty)}
          </div>
        </div>
      {/each}

      {#if items.length === 0}
        <p class="py-10 text-center text-sm text-tb-slate-400">購物車是空的</p>
      {/if}

      <div>
        <div class="mb-1.5 text-xs font-bold text-tb-slate-500">備註給商家</div>
        <textarea
          bind:value={notes}
          maxlength="100"
          placeholder="例:不要香菜、醬料分開…"
          class="h-16 w-full resize-none rounded-2xl bg-tb-slate-50 px-3 py-2.5 text-sm outline-none ring-1 ring-tb-slate-200 focus:ring-tb-slate-400"
        ></textarea>
      </div>
    </div>

    <div class="border-t border-tb-slate-100 px-5 pb-6 pt-3">
      {#if error}
        <p class="mb-2 rounded-lg bg-tb-rose-50 px-3 py-2 text-xs text-tb-rose-700">{error}</p>
      {/if}
      <div class="mb-3 flex justify-between text-sm">
        <span class="text-tb-slate-500">合計(薪資代扣)</span>
        <span class="text-xl font-black tabular-nums text-tb-slate-900">{money(cart.total)}</span>
      </div>
      <button
        type="button"
        disabled={cart.count === 0 || submitting}
        onclick={submit}
        class="w-full rounded-2xl bg-tb-red-600 py-4 text-sm font-extrabold text-white active:bg-tb-red-700 disabled:bg-tb-slate-200 disabled:text-tb-slate-400"
      >
        {submitting ? "送出中…" : "送出預訂"}
      </button>
    </div>
  {/if}
</BottomSheet>
