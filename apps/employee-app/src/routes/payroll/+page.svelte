<script lang="ts">
  // PayrollScreen — current-period hero (live running total) + per-order
  // lines from /api/employee/payroll/current (backend B2). Tapping a line
  // opens the EntryDetailSheet for rating / complaint.
  import { onMount } from "svelte";
  import { getCurrentPayroll, type PayrollLine } from "$lib/api";
  import { money } from "$lib/sample";
  import AppIcon from "$lib/components/AppIcon.svelte";
  import EntryDetailSheet from "$lib/components/EntryDetailSheet.svelte";

  let lines = $state<PayrollLine[]>([]);
  let totalMinor = $state(0);
  let loading = $state(true);
  let error = $state<string | null>(null);
  let selected = $state<PayrollLine | null>(null);

  async function load() {
    loading = true;
    error = null;
    try {
      const res = await getCurrentPayroll();
      lines = res.lines;
      totalMinor = res.total_minor;
    } catch (e) {
      error = e instanceof Error ? e.message : "載入失敗";
      lines = [];
      totalMinor = 0;
    } finally {
      loading = false;
    }
  }

  onMount(load);

  const period = $derived(new Date().toISOString().slice(0, 7).replace("-", " / "));
  const chargedCount = $derived(lines.filter((l) => l.status === "charged").length);

  // After a rating succeeds, mark the matching line locally.
  function markRated(orderId: string) {
    lines = lines.map((l) => (l.order_id === orderId ? { ...l, rated: true } : l));
  }

  function statusLabel(l: PayrollLine): string {
    if (l.status === "reversed") return "已沖銷";
    if (l.status === "no_show") return "未領";
    return l.rated ? "已評分" : "評分 ›";
  }
</script>

<div class="flex h-full flex-col">
  <div class="flex-shrink-0 bg-white px-4 pb-3" style="padding-top: max(env(safe-area-inset-top), 1rem)">
    <h1 class="text-xl font-black text-tb-slate-900">薪資扣款</h1>
    <p class="mt-0.5 text-xs text-tb-slate-500">{period} 進行中</p>
  </div>

  <div class="no-scroll flex-1 overflow-y-auto">
    <!-- Hero -->
    <div
      class="mx-4 mt-3 overflow-hidden rounded-3xl bg-gradient-to-br from-tb-slate-900 via-tb-rose-900 to-tb-red-800 p-5 text-white"
    >
      <div class="text-[11px] font-bold uppercase tracking-[0.18em] text-tb-amber-300">
        本月已扣款
      </div>
      {#if loading}
        <div class="mt-2 h-10 w-32 animate-pulse rounded bg-white/20"></div>
      {:else}
        <div class="mt-1 text-4xl font-black tabular-nums">{money(totalMinor)}</div>
        <div class="mt-1 text-xs text-white/70">
          {chargedCount} 筆訂單 · {period} 進行中
        </div>
      {/if}
    </div>

    <!-- Lines -->
    <div class="px-4 pb-1 pt-4 text-[11px] font-bold uppercase tracking-wider text-tb-slate-500">
      本期明細
    </div>
    <div class="grid gap-2 px-4 pb-6">
      {#if loading}
        {#each [0, 1, 2] as i (i)}
          <div class="h-16 animate-pulse rounded-2xl bg-tb-slate-200"></div>
        {/each}
      {:else if error}
        <div
          class="grid place-items-center rounded-2xl border border-dashed border-tb-slate-300 bg-white py-12 text-center"
        >
          <p class="text-sm text-tb-slate-500">{error}</p>
          <button
            type="button"
            onclick={load}
            class="mt-3 rounded-full bg-tb-red-600 px-4 py-1.5 text-xs font-bold text-white"
          >
            重試
          </button>
        </div>
      {:else if lines.length === 0}
        <div
          class="grid place-items-center rounded-2xl border border-dashed border-tb-slate-300 bg-white py-12 text-center"
        >
          <p class="text-sm text-tb-slate-500">本期還沒有扣款紀錄</p>
        </div>
      {:else}
        {#each lines as line (line.order_id)}
          {@const reversed = line.status === "reversed"}
          {@const charged = line.status === "charged"}
          <button
            type="button"
            onclick={() => (selected = line)}
            class="flex w-full items-center gap-3 rounded-2xl bg-white p-3.5 text-left shadow-sm ring-1 ring-tb-slate-200/70 active:bg-tb-slate-50"
          >
            <div
              class="grid h-10 w-10 flex-shrink-0 place-items-center rounded-full {charged
                ? 'bg-tb-emerald-50'
                : 'bg-tb-rose-50'}"
            >
              {#if charged}
                <AppIcon name="check" class="h-5 w-5 text-tb-emerald-600" />
              {:else}
                <span class="text-xs font-bold text-tb-rose-600">
                  {reversed ? "沖銷" : "未領"}
                </span>
              {/if}
            </div>
            <div class="min-w-0 flex-1">
              <div class="truncate text-xs font-bold text-tb-slate-900">{line.vendor_name}</div>
              <div class="truncate text-[10px] text-tb-slate-500">
                {line.items_summary} · {line.supply_date}
              </div>
            </div>
            <div class="text-right">
              <div
                class="text-sm font-black tabular-nums {reversed
                  ? 'text-tb-slate-400 line-through'
                  : 'text-tb-slate-900'}"
              >
                {reversed ? "-" : ""}{money(line.amount_minor)}
              </div>
              <div class="mt-0.5 text-[10px] text-tb-slate-400">{statusLabel(line)}</div>
            </div>
          </button>
        {/each}
      {/if}
    </div>
  </div>
</div>

<EntryDetailSheet entry={selected} onClose={() => (selected = null)} onRated={markRated} />
