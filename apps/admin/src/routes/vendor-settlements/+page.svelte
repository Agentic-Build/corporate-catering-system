<script lang="ts">
  import { enhance } from "$app/forms";
  import { PageHeader, Card, StateTag, Button, Icon, EmptyState, Modal } from "@tbite/ui";
  let { data, form } = $props();

  /** Minor units → NT$ display, e.g. 12000 → "NT$120". */
  const ntd = (minor: number) => "NT$" + Math.round(minor).toLocaleString();

  const statusTone = {
    closed: "success",
    void: "neutral",
  } as Record<string, "info" | "neutral" | "warning" | "danger" | "success">;
  const statusLabel = {
    closed: "已關帳",
    void: "已作廢",
  } as Record<string, string>;

  const closed = $derived(data.settlements.filter((s: any) => s.status === "closed"));
  const totalGross = $derived(
    closed.reduce((sum: number, s: any) => sum + Number(s.gross_minor ?? 0), 0),
  );
  const totalOrders = $derived(
    closed.reduce((sum: number, s: any) => sum + Number(s.order_count ?? 0), 0),
  );
  const totalPortions = $derived(
    closed.reduce((sum: number, s: any) => sum + Number(s.portion_count ?? 0), 0),
  );

  // Confirm void modal
  let confirmVoidTarget = $state<{ id: string; vendorId: string } | null>(null);
  let voidFormEl = $state<HTMLFormElement | null>(null);
  let submittingVoid = $state(false);
</script>

<PageHeader
  eyebrow="商家對帳"
  title="商家結算總覽"
  subtitle="選擇期間檢視全商家結算單 · 關帳會逐商家聚合已備餐訂單並寫入稽核日誌"
/>

{#if form?.error}
  <p class="rounded-lg bg-tb-rose-50 px-3 py-2 text-sm text-tb-rose-700">{form.error}</p>
{/if}

<!-- Period picker -->
<Card title="結算期間">
  <form method="GET" class="flex flex-wrap items-end gap-3">
    <label class="flex flex-col gap-1">
      <span class="text-[11px] font-bold uppercase tracking-eyebrow text-tb-slate-500">月份</span>
      <input
        name="period"
        type="month"
        value={data.period}
        class="rounded-lg border border-tb-slate-300 px-3 py-2 text-sm text-tb-slate-900 focus:border-tb-slate-500 focus:outline-none focus:ring-2 focus:ring-tb-slate-300"
      />
    </label>
    <Button variant="secondary" size="md" type="submit">
      <Icon name="search" class="h-3.5 w-3.5" />檢視
    </Button>
  </form>
</Card>

<!-- Close period -->
<Card
  tone="info"
  title="關帳 · {data.period}"
  description="對該期間內每個有訂單的商家各產生一筆結算單；重複關同期間需先作廢"
>
  {#snippet actions()}
    <form method="POST" action="?/close">
      <input type="hidden" name="period" value={data.period} />
      <Button variant="primary" size="md" type="submit">
        <Icon name="check" class="h-3.5 w-3.5" />關帳此期間
      </Button>
    </form>
  {/snippet}
</Card>

<!-- Overview table -->
<Card
  title="結算總覽 · {data.period}"
  description={closed.length > 0
    ? `${closed.length} 家已關帳 · 訂單 ${totalOrders} 筆 · 份數 ${totalPortions} · 應付總額 ${ntd(totalGross)}`
    : "此期間尚無結算資料"}
>
  {#if data.settlements.length === 0}
    <EmptyState title="此期間尚無結算單" hint="按上方「關帳此期間」即可產生結算單" />
  {:else}
    <div class="overflow-x-auto rounded-xl border border-tb-slate-200">
      <table class="w-full min-w-[48rem] text-sm">
        <thead
          class="bg-tb-slate-50/60 text-left text-[11px] font-bold uppercase tracking-wider text-tb-slate-500"
        >
          <tr>
            <th scope="col" class="px-4 py-2.5">商家</th>
            <th scope="col" class="px-4 py-2.5">期間</th>
            <th scope="col" class="px-4 py-2.5 text-right">訂單數</th>
            <th scope="col" class="px-4 py-2.5 text-right">份數</th>
            <th scope="col" class="px-4 py-2.5 text-right">應付金額</th>
            <th scope="col" class="px-4 py-2.5">狀態</th>
            <th scope="col" class="px-4 py-2.5"></th>
          </tr>
        </thead>
        <tbody class="divide-y divide-tb-slate-100">
          {#each data.settlements as s (s.id)}
            <tr class="hover:bg-tb-slate-50/60">
              <td class="px-4 py-3 font-jetbrains-mono text-xs text-tb-slate-700">
                <span title={s.vendor_id}>{s.vendor_id.slice(0, 8)}</span>
              </td>
              <td class="px-4 py-3 font-jetbrains-mono text-xs text-tb-slate-500">
                {s.period_start} — {s.period_end}
              </td>
              <td class="px-4 py-3 text-right font-jetbrains-mono tabular-nums">
                {s.order_count}
              </td>
              <td class="px-4 py-3 text-right font-jetbrains-mono tabular-nums">
                {s.portion_count}
              </td>
              <td class="px-4 py-3 text-right font-jetbrains-mono font-bold tabular-nums">
                {ntd(Number(s.gross_minor))}
              </td>
              <td class="px-4 py-3">
                <StateTag tone={statusTone[s.status] ?? "neutral"}>
                  {statusLabel[s.status] ?? s.status}
                </StateTag>
              </td>
              <td class="px-4 py-3 text-right">
                {#if s.status === "closed"}
                  <!-- Hidden form — submitted programmatically after confirmation -->
                  <form
                    method="POST"
                    action="?/voidSettlement"
                    use:enhance={() => {
                      submittingVoid = true;
                      return async ({ update }) => {
                        await update();
                        submittingVoid = false;
                        confirmVoidTarget = null;
                      };
                    }}
                    bind:this={voidFormEl}
                  >
                    <input type="hidden" name="id" value={s.id} />
                    <button
                      type="button"
                      class="text-xs font-semibold text-tb-rose-600 hover:text-tb-rose-700"
                      onclick={() => (confirmVoidTarget = { id: s.id, vendorId: s.vendor_id })}
                    >
                      作廢
                    </button>
                  </form>
                {:else}
                  <span class="text-xs text-tb-slate-500">—</span>
                {/if}
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  {/if}
</Card>

<!-- Confirm void modal -->
<Modal
  open={confirmVoidTarget !== null}
  onClose={() => (confirmVoidTarget = null)}
  title="確認作廢結算單"
>
  <p class="text-sm text-tb-slate-700">
    作廢後此結算單將標記為「已作廢」，金額不會入帳。此操作<strong>不可復原</strong
    >，如需重新關帳請先確認期間正確。
  </p>
  {#if confirmVoidTarget}
    <p class="mt-2 font-jetbrains-mono text-xs text-tb-slate-500">
      商家 ID：{confirmVoidTarget.vendorId}
    </p>
  {/if}
  {#snippet footer()}
    <Button variant="secondary" size="md" onclick={() => (confirmVoidTarget = null)}>取消</Button>
    <Button
      variant="danger"
      size="md"
      disabled={submittingVoid}
      onclick={() => {
        voidFormEl?.requestSubmit();
      }}
    >
      {submittingVoid ? "處理中…" : "確認作廢"}
    </Button>
  {/snippet}
</Modal>
