<script lang="ts">
  import { PageHeader, Card, Button } from "@tbite/ui";
  import ImageUploader from "$lib/components/ImageUploader.svelte";
  let { form } = $props();

  let images = $state<string[]>([]);

  const fieldClass =
    "mt-1 w-full rounded-lg border border-tb-slate-300 px-3 py-2 text-sm focus:border-tb-red-500 focus:outline-none focus:ring-4 focus:ring-tb-red-100";
</script>

<div class="max-w-xl">
  <PageHeader eyebrow="Menu Library · 菜單管理" title="新增餐點" />

  <Card>
    <form method="POST" class="space-y-3">
      <label class="block text-sm font-semibold text-tb-slate-800">
        名稱
        <input name="name" required class={fieldClass} />
      </label>
      <label class="block text-sm font-semibold text-tb-slate-800">
        敘述
        <textarea name="description" rows="2" class={fieldClass}></textarea>
      </label>
      <label class="block text-sm font-semibold text-tb-slate-800">
        價格（NTD）
        <input
          name="price"
          type="number"
          min="0"
          required
          class="{fieldClass} font-jetbrains-mono tabular-nums"
        />
      </label>
      <label class="block text-sm font-semibold text-tb-slate-800">
        標籤（逗號分隔）
        <input name="tags" placeholder="hot, healthy" class={fieldClass} />
      </label>
      <label class="block text-sm font-semibold text-tb-slate-800">
        徽章（逗號分隔）
        <input name="badges" placeholder="可薪資代扣, 低於 500 kcal" class={fieldClass} />
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

      <div class="flex gap-2 pt-1">
        <Button variant="primary" type="submit">建立</Button>
        <a href="/menus"><Button variant="secondary">取消</Button></a>
      </div>
    </form>
  </Card>
</div>
