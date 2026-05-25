<script lang="ts">
  import { PageHeader, Card, Button, Modal } from "@tbite/ui";
  let { data, form } = $props();

  const selectedCodes = $derived(new Set<string>(data.myPlantCodes));
  let confirmOpen = $state(false);
  let confirmed = false;
  let formEl: HTMLFormElement;

  function onsubmit(e: SubmitEvent) {
    const anySelected = formEl.querySelector('input[name="plants"]:checked') !== null;
    if (!anySelected && !confirmed) {
      e.preventDefault();
      confirmOpen = true;
    }
  }

  function confirmClear() {
    confirmed = true;
    confirmOpen = false;
    formEl.requestSubmit();
  }
</script>

<PageHeader
  eyebrow="服務廠區"
  title="服務廠區設定"
  subtitle="選擇本商家提供供餐服務的廠區。福委會仍可在核准時覆蓋此設定。"
/>

{#if form?.error}
  <p class="mb-4 rounded-tb-xl bg-tb-rose-50 px-3 py-2 text-sm text-tb-rose-700">{form.error}</p>
{/if}
{#if form?.ok}
  <p class="mb-4 rounded-tb-xl bg-tb-emerald-50 px-3 py-2 text-sm text-tb-emerald-700">
    服務廠區已更新。
  </p>
{/if}

<div class="max-w-2xl">
  <Card>
    <form method="POST" action="?/save" class="space-y-4" bind:this={formEl} {onsubmit}>
      <fieldset>
        <legend class="text-[11px] font-bold uppercase tracking-eyebrow text-tb-slate-500">
          可服務廠區（複選）
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
