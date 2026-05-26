<script lang="ts">
  import { PageHeader, Card, StateTag, Button, Icon, EmptyState } from "@tbite/ui";
  let { data, form } = $props();

  const categoryLabel = {
    wrong_item: "送錯餐點",
    missing_item: "餐點短缺",
    quality: "餐點品質",
    portion: "份量不足",
    hygiene: "衛生問題",
    other: "其他",
  } as Record<string, string>;

  /** Format an ISO timestamp as "YYYY-MM-DD HH:MM"; "—" when absent. */
  function ts(s: string | null | undefined): string {
    return s ? s.slice(0, 16).replace("T", " ") : "—";
  }
</script>

<PageHeader
  eyebrow="客訴治理"
  title="已升級客訴處理"
  subtitle="員工升級至福委會的客訴 · 結案後寫入稽核日誌且不可復原"
/>

{#if form?.error}
  <p class="rounded-lg bg-tb-rose-50 px-3 py-2 text-sm text-tb-rose-700">{form.error}</p>
{/if}

<div class="mt-4">
  {#if data.complaints.length === 0}
    <EmptyState icon="check" title="目前沒有待處理的升級客訴" hint="員工升級客訴後會出現在此處" />
  {:else}
    <div class="grid gap-3">
      {#each data.complaints as c (c.id)}
        <Card>
          <div class="flex flex-wrap items-center justify-between gap-2">
            <div class="flex items-center gap-2">
              <span class="font-jetbrains-mono text-xs text-tb-slate-500">#{c.id.slice(0, 8)}</span>
              <StateTag tone="warning" pulse>已升級</StateTag>
              <StateTag tone="neutral">{categoryLabel[c.category] ?? c.category}</StateTag>
            </div>
            <span class="font-jetbrains-mono text-xs text-tb-slate-500">
              提報於 {ts(c.created_at)}
            </span>
          </div>

          <p class="mt-2 font-jetbrains-mono text-xs text-tb-slate-500">
            訂單 {c.order_id.slice(0, 8)} · 商家 {c.vendor_id.slice(0, 8)} · 員工 {c.user_id.slice(
              0,
              8,
            )}
          </p>

          <!-- Complaint history timeline -->
          <div class="mt-3 grid gap-2 border-t border-tb-slate-100 pt-3">
            <div class="rounded-xl bg-tb-slate-50 p-3">
              <p class="text-[11px] font-bold uppercase tracking-eyebrow text-tb-slate-500">
                員工描述
              </p>
              <p class="mt-1 whitespace-pre-wrap text-sm text-tb-slate-900">{c.description}</p>
            </div>

            {#if c.vendor_response}
              <div class="rounded-xl border border-tb-red-200 bg-tb-red-50/60 p-3">
                <p class="text-[11px] font-bold uppercase tracking-eyebrow text-tb-red-700">
                  商家回覆 · {ts(c.vendor_responded_at)}
                </p>
                <p class="mt-1 whitespace-pre-wrap text-sm text-tb-slate-900">
                  {c.vendor_response}
                </p>
              </div>
            {:else}
              <p
                class="rounded-xl border border-dashed border-tb-slate-300 bg-tb-slate-50/60 px-3 py-2 text-xs text-tb-slate-500"
              >
                商家未於回覆期限內回應
              </p>
            {/if}

            <div class="rounded-xl border border-tb-amber-300 bg-tb-amber-50/60 p-3">
              <p class="text-[11px] font-bold uppercase tracking-eyebrow text-tb-amber-700">
                員工升級 · {ts(c.escalated_at)}
              </p>
              <p class="mt-1 text-sm text-tb-slate-700">員工對處理結果不滿意，升級至福委會。</p>
            </div>
          </div>

          <!-- Resolution action -->
          <form
            method="POST"
            action="?/resolve"
            class="mt-3 space-y-2 rounded-xl border border-tb-emerald-200 bg-tb-emerald-50/40 p-3"
          >
            <input type="hidden" name="id" value={c.id} />
            <p class="text-[11px] font-bold uppercase tracking-eyebrow text-tb-emerald-700">
              福委會結案
            </p>
            <label class="flex flex-col gap-1 text-xs text-tb-slate-700">
              結案說明 <span class="text-tb-rose-700">*</span>（至少 5 字）
              <textarea
                name="resolution"
                rows="3"
                required
                minlength="5"
                placeholder="說明處理結果與後續措施"
                class="rounded-lg border border-tb-slate-300 px-2 py-1.5 text-sm text-tb-slate-900 focus:border-tb-slate-500 focus:outline-none focus:ring-2 focus:ring-tb-slate-300"
              ></textarea>
            </label>
            <label class="flex items-center gap-2 text-xs text-tb-slate-700">
              <input
                type="checkbox"
                name="compensate"
                value="true"
                class="h-4 w-4 rounded border-tb-slate-300 text-tb-emerald-600 focus:ring-tb-emerald-300"
              />
              同時啟動薪資沖銷（補償退款）
            </label>
            <Button variant="primary" size="md" type="submit">
              <Icon name="check" class="h-3.5 w-3.5" />結案
            </Button>
          </form>
        </Card>
      {/each}
    </div>
  {/if}
</div>
