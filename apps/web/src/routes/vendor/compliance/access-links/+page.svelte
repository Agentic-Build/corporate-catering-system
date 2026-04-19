<script lang="ts">
  import { onMount } from "svelte";

  import { Button, Card, FormField, PageHeader, toasts } from "$lib/components/ui";
  import { apiClient, ensureApiClientConfigured } from "$lib/platform/api";
  import { normalizeApiFailure } from "$lib/platform/api/failure";

  import type { PageData } from "./$types";

  let { data }: { data: PageData } = $props();

  type AccessLink = Awaited<ReturnType<typeof apiClient.vendor.createVendorObjectStorageAccessLink>>;

  let draft = $state({ objectRef: "", locale: "zh-TW" });
  let result = $state<AccessLink | null>(null);
  let submitting = $state(false);

  onMount(() => {
    try {
      ensureApiClientConfigured(data.auth.apiBearerToken);
    } catch (error) {
      toasts.error(normalizeApiFailure(error).localizedMessage);
    }
  });

  async function submit() {
    if (submitting) return;
    const objectRef = draft.objectRef.trim();
    if (!objectRef) {
      toasts.error("請輸入 objectRef。");
      return;
    }
    submitting = true;
    try {
      const response = await apiClient.vendor.createVendorObjectStorageAccessLink({
        objectRef,
        locale: draft.locale.trim() || undefined
      });
      result = response;
      toasts.success("下載連結已產生。");
    } catch (error) {
      toasts.error(normalizeApiFailure(error).localizedMessage);
    } finally {
      submitting = false;
    }
  }
</script>

<PageHeader
  title="建立下載連結"
  description="為既有的 objectRef 產生限時預簽章下載 URL。"
  breadcrumbs={data.breadcrumbs}
/>

<div class="grid gap-4 md:grid-cols-2">
  <Card title="輸入 objectRef">
    <div class="grid gap-3">
      <FormField label="objectRef" required>
        <input class="rounded border border-slate-300 bg-white px-2 py-1.5" placeholder="obj://..." bind:value={draft.objectRef} />
      </FormField>
      <FormField label="Locale" hint="選填">
        <input class="rounded border border-slate-300 bg-white px-2 py-1.5" bind:value={draft.locale} />
      </FormField>
      <div class="flex justify-end">
        <Button variant="primary" onclick={submit} loading={submitting}>產生下載連結</Button>
      </div>
    </div>
  </Card>

  <Card title="結果">
    {#if result}
      <dl class="grid gap-2 text-sm text-slate-700">
        <div>
          <dt class="text-xs text-slate-500">objectRef</dt>
          <dd class="font-mono text-xs break-all">{result.objectRef}</dd>
        </div>
        <div>
          <dt class="text-xs text-slate-500">downloadUrl</dt>
          <dd class="font-mono text-xs break-all">{result.downloadUrl}</dd>
        </div>
        <div>
          <dt class="text-xs text-slate-500">downloadExpiresAtEpochSeconds</dt>
          <dd class="tabular-nums">{result.downloadExpiresAtEpochSeconds}</dd>
        </div>
      </dl>
    {:else}
      <p class="text-sm text-slate-500">尚未建立。送出後會在此顯示下載連結。</p>
    {/if}
  </Card>
</div>
