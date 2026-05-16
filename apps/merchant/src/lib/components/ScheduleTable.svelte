<script lang="ts">
  // Single-day schedule table — ported from MerchantView.jsx ScheduleTable.
  // Cap edits post the `setSupply` action; "移除" sets capacity to 0.
  // Today's row is read-only. There is no per-day on/off control: publish/
  // archive are global to the menu item and live on /menus + the library.
  import { Button, Icon } from "@tbite/ui";
  import OrderProgress from "./OrderProgress.svelte";

  interface Slot {
    itemId: string;
    name: string;
    description: string;
    image: string | null;
    price: number;
    cap: number;
    ordered: number;
    pickupWindow: string;
  }
  interface Day {
    id: string;
    head: string;
    weekday: string;
    offset: number;
  }
  interface Props {
    day: Day;
    slots: Slot[];
    onOpenLibrary: () => void;
    /** Submits a `setSupply` post for `itemId` on `day.id` at `capacity`. */
    submitCap: (itemId: string, capacity: number, pickupWindow: string) => void;
  }
  let { day, slots, onOpenLibrary, submitCap }: Props = $props();

  const isToday = $derived(day.offset === 0);
  const isTomorrow = $derived(day.offset === 1);
  const canEdit = $derived(!isToday);
  const empty = $derived(slots.length === 0);

  // Optimistic cap overrides keyed by itemId — updated immediately on each
  // click so consecutive edits compound (server data only refreshes after
  // the enhanced form resolves). Reconciled to server values once `slots`
  // reloads with the committed capacity.
  let capOverride = $state<Record<string, number>>({});
  $effect(() => {
    const next = { ...capOverride };
    let changed = false;
    for (const s of slots) {
      if (next[s.itemId] !== undefined && next[s.itemId] === s.cap) {
        delete next[s.itemId];
        changed = true;
      }
    }
    if (changed) capOverride = next;
  });

  function capOf(slot: Slot): number {
    return capOverride[slot.itemId] ?? slot.cap;
  }
  const totalCap = $derived(slots.reduce((s, x) => s + capOf(x), 0));
  const totalOrdered = $derived(slots.reduce((s, x) => s + x.ordered, 0));

  function setCap(slot: Slot, next: number) {
    capOverride = { ...capOverride, [slot.itemId]: next };
    submitCap(slot.itemId, next, slot.pickupWindow);
  }
  function step(slot: Slot, delta: number) {
    setCap(slot, Math.max(slot.ordered, Math.max(5, capOf(slot) + delta)));
  }
  function onInput(slot: Slot, value: string) {
    setCap(slot, Math.max(slot.ordered, Math.max(5, parseInt(value, 10) || 0)));
  }
  function remove(slot: Slot) {
    // "移除" delists the item for this day — capacity 0.
    setCap(slot, 0);
  }
</script>

<section
  class="overflow-hidden rounded-tb-2xl border border-tb-slate-200 bg-white shadow-tb-sm"
>
  <header
    class="flex flex-wrap items-center justify-between gap-3 border-b border-tb-slate-100 px-5 py-4"
  >
    <div>
      <div class="flex items-center gap-2">
        <h3 class="text-base font-extrabold text-tb-slate-900">{day.head}的菜色</h3>
        <span class="font-jetbrains-mono text-[11px] text-tb-slate-500">{day.weekday}</span>
      </div>
      <p class="mt-0.5 text-xs text-tb-slate-500">
        {#if isToday}今日已截單 · 備餐進行中，無法再變更菜色。
        {:else if isTomorrow}今日 17:00 截單 · 之後將無法加入或下架。
        {:else}員工可預訂中 · 截單前可自由增減與調整上限。{/if}
      </p>
    </div>
    {#if canEdit}
      <div class="flex items-center gap-2">
        <Button variant="primary" size="sm" onclick={onOpenLibrary}>
          <Icon name="plus" class="h-3.5 w-3.5" />從餐點庫加入
        </Button>
      </div>
    {/if}
  </header>

  {#if empty}
    <div class="grid place-items-center px-6 py-14 text-center">
      <Icon name="doc" class="h-9 w-9 text-tb-slate-300" />
      <p class="mt-2 text-sm font-bold text-tb-slate-700">
        尚未為 {day.head} 排定任何菜色
      </p>
      <p class="mt-1 text-xs text-tb-slate-500">
        大部分商家會在前 3 日完成排菜。從餐點庫挑選即可重新上架。
      </p>
      {#if canEdit}
        <div class="mt-4 flex gap-2">
          <Button variant="primary" size="sm" onclick={onOpenLibrary}>
            <Icon name="plus" class="h-3.5 w-3.5" />從餐點庫加入
          </Button>
        </div>
      {/if}
    </div>
  {:else}
    <table class="w-full">
      <thead
        class="bg-tb-slate-50/60 text-left text-[11px] font-bold uppercase tracking-eyebrow text-tb-slate-500"
      >
        <tr>
          <th class="px-5 py-3">品項</th>
          <th class="px-3 py-3 text-right">售價</th>
          <th class="px-3 py-3 text-right">上限</th>
          <th class="px-3 py-3" style="min-width: 220px">已訂購</th>
          <th class="px-5 py-3"></th>
        </tr>
      </thead>
      <tbody class="divide-y divide-tb-slate-100">
        {#each slots as slot (slot.itemId)}
          {@const cap = capOf(slot)}
          <tr class="text-sm hover:bg-tb-slate-50/60">
            <td class="px-5 py-3">
              <div class="flex items-center gap-3">
                {#if slot.image}
                  <img
                    src={slot.image}
                    alt=""
                    class="h-12 w-12 flex-shrink-0 rounded-lg object-cover"
                  />
                {:else}
                  <div
                    class="grid h-12 w-12 flex-shrink-0 place-items-center rounded-lg bg-tb-slate-100 text-tb-slate-400"
                  >
                    <Icon name="doc" class="h-5 w-5" />
                  </div>
                {/if}
                <div class="min-w-0">
                  <div class="truncate font-bold text-tb-slate-900">{slot.name}</div>
                  <div class="truncate text-[11px] text-tb-slate-500">
                    {slot.description}
                  </div>
                </div>
              </div>
            </td>
            <td
              class="px-3 py-3 text-right font-jetbrains-mono font-bold tabular-nums text-tb-slate-900"
            >
              ${slot.price.toLocaleString()}
            </td>
            <td class="px-3 py-3 text-right">
              {#if canEdit}
                <div
                  class="inline-flex items-center overflow-hidden rounded-lg border border-tb-slate-200"
                >
                  <button
                    type="button"
                    disabled={cap <= 5}
                    onclick={() => step(slot, -5)}
                    class="grid h-8 w-8 place-items-center text-tb-slate-500 hover:bg-tb-slate-50 disabled:opacity-30"
                    aria-label="減少上限"
                  >
                    <Icon name="minus" class="h-3.5 w-3.5" />
                  </button>
                  <input
                    type="number"
                    value={cap}
                    min={slot.ordered}
                    onchange={(e) => onInput(slot, e.currentTarget.value)}
                    class="w-14 border-l border-r border-tb-slate-200 bg-transparent py-1 text-center font-jetbrains-mono text-sm font-bold tabular-nums text-tb-slate-900 focus:bg-tb-red-50 focus:outline-none"
                  />
                  <button
                    type="button"
                    onclick={() => step(slot, 5)}
                    class="grid h-8 w-8 place-items-center text-tb-slate-500 hover:bg-tb-slate-50"
                    aria-label="增加上限"
                  >
                    <Icon name="plus" class="h-3.5 w-3.5" />
                  </button>
                </div>
              {:else}
                <span class="font-jetbrains-mono font-bold tabular-nums text-tb-slate-700">
                  {cap}
                </span>
              {/if}
            </td>
            <td class="px-3 py-3">
              <OrderProgress ordered={slot.ordered} {cap} />
            </td>
            <td class="px-5 py-3 text-right">
              {#if canEdit}
                <Button variant="ghost" size="sm" onclick={() => remove(slot)}>
                  移除
                </Button>
              {/if}
            </td>
          </tr>
        {/each}
      </tbody>
      <tfoot>
        <tr class="border-t border-tb-slate-200 bg-tb-slate-50/60 text-sm">
          <td class="px-5 py-3 font-bold text-tb-slate-700">合計</td>
          <td class="px-3 py-3"></td>
          <td
            class="px-3 py-3 text-right font-jetbrains-mono font-bold tabular-nums text-tb-slate-900"
          >
            {totalCap}
          </td>
          <td class="px-3 py-3">
            <span
              class="font-jetbrains-mono text-sm font-bold tabular-nums text-tb-red-700"
            >
              共 {totalOrdered} 份已訂
            </span>
          </td>
          <td></td>
        </tr>
      </tfoot>
    </table>
  {/if}
</section>
