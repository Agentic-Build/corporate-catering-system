<script lang="ts">
  // Global 領餐碼 modal. Driven by the layout's list of today's `ready`
  // orders: 0 → empty state, 1 → show its code directly, many → order
  // picker first. Codes are fetched (and re-fetched on expiry) from the
  // /api/employee/orders/{id}/pickup-code server proxy.
  import { Modal, EmptyState, Icon } from "@tbite/ui";
  import TotpView from "./TotpView.svelte";

  export interface ReadyOrder {
    id: string;
    supply_date: string;
    plant: string;
    total_price_minor: number;
    item_count: number;
  }

  interface Props {
    open: boolean;
    onClose: () => void;
    orders: ReadyOrder[];
  }
  let { open, onClose, orders }: Props = $props();

  type Code = { order_id: string; code: string; expires_in_seconds: number };

  let selectedId = $state<string | null>(null);
  let code = $state<Code | null>(null);
  let loading = $state(false);
  let loadError = $state<string | null>(null);

  // Which order's code we are showing — auto-select when there's exactly one.
  const activeId = $derived(selectedId ?? (orders.length === 1 ? orders[0].id : null));

  async function fetchCode(id: string) {
    loading = true;
    loadError = null;
    try {
      const r = await fetch(`/api/employee/orders/${id}/pickup-code`);
      if (!r.ok) throw new Error("pickup code unavailable");
      code = (await r.json()) as Code;
    } catch {
      code = null;
      loadError = "無法取得領餐碼，請稍後再試。";
    } finally {
      loading = false;
    }
  }

  // Fetch (or clear) the code whenever the active order changes or the modal
  // opens/closes.
  $effect(() => {
    if (open && activeId) {
      fetchCode(activeId);
    } else if (!open) {
      selectedId = null;
      code = null;
      loadError = null;
    }
  });
</script>

<Modal {open} {onClose} title="我的領餐碼" width="max-w-sm">
  {#if orders.length === 0}
    <EmptyState icon="qr" title="目前沒有可領取的訂單" hint="餐點備妥後，領餐碼會自動出現於此。" />
  {:else if activeId === null}
    <!-- Multiple ready orders — pick one first. -->
    <p class="mb-3 text-sm text-tb-slate-600">你有多筆已備餐的訂單，請選擇要領取的訂單。</p>
    <ul class="grid gap-2">
      {#each orders as o (o.id)}
        <li>
          <button
            type="button"
            onclick={() => (selectedId = o.id)}
            class="flex w-full items-center gap-3 rounded-tb-2xl border border-tb-slate-200 bg-white p-3 text-left transition hover:border-tb-slate-400"
          >
            <span class="grid h-9 w-9 flex-shrink-0 place-items-center rounded-tb-xl bg-tb-red-50">
              <Icon name="doc" class="h-4 w-4 text-tb-red-600" />
            </span>
            <span class="min-w-0 flex-1">
              <span class="block truncate text-sm font-bold text-tb-slate-900">
                {o.supply_date} · {o.plant}
              </span>
              <span class="block text-xs text-tb-slate-500">
                {o.item_count} 份 · ${o.total_price_minor.toLocaleString()}
              </span>
            </span>
            <Icon name="chevron" class="h-4 w-4 -rotate-90 text-tb-slate-400" />
          </button>
        </li>
      {/each}
    </ul>
  {:else if loading && !code}
    <div class="grid place-items-center py-12">
      <div class="h-44 w-44 animate-pulse rounded-tb bg-tb-slate-100"></div>
    </div>
  {:else if loadError}
    <div class="rounded-tb-xl bg-tb-rose-50 px-3 py-3 text-sm text-tb-rose-700">{loadError}</div>
  {:else if code}
    {#if orders.length > 1}
      <button
        type="button"
        onclick={() => (selectedId = null)}
        class="mb-3 text-xs font-semibold text-tb-slate-500 hover:text-tb-slate-900"
        >← 選擇其他訂單</button
      >
    {/if}
    <TotpView
      orderId={code.order_id}
      code={code.code}
      expiresInSeconds={code.expires_in_seconds}
      onExpire={() => activeId && fetchCode(activeId)}
    />
  {/if}
</Modal>
