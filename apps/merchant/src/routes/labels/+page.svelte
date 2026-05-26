<script lang="ts">
  import { PageHeader, Button, Icon, EmptyState } from "@tbite/ui";
  import { buildPickupQR } from "@tbite/pickup";

  let { data } = $props();

  interface OrderItem {
    menu_item_id: string;
    qty: number;
  }
  interface MerchantOrder {
    id: string;
    plant: string;
    status: string;
    items: OrderItem[];
  }
  const orders = $derived((data.orders ?? []) as MerchantOrder[]);

  // QR is generated client-side: order id → tbite://pickup?order=<id> → dataURL.
  // Keyed by order id so each label renders its own sticker QR.
  let qrByOrder = $state<Record<string, string>>({});

  // 用 $effect 而非 onMount：日期分頁靠 query 導航不會重新 mount，
  // 但 orders ($derived) 會反應式更新；讀取 orders 讓本 effect 訂閱其變更並重算 QR。
  $effect(() => {
    // 同步讀取 orders 才能讓 effect 訂閱其變更（await 後讀取不會被追蹤）。
    const currentOrders = orders;
    let cancelled = false;
    (async () => {
      // qrcode is CommonJS and breaks SvelteKit/Vite SSR, so import it lazily
      // here — $effect only runs in the browser, keeping it out of the SSR graph.
      const mod = await import("qrcode");
      const QRCode = (mod as unknown as { default?: typeof mod }).default ?? mod;
      const next: Record<string, string> = {};
      for (const o of currentOrders) {
        next[o.id] = await QRCode.toDataURL(buildPickupQR(o.id), {
          width: 200,
          margin: 1,
          color: { dark: "#0f172a", light: "#ffffff" },
        });
      }
      if (!cancelled) qrByOrder = next;
    })();
    return () => {
      cancelled = true;
    };
  });
</script>

<PageHeader
  eyebrow="Labels · 餐點貼紙"
  title="貼紙匯出"
  subtitle="{data.date} · {data.totalCount} 筆訂單"
>
  {#snippet actions()}
    <Button variant="primary" size="sm" onclick={() => window.print()}>
      <Icon name="doc" class="h-3.5 w-3.5" />列印貼紙
    </Button>
  {/snippet}
</PageHeader>

<div class="mb-4 flex flex-wrap gap-1 rounded-full bg-tb-slate-100 p-1 print:hidden">
  {#each data.days as d (d.id)}
    <a
      href="?date={d.id}"
      class="rounded-full px-3 py-1 text-xs font-semibold {data.date === d.id
        ? 'bg-tb-slate-900 text-white'
        : 'text-tb-slate-700 hover:text-tb-slate-900'}"
    >
      {d.label}
    </a>
  {/each}
</div>

{#if orders.length === 0}
  <EmptyState icon="doc" title="本日無訂單" hint="員工下單後，餐點貼紙會顯示於此供列印。" />
{:else}
  <div class="label-grid">
    {#each orders as o (o.id)}
      <div class="label">
        <div class="label-id font-jetbrains-mono">{o.id.slice(0, 8)}</div>
        {#if qrByOrder[o.id]}
          <img class="label-qr" src={qrByOrder[o.id]} alt="QR {o.id.slice(0, 8)}" />
        {:else}
          <div class="label-qr label-qr-placeholder"></div>
        {/if}
        <div class="label-meta">
          <span>{o.plant}</span>
          <span class="font-jetbrains-mono">{data.date}</span>
        </div>
        {#if o.items?.length}
          <ul class="label-items">
            {#each o.items as item (item.menu_item_id)}
              <li>{data.itemsById[item.menu_item_id]?.name ?? "未知餐點"} ×{item.qty}</li>
            {/each}
          </ul>
        {/if}
      </div>
    {/each}
  </div>
{/if}

<style>
  .label-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(180px, 1fr));
    gap: 0.75rem;
  }
  .label {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 0.5rem;
    border: 1px solid #cbd5e1;
    border-radius: 0.75rem;
    padding: 0.75rem;
    background: #ffffff;
    /* keep a single sticker intact across page breaks when printing */
    break-inside: avoid;
    page-break-inside: avoid;
  }
  .label-id {
    font-size: 1rem;
    font-weight: 700;
    letter-spacing: 0.05em;
    color: #0f172a;
  }
  .label-qr {
    width: 140px;
    height: 140px;
  }
  .label-qr-placeholder {
    background: #f1f5f9;
    border-radius: 0.5rem;
  }
  .label-meta {
    display: flex;
    width: 100%;
    justify-content: space-between;
    font-size: 0.7rem;
    color: #475569;
  }
  .label-items {
    width: 100%;
    list-style: none;
    margin: 0;
    padding: 0;
    font-size: 0.65rem;
    color: #334155;
    border-top: 1px solid #e2e8f0;
    padding-top: 0.25rem;
    line-height: 1.5;
  }

  @media print {
    /* Tighten the grid into a printable sticker sheet; hide chrome. */
    .label-grid {
      grid-template-columns: repeat(3, 1fr);
      gap: 0.4rem;
    }
    .label {
      border-color: #94a3b8;
    }
  }
</style>
