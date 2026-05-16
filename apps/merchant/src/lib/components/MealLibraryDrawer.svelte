<script lang="ts">
  // Meal-library drawer — ported from MerchantView.jsx MealLibraryDrawer.
  // The reference's kcal field has no API source, so it is omitted.
  import { Drawer, Button, Icon, SearchInput } from "@tbite/ui";

  interface Props {
    open: boolean;
    onClose: () => void;
    /** Full menu-item library (incl. archived). */
    library: any[];
    /** Item ids already scheduled on the selected day. */
    scheduledIds: Set<string>;
    /** Adds an item to the selected day (publishes first if archived). */
    onAdd: (item: any) => void;
  }
  let { open, onClose, library, scheduledIds, onAdd }: Props = $props();

  let query = $state("");
  const filtered = $derived(
    library.filter(
      (m: any) =>
        !query || m.name?.includes(query) || (m.tags ?? []).some((t: string) => t.includes(query)),
    ),
  );
</script>

<Drawer {open} {onClose} maxWidth="max-w-lg">
  {#snippet header()}
    <div class="flex items-center justify-between gap-3">
      <div>
        <div class="text-[11px] font-bold uppercase tracking-eyebrow-wide text-tb-red-600">
          餐點庫 · Meal Library
        </div>
        <h2 class="mt-0.5 text-lg font-extrabold text-tb-slate-900">從歷史菜色重新上架</h2>
      </div>
      <button
        type="button"
        onclick={onClose}
        class="rounded-lg p-2 text-tb-slate-500 hover:bg-tb-slate-100"
        aria-label="關閉"
      >
        <Icon name="close" class="h-5 w-5" />
      </button>
    </div>
    <p class="mt-2 text-xs text-tb-slate-500">
      所有曾經上架過的菜色都會保留照片、名稱、描述與價格 — 重新上架時只需設定當日上限。
    </p>
    <div class="mt-3">
      <SearchInput value={query} onInput={(v) => (query = v)} placeholder="搜尋餐點名稱或標籤…" />
    </div>
  {/snippet}

  <ul class="grid gap-3">
    {#each filtered as m (m.id)}
      {@const already = scheduledIds.has(m.id)}
      <li
        class="flex items-stretch gap-3 rounded-tb-2xl border bg-white p-3 {already
          ? 'border-tb-slate-100 opacity-60'
          : 'border-tb-slate-200 hover:border-tb-slate-300 hover:shadow-tb-sm'}"
      >
        {#if m.images?.[0]}
          <img src={m.images[0]} alt="" class="h-24 w-24 flex-shrink-0 rounded-xl object-cover" />
        {:else}
          <div
            class="grid h-24 w-24 flex-shrink-0 place-items-center rounded-xl bg-tb-slate-100 text-tb-slate-400"
          >
            <Icon name="doc" class="h-7 w-7" />
          </div>
        {/if}
        <div class="flex min-w-0 flex-1 flex-col">
          <div class="flex items-start justify-between gap-2">
            <div class="min-w-0">
              <div class="truncate text-sm font-extrabold text-tb-slate-900">
                {m.name}
              </div>
              <div class="text-[11px] text-tb-slate-500">{m.description}</div>
            </div>
            <div class="text-right">
              <div class="font-jetbrains-mono text-base font-black tabular-nums text-tb-slate-900">
                ${m.price_minor?.toLocaleString() ?? 0}
              </div>
            </div>
          </div>
          <div class="mt-1 flex flex-wrap items-center gap-1.5">
            {#each m.tags ?? [] as t (t)}
              <span
                class="rounded-full bg-tb-slate-100 px-2 py-0.5 text-[10px] font-bold text-tb-slate-700"
              >
                {t}
              </span>
            {/each}
          </div>
          <div class="mt-auto flex items-center justify-between gap-2 pt-2">
            <div class="text-[11px] text-tb-slate-500">
              上次上架 ·
              <b class="text-tb-slate-800">{m.last_used ?? "尚未上架"}</b>
              {#if m.total_sold > 0}
                <span class="ml-2">
                  累計售出
                  <b class="font-jetbrains-mono tabular-nums text-tb-slate-800">
                    {m.total_sold.toLocaleString()}
                  </b>
                </span>
              {/if}
            </div>
            {#if already}
              <span
                class="inline-flex items-center gap-1 text-[11px] font-bold text-tb-emerald-700"
              >
                <Icon name="check" class="h-3.5 w-3.5" />已排入此日
              </span>
            {:else}
              <Button variant="primary" size="sm" onclick={() => onAdd(m)}>
                <Icon name="plus" class="h-3.5 w-3.5" />加入此日
              </Button>
            {/if}
          </div>
        </div>
      </li>
    {/each}
    {#if filtered.length === 0}
      <li class="py-10 text-center text-sm text-tb-slate-500">沒有符合的餐點</li>
    {/if}
  </ul>

  {#snippet footer()}
    <a href="/menus/new" class="block">
      <Button variant="secondary" fullWidth>
        <Icon name="plus" class="h-4 w-4" />建立全新菜色
      </Button>
    </a>
  {/snippet}
</Drawer>
