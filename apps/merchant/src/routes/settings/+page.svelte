<script lang="ts">
  import { PageHeader, Card, Button, Modal } from "@tbite/ui";

  let { data, form } = $props();

  const selectedCodes = $derived(new Set<string>(data.myPlantCodes));
  let confirmOpen = $state(false);
  let confirmed = false;
  let plantsFormEl: HTMLFormElement;

  function onPlantsSubmit(e: SubmitEvent) {
    const anySelected = plantsFormEl.querySelector('input[name="plants"]:checked') !== null;
    if (!anySelected && !confirmed) {
      e.preventDefault();
      confirmOpen = true;
    }
  }

  function confirmClear() {
    confirmed = true;
    confirmOpen = false;
    plantsFormEl.requestSubmit();
  }
</script>

<PageHeader
  eyebrow="Settings · 營運設定"
  title="營運設定"
  subtitle="設定截單時間、預購開放天數及服務廠區。"
/>

<div class="max-w-xl space-y-6">
  <Card>
    {#if form?.error}
      <p class="mb-3 rounded-tb-xl bg-tb-rose-50 px-3 py-2 text-sm text-tb-rose-700">
        {form.error}
      </p>
    {/if}
    {#if form?.settingsOk}
      <p class="mb-3 rounded-tb-xl bg-tb-emerald-50 px-3 py-2 text-sm text-tb-emerald-700">
        設定已儲存。
      </p>
    {/if}
    <form method="POST" action="?/save" class="space-y-4">
      <label class="flex flex-col gap-1.5 text-sm">
        <span class="text-[11px] font-bold uppercase tracking-eyebrow text-tb-slate-500">
          截單時間（取餐日前一天的整點，0–23）
        </span>
        <input
          type="number"
          name="cutoff_hour"
          min="0"
          max="23"
          required
          value={data.settings.cutoff_hour}
          class="w-32 rounded-tb-lg border border-tb-slate-300 px-3 py-2 text-sm transition focus:border-tb-red-500 focus:outline-none focus:ring-4 focus:ring-tb-red-100"
        />
        <span class="text-xs text-tb-slate-500"> 例如填 17，表示取餐日前一天 17:00 截單。 </span>
      </label>
      <label class="flex flex-col gap-1.5 text-sm">
        <span class="text-[11px] font-bold uppercase tracking-eyebrow text-tb-slate-500">
          預購開放天數（1–30）
        </span>
        <input
          type="number"
          name="preorder_window_days"
          min="1"
          max="30"
          required
          value={data.settings.preorder_window_days}
          class="w-32 rounded-tb-lg border border-tb-slate-300 px-3 py-2 text-sm transition focus:border-tb-red-500 focus:outline-none focus:ring-4 focus:ring-tb-red-100"
        />
        <span class="text-xs text-tb-slate-500"> 員工最多可提前幾天向本商家預訂。 </span>
      </label>
      <Button variant="primary" size="md" type="submit">儲存設定</Button>
    </form>
  </Card>

  <Card>
    {#if form?.error}
      <p class="mb-3 rounded-tb-xl bg-tb-rose-50 px-3 py-2 text-sm text-tb-rose-700">
        {form.error}
      </p>
    {/if}
    {#if form?.plantsOk}
      <p class="mb-3 rounded-tb-xl bg-tb-emerald-50 px-3 py-2 text-sm text-tb-emerald-700">
        服務廠區已更新。
      </p>
    {/if}
    <form method="POST" action="?/savePlants" class="space-y-4" bind:this={plantsFormEl} onsubmit={onPlantsSubmit}>
      <fieldset>
        <legend class="text-[11px] font-bold uppercase tracking-eyebrow text-tb-slate-500">
          服務廠區（複選）
        </legend>
        <div class="mt-3 grid grid-cols-1 gap-2 sm:grid-cols-2">
          {#each data.allPlants as p (p.code)}
            <label
              class="flex cursor-pointer items-start gap-3 rounded-lg border border-tb-slate-200 px-4 py-3 transition hover:border-tb-slate-400 has-[:checked]:border-tb-red-400 has-[:checked]:bg-tb-red-50"
            >
              <input
                type="checkbox"
                name="plants"
                value={p.code}
                checked={selectedCodes.has(p.code)}
                class="mt-0.5 h-4 w-4 accent-tb-red-600"
              />
              <div>
                <div class="font-semibold text-tb-slate-900">{p.label}</div>
                {#if p.address}
                  <div class="text-xs text-tb-slate-500">{p.address}</div>
                {/if}
                <div class="font-jetbrains-mono text-[10px] text-tb-slate-400">{p.code}</div>
              </div>
            </label>
          {:else}
            <p class="col-span-2 text-sm text-tb-slate-500">尚無可選廠區，請聯絡福委會建立廠區。</p>
          {/each}
        </div>
      </fieldset>
      <Button variant="primary" size="md" type="submit">儲存服務廠區</Button>
    </form>
  </Card>
</div>

<Modal open={confirmOpen} onClose={() => (confirmOpen = false)} title="暫停所有供餐服務？">
  <p class="text-sm text-tb-slate-600">
    您尚未選擇任何廠區。儲存後將清空本商家的服務廠區，暫停所有供餐服務。
  </p>
  {#snippet footer()}
    <Button variant="secondary" size="md" onclick={() => (confirmOpen = false)}>取消</Button>
    <Button variant="danger" size="md" onclick={confirmClear}>確認暫停</Button>
  {/snippet}
</Modal>
