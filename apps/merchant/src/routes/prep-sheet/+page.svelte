<script lang="ts">
  import { PageHeader, Button, Icon, EmptyState } from "@tbite/ui";
  import { dayId } from "@tbite/web-shared";

  let { data } = $props();

  interface Item {
    menu_item_id: string;
    name: string;
    qty: number;
  }
  interface Order {
    order_id: string;
    order_number: number;
    total_price_minor: number;
    notes: string;
    items: Item[];
  }
  interface Plant {
    plant: string;
    order_count: number;
    portion_count: number;
    items: Item[];
    orders: Order[];
  }
  const plants = $derived((data.sheet.plants ?? []) as Plant[]);

  const days = $derived.by(() => {
    const out: { id: string; label: string }[] = [];
    for (let i = 0; i < 7; i++) {
      const id = dayId(i);
      out.push({ id, label: i === 0 ? "今天" : i === 1 ? "明天" : id.slice(5) });
    }
    return out;
  });

  function exportCsv() {
    const rows: string[][] = [["廠區", "訂單", "品項", "份數", "備註"]];
    for (const p of plants) {
      for (const o of p.orders) {
        for (const it of o.items) {
          rows.push([p.plant, `#${o.order_number}`, it.name, String(it.qty), o.notes ?? ""]);
        }
      }
    }
    const csv =
      "﻿" +
      rows.map((r) => r.map((c) => `"${String(c).replace(/"/g, '""')}"`).join(",")).join("\n");
    const blob = new Blob([csv], { type: "text/csv;charset=utf-8" });
    const a = document.createElement("a");
    a.href = URL.createObjectURL(blob);
    a.download = `prep-sheet-${data.date}.csv`;
    a.click();
    URL.revokeObjectURL(a.href);
  }
</script>

<PageHeader
  eyebrow="Prep Sheet · 備餐與配送輸出"
  title="備餐輸出"
  subtitle="{data.date} · {data.sheet.total_orders} 筆訂單 · {data.sheet.total_portions} 份"
>
  {#snippet actions()}
    <Button variant="secondary" size="sm" onclick={exportCsv}>
      <Icon name="download" class="h-3.5 w-3.5" />匯出 CSV
    </Button>
    <Button variant="primary" size="sm" onclick={() => window.print()}>
      <Icon name="doc" class="h-3.5 w-3.5" />列印備餐單
    </Button>
  {/snippet}
</PageHeader>

<div class="mb-4 flex flex-wrap gap-1 rounded-full bg-tb-slate-100 p-1 print:hidden">
  {#each days as d (d.id)}
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

{#if plants.length === 0}
  <EmptyState icon="doc" title="本日無待備餐訂單" hint="員工下單後，分區表與配送清單會顯示於此。" />
{:else}
  <div class="space-y-6">
    {#each plants as p (p.plant)}
      <section class="rounded-tb-2xl border border-tb-slate-200 bg-white p-5 shadow-tb-sm">
        <header class="mb-3 flex items-center justify-between">
          <h2 class="text-lg font-black text-tb-slate-900">{p.plant}</h2>
          <span class="font-jetbrains-mono text-xs text-tb-slate-500">
            {p.order_count} 筆 · {p.portion_count} 份
          </span>
        </header>

        <!-- 廠區分區表 — aggregated item counts -->
        <h3 class="mb-1 text-xs font-bold uppercase tracking-eyebrow text-tb-red-600">分區彙總</h3>
        <table class="mb-4 w-full text-sm">
          <thead class="sr-only">
            <tr>
              <th scope="col">品項名稱</th>
              <th scope="col">份數</th>
            </tr>
          </thead>
          <tbody class="divide-y divide-tb-slate-100">
            {#each p.items as it (it.menu_item_id)}
              <tr>
                <td class="py-1.5 font-semibold text-tb-slate-900">{it.name}</td>
                <td class="py-1.5 text-right font-jetbrains-mono tabular-nums">× {it.qty}</td>
              </tr>
            {/each}
          </tbody>
        </table>

        <!-- 配送籃清單 — one row per order -->
        <h3 class="mb-1 text-xs font-bold uppercase tracking-eyebrow text-tb-red-600">
          配送籃清單
        </h3>
        <table class="w-full text-sm">
          <thead class="text-left text-[11px] font-bold uppercase text-tb-slate-500">
            <tr>
              <th scope="col" class="py-1.5">訂單</th>
              <th scope="col" class="py-1.5">品項</th>
              <th scope="col" class="py-1.5">備註</th>
            </tr>
          </thead>
          <tbody class="divide-y divide-tb-slate-100">
            {#each p.orders as o (o.order_id)}
              <tr>
                <td class="py-1.5 font-jetbrains-mono text-xs text-tb-slate-600">
                  #{o.order_number}
                </td>
                <td class="py-1.5">
                  {o.items.map((it) => `${it.name}×${it.qty}`).join("、")}
                </td>
                <td class="py-1.5 text-xs text-tb-amber-800">{o.notes || "—"}</td>
              </tr>
            {/each}
          </tbody>
        </table>

        <!-- 餐點標籤 — one card per order, for the basket -->
        <h3 class="mb-2 mt-4 text-xs font-bold uppercase tracking-eyebrow text-tb-red-600">
          餐點標籤
        </h3>
        <div class="grid grid-cols-2 gap-2 md:grid-cols-3">
          {#each p.orders as o (o.order_id)}
            <div class="rounded-tb-xl border border-tb-slate-300 p-2.5 text-xs">
              <div class="flex items-center justify-between">
                <span class="font-bold text-tb-slate-900">{p.plant}</span>
                <span class="font-jetbrains-mono text-tb-slate-500">{o.order_id.slice(0, 8)}</span>
              </div>
              <div class="mt-1 text-tb-slate-800">
                {o.items.map((it) => `${it.name}×${it.qty}`).join("、")}
              </div>
              {#if o.notes}
                <div class="mt-1 font-semibold text-tb-amber-800">※ {o.notes}</div>
              {/if}
            </div>
          {/each}
        </div>
      </section>
    {/each}
  </div>
{/if}
