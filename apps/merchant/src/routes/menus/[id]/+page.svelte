<script lang="ts">
  import { PageHeader, Card, Button, StateTag } from "@tbite/ui";
  import ImageUploader from "$lib/components/ImageUploader.svelte";
  let { data, form } = $props();
  const item = $derived(data.item);

  let images = $state<string[]>([...((data.item.images as string[] | null) ?? [])]);

  const statusMeta = {
    active: { tone: "success", label: "上架中" },
    draft: { tone: "neutral", label: "草稿" },
    archived: { tone: "warning", label: "已封存" },
  } as Record<string, { tone: "success" | "neutral" | "warning"; label: string }>;
  const meta = $derived(statusMeta[item.status] ?? { tone: "neutral", label: item.status });

  const fieldClass =
    "mt-1 w-full rounded-lg border border-tb-slate-300 px-3 py-2 text-sm focus:border-tb-red-500 focus:outline-none focus:ring-4 focus:ring-tb-red-100";
</script>

<div class="max-w-xl">
  <PageHeader eyebrow="Menu Library · 菜單管理" title="編輯餐點">
    {#snippet actions()}
      <StateTag tone={meta.tone}>{meta.label}</StateTag>
    {/snippet}
  </PageHeader>

  <Card>
    <form method="POST" action="?/update" class="space-y-3">
      <label class="block text-sm font-semibold text-tb-slate-800">
        名稱
        <input name="name" required value={item.name} class={fieldClass} />
      </label>
      <label class="block text-sm font-semibold text-tb-slate-800">
        敘述
        <textarea name="description" rows="2" class={fieldClass}>{item.description}</textarea>
      </label>
      <label class="block text-sm font-semibold text-tb-slate-800">
        價格（NTD）
        <input
          name="price"
          type="number"
          min="0"
          required
          value={item.price_minor}
          class="{fieldClass} font-jetbrains-mono tabular-nums"
        />
      </label>
      <label class="block text-sm font-semibold text-tb-slate-800">
        標籤（逗號分隔）
        <input name="tags" value={(item.tags ?? []).join(",")} class={fieldClass} />
      </label>
      <label class="block text-sm font-semibold text-tb-slate-800">
        徽章（逗號分隔）
        <input name="badges" value={(item.badges ?? []).join(",")} class={fieldClass} />
      </label>
      <div class="block text-sm font-semibold text-tb-slate-800">
        餐點圖片
        <div class="mt-1.5">
          <input type="hidden" name="images" value={JSON.stringify(images)} />
          <ImageUploader bind:images />
        </div>
      </div>
      {#if form?.error}
        <p class="rounded-lg bg-tb-rose-50 px-3 py-2 text-sm text-tb-rose-700">
          {form.error}
        </p>
      {/if}
      <div class="pt-1">
        <Button variant="primary" type="submit">儲存</Button>
      </div>
    </form>
  </Card>

  <div class="mt-4 flex flex-wrap gap-2">
    {#if item.status !== "active"}
      <form method="POST" action="?/publish">
        <Button variant="primary" type="submit">上架</Button>
      </form>
    {/if}
    {#if item.status !== "archived"}
      <form method="POST" action="?/archive">
        <Button variant="danger" type="submit">封存</Button>
      </form>
    {/if}
    <a href="/menus"><Button variant="secondary">返回</Button></a>
  </div>
</div>
