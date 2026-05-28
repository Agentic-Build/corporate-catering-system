<script lang="ts">
  // Cart drawer; submits `?/placeOrder` on the home route via hidden form.
  import { Drawer, Icon, Button } from "@tbite/ui";
  import { enhance } from "$app/forms";
  import { cart } from "$lib/cart.svelte";

  interface Props {
    open: boolean;
    onClose: () => void;
    plant: string;
    supplyDate: string;
  }
  let { open, onClose, plant, supplyDate }: Props = $props();

  const entries = $derived(Object.entries(cart.items));

  // Show `?/placeOrder` errors inside the drawer, not behind on the page.
  let submitError = $state<string | null>(null);
  let submitting = $state(false);
</script>

<Drawer {open} {onClose}>
  {#snippet header()}
    <div class="flex items-center justify-between">
      <div>
        <div class="text-xs text-tb-slate-500">購物車</div>
        <h2 class="text-lg font-extrabold text-tb-slate-900">本日預訂 · {cart.count} 份</h2>
      </div>
      <button
        type="button"
        onclick={onClose}
        class="rounded-lg p-2 text-tb-slate-500 transition hover:bg-tb-slate-100"
        aria-label="關閉"
      >
        <Icon name="close" class="h-5 w-5" />
      </button>
    </div>
  {/snippet}

  {#if entries.length === 0}
    <div class="grid h-full place-items-center text-center">
      <div>
        <Icon name="cart" class="mx-auto h-10 w-10 text-tb-slate-300" />
        <p class="mt-2 text-sm text-tb-slate-500">尚未選購任何餐點</p>
      </div>
    </div>
  {:else}
    <ul class="grid gap-3">
      {#each entries as [id, line] (id)}
        <li class="flex items-center gap-3 rounded-tb-2xl border border-tb-slate-200 bg-white p-3">
          {#if line.image}
            <img
              src={line.image}
              alt=""
              class="h-14 w-14 flex-shrink-0 rounded-tb-xl object-cover"
            />
          {:else}
            <div
              class="grid h-14 w-14 flex-shrink-0 place-items-center rounded-tb-xl bg-tb-slate-100 text-[9px] uppercase tracking-eyebrow text-tb-slate-400"
            >
              無圖
            </div>
          {/if}
          <div class="min-w-0 flex-1">
            <div class="truncate text-sm font-bold text-tb-slate-900">{line.name}</div>
            <div class="text-xs text-tb-slate-500">{line.vendor}</div>
            <div
              class="mt-0.5 font-jetbrains-mono text-sm font-bold tabular-nums text-tb-slate-800"
            >
              ${line.price.toLocaleString()}
            </div>
          </div>
          <div class="flex items-center gap-1.5">
            <button
              type="button"
              onclick={() => cart.dec(id)}
              class="grid min-h-[44px] min-w-[44px] place-items-center rounded-full border border-tb-slate-200 text-tb-slate-700 transition hover:bg-tb-slate-50"
              aria-label="減少"
            >
              <Icon name="minus" class="h-3.5 w-3.5" />
            </button>
            <span class="w-6 text-center font-jetbrains-mono text-sm font-bold tabular-nums"
              >{line.qty}</span
            >
            <button
              type="button"
              onclick={() => cart.inc(id)}
              class="grid min-h-[44px] min-w-[44px] place-items-center rounded-full border border-tb-slate-200 text-tb-slate-700 transition hover:bg-tb-slate-50"
              aria-label="增加"
            >
              <Icon name="plus" class="h-3.5 w-3.5" />
            </button>
          </div>
        </li>
      {/each}
    </ul>
  {/if}

  {#snippet footer()}
    <div class="grid gap-1 text-sm">
      <div class="flex justify-between text-tb-slate-600">
        <span>小計</span>
        <span class="font-jetbrains-mono font-semibold tabular-nums text-tb-slate-900"
          >${cart.total.toLocaleString()}</span
        >
      </div>
      <div class="flex justify-between text-tb-slate-600">
        <span>外送費</span><span class="font-semibold text-tb-slate-900">免費</span>
      </div>
      <div class="mt-1 flex items-end justify-between border-t border-tb-slate-200 pt-2">
        <span class="text-sm text-tb-slate-600">合計（月結）</span>
        <span class="font-jetbrains-mono text-2xl font-black tabular-nums text-tb-slate-900"
          >${cart.total.toLocaleString()}</span
        >
      </div>
    </div>
    <form
      method="POST"
      action="/?/placeOrder"
      use:enhance={() => {
        submitError = null;
        submitting = true;
        return async ({ result, update }) => {
          if (result.type === "redirect") cart.clear();
          else if (result.type === "failure")
            submitError = (result.data?.error as string) ?? "送出預訂失敗，請稍後再試。";
          await update();
          submitting = false;
        };
      }}
      class="mt-3"
    >
      <input type="hidden" name="plant" value={plant} />
      <input type="hidden" name="supply_date" value={supplyDate} />
      <label class="mb-2 flex flex-col gap-1 text-xs">
        <span class="font-semibold text-tb-slate-600">特殊需求備註（選填）</span>
        <textarea
          name="notes"
          rows="2"
          maxlength="500"
          placeholder="例如：不要辣、過敏原、餐具需求…"
          class="rounded-tb-lg border border-tb-slate-300 px-3 py-2 text-sm transition focus:border-tb-red-500 focus:outline-none focus:ring-4 focus:ring-tb-red-100"
        ></textarea>
      </label>
      {#each entries as [id, line] (id)}
        <input type="hidden" name="item_id" value={id} />
        <input type="hidden" name="qty" value={line.qty} />
      {/each}
      {#if submitError}
        <p class="mb-2 rounded-tb-xl bg-tb-rose-50 px-3 py-2 text-sm text-tb-rose-700" role="alert">
          {submitError}
        </p>
      {/if}
      <Button
        variant="primary"
        size="md"
        type="submit"
        fullWidth
        disabled={submitting || entries.length === 0}
      >
        {submitting ? "送出中…" : "送出預訂 · 本月月結"}
      </Button>
    </form>
    <p class="mt-2 text-center text-[11px] text-tb-slate-500">截單前可至「我的訂單」修改或取消。</p>
  {/snippet}
</Drawer>
