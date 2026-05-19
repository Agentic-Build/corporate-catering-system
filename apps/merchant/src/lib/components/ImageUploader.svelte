<script lang="ts">
  // 餐點圖片上傳 — picks image files, POSTs each to the /api/uploads proxy
  // (which forwards to POST /api/merchant/uploads), shows thumbnails, and
  // supports remove + reorder. The resulting URL list is bindable so the
  // edit/create form can submit it as a hidden JSON input.
  import { Icon } from "@tbite/ui";

  interface Props {
    images: string[];
  }
  let { images = $bindable() }: Props = $props();

  let fileInput = $state<HTMLInputElement>();
  let uploading = $state(false);
  let uploadError = $state<string | null>(null);

  async function onPick(e: Event) {
    const input = e.currentTarget as HTMLInputElement;
    const files = [...(input.files ?? [])];
    if (files.length === 0) return;
    uploading = true;
    uploadError = null;
    try {
      for (const file of files) {
        const fd = new FormData();
        fd.set("file", file);
        const r = await fetch("/api/uploads", { method: "POST", body: fd });
        if (!r.ok) {
          uploadError = `「${file.name}」上傳失敗，請確認為 JPEG / PNG / WebP 且不超過 2MB。`;
          continue;
        }
        const data = (await r.json()) as { url: string };
        images = [...images, data.url];
      }
    } finally {
      uploading = false;
      input.value = "";
    }
  }

  function remove(idx: number) {
    images = images.filter((_, i) => i !== idx);
  }

  function move(idx: number, delta: number) {
    const next = idx + delta;
    if (next < 0 || next >= images.length) return;
    const copy = [...images];
    [copy[idx], copy[next]] = [copy[next], copy[idx]];
    images = copy;
  }
</script>

<div class="space-y-3">
  {#if images.length > 0}
    <ul class="grid grid-cols-3 gap-2 sm:grid-cols-4">
      {#each images as url, i (url)}
        <li
          class="group relative aspect-square overflow-hidden rounded-tb-lg border border-tb-slate-200 bg-tb-slate-50"
        >
          <img src={url} alt={`餐點圖片 ${i + 1}`} class="h-full w-full object-cover" />
          {#if i === 0}
            <span
              class="absolute left-1 top-1 rounded-full bg-tb-slate-900/80 px-1.5 py-0.5 text-[9px] font-semibold text-white"
            >
              主圖
            </span>
          {/if}
          <div
            class="absolute inset-x-0 bottom-0 flex items-center justify-between gap-1 bg-tb-slate-900/60 p-1 opacity-0 transition group-hover:opacity-100"
          >
            <button
              type="button"
              onclick={() => move(i, -1)}
              disabled={i === 0}
              aria-label="往前移"
              class="rounded p-0.5 text-white transition hover:bg-white/20 disabled:opacity-30"
            >
              <Icon name="chevron" class="h-3.5 w-3.5 rotate-90" />
            </button>
            <button
              type="button"
              onclick={() => remove(i)}
              aria-label="移除圖片"
              class="rounded p-0.5 text-white transition hover:bg-white/20"
            >
              <Icon name="close" class="h-3.5 w-3.5" />
            </button>
            <button
              type="button"
              onclick={() => move(i, 1)}
              disabled={i === images.length - 1}
              aria-label="往後移"
              class="rounded p-0.5 text-white transition hover:bg-white/20 disabled:opacity-30"
            >
              <Icon name="chevron" class="h-3.5 w-3.5 -rotate-90" />
            </button>
          </div>
        </li>
      {/each}
    </ul>
  {/if}

  <input
    bind:this={fileInput}
    type="file"
    accept="image/jpeg,image/png,image/webp"
    multiple
    onchange={onPick}
    class="hidden"
  />
  <button
    type="button"
    onclick={() => fileInput?.click()}
    disabled={uploading}
    class="inline-flex items-center gap-2 rounded-lg border border-dashed border-tb-slate-300 px-3 py-2 text-sm font-semibold text-tb-slate-700 transition hover:border-tb-slate-500 disabled:opacity-50"
  >
    <Icon name="plus" class="h-4 w-4" />
    {uploading ? "上傳中…" : "新增圖片"}
  </button>

  {#if uploadError}
    <p class="rounded-lg bg-tb-rose-50 px-3 py-2 text-sm text-tb-rose-700">{uploadError}</p>
  {/if}
</div>
