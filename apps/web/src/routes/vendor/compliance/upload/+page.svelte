<script lang="ts">
  import { onMount } from "svelte";

  import {
    Button,
    Card,
    FileDropzone,
    FormField,
    PageHeader,
    toasts
  } from "$lib/components/ui";
  import { apiClient, ensureApiClientConfigured } from "$lib/platform/api";
  import { normalizeApiFailure } from "$lib/platform/api/failure";
  import { friendlyArtifactClass } from "$lib/platform/labels";
  import type { StorageArtifactClass } from "../../../../../../../contract/generated/ts-client";

  import type { PageData } from "./$types";

  let { data }: { data: PageData } = $props();

  type UploadableClass = "COMPLIANCE_DOCUMENT" | "MENU_IMAGE";

  const TAB_CLASSES: readonly UploadableClass[] = ["COMPLIANCE_DOCUMENT", "MENU_IMAGE"];

  let artifactClass = $state<UploadableClass>("COMPLIANCE_DOCUMENT");
  let locale = $state("zh-TW");
  let lastObjectRef = $state<string | null>(null);
  let lastFileName = $state<string | null>(null);
  let copied = $state(false);
  // Force FileDropzone to re-mount when tab changes so upload state resets.
  let dropzoneKey = $state(0);

  onMount(() => {
    try {
      ensureApiClientConfigured(data.auth.apiBearerToken);
    } catch (error) {
      toasts.error(normalizeApiFailure(error).localizedMessage);
    }
  });

  const accept = $derived(
    artifactClass === "MENU_IMAGE" ? "image/*" : ".pdf,.jpg,.jpeg,.png"
  );
  const maxSizeBytes = $derived(
    artifactClass === "MENU_IMAGE" ? 10 * 1024 * 1024 : 20 * 1024 * 1024
  );
  const hint = $derived(
    artifactClass === "MENU_IMAGE"
      ? "菜單圖片 — 支援 JPG / PNG / WEBP，單檔最大 10 MB"
      : "合規文件 — 支援 PDF / JPG / PNG，單檔最大 20 MB"
  );

  async function uploadPlan(file: File): Promise<{ objectRef: string }> {
    const response = await apiClient.vendor.createVendorObjectStorageUploadPlan({
      artifactClass,
      fileName: file.name,
      mimeType: file.type || "application/octet-stream",
      sizeBytes: file.size,
      locale: locale.trim() || undefined
    });

    const { uploadUrl, requiredHeaders, objectRef } = response.primary;
    const putResponse = await fetch(uploadUrl, {
      method: "PUT",
      headers: requiredHeaders,
      body: file
    });
    if (!putResponse.ok) {
      throw new Error(`上傳失敗（HTTP ${putResponse.status}）`);
    }
    return { objectRef };
  }

  function onUploaded(objectRef: string, file: File) {
    lastObjectRef = objectRef;
    lastFileName = file.name;
    copied = false;
    toasts.success(`已上傳 ${file.name}`);
  }

  function switchTab(next: UploadableClass) {
    if (artifactClass === next) return;
    artifactClass = next;
    dropzoneKey += 1;
  }

  async function copyRef() {
    if (!lastObjectRef) return;
    try {
      await navigator.clipboard.writeText(lastObjectRef);
      copied = true;
      toasts.success("已複製 objectRef");
    } catch {
      toasts.error("瀏覽器不支援自動複製，請手動選取。");
    }
  }
</script>

<PageHeader
  title="上傳合規文件 / 菜單圖片"
  description="直接拖檔進來即可上傳，無需手動填寫檔名或 MIME。"
  breadcrumbs={data.breadcrumbs}
/>

<Card>
  <div class="flex flex-wrap gap-2">
    {#each TAB_CLASSES as cls}
      <button
        type="button"
        class={`rounded-full border px-3 py-1.5 text-sm font-semibold transition ${
          artifactClass === cls
            ? "border-cyan-700 bg-cyan-700 text-white"
            : "border-slate-300 bg-white text-slate-700 hover:border-cyan-600 hover:text-cyan-800"
        }`}
        onclick={() => switchTab(cls)}
      >
        {friendlyArtifactClass(cls)}
      </button>
    {/each}
  </div>

  <FormField label="語系 locale" hint="選填，用於文件國際化對應">
    <input
      class="rounded border border-slate-300 bg-white px-2 py-1.5"
      placeholder="zh-TW"
      bind:value={locale}
    />
  </FormField>

  {#key dropzoneKey}
    <FileDropzone
      plan={uploadPlan}
      {accept}
      {maxSizeBytes}
      {hint}
      label={`拖檔或點擊上傳 ${friendlyArtifactClass(artifactClass)}`}
      onuploaded={onUploaded}
    />
  {/key}
</Card>

{#if lastObjectRef}
  <Card title="上傳完成" variant="success">
    <p class="text-sm text-emerald-900">
      檔案 <span class="font-semibold">{lastFileName}</span> 已上傳，可用下列 objectRef 貼到菜單或寄給合規對口：
    </p>
    <div class="flex flex-wrap items-center gap-2">
      <code class="inline-flex max-w-full flex-1 items-center break-all rounded bg-white px-2 py-1.5 font-mono text-xs text-slate-800 ring-1 ring-emerald-300">
        {lastObjectRef}
      </code>
      <Button size="sm" variant="primary" onclick={copyRef}>
        {copied ? "已複製 ✓" : "複製 objectRef"}
      </Button>
    </div>
  </Card>
{/if}
