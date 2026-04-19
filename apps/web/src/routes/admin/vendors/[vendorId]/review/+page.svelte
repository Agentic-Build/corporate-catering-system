<script lang="ts">
  import { goto } from "$app/navigation";
  import { onMount } from "svelte";

  import { PageHeader, Card, Button, FormField, toasts } from "$lib/components/ui";
  import { apiClient } from "$lib/platform/api";
  import {
    configureAdminApi,
    describeApiError,
    VENDOR_REVIEW_DECISION_OPTIONS,
    type VendorReviewDecision
  } from "$lib/admin/api";

  import type { PageData } from "./$types";

  let { data }: { data: PageData } = $props();

  const vendorId = $derived(data.vendorId);

  let decision = $state<VendorReviewDecision>("APPROVED");
  let comment = $state("符合入駐規範並通過福委會審核。");
  let submitting = $state(false);
  let formError = $state<string | null>(null);

  onMount(() => {
    configureAdminApi(data.auth.apiBearerToken);
  });

  async function submit(event: SubmitEvent) {
    event.preventDefault();
    formError = null;

    const trimmed = comment.trim();
    if (trimmed.length < 5) {
      formError = "審核意見至少需 5 個字元。";
      return;
    }

    submitting = true;
    try {
      const updated = await apiClient.admin.reviewVendorApplication(vendorId, {
        decision,
        comment: trimmed
      });
      toasts.success(`商家 ${updated.vendorId} 審核更新為 ${updated.status}。`);
      await goto(`/admin/vendors/${vendorId}`);
    } catch (error) {
      const message = describeApiError(error);
      formError = message;
      toasts.error(message);
    } finally {
      submitting = false;
    }
  }
</script>

<PageHeader
  eyebrow="商家審核"
  title="審核決策"
  description={`對 ${vendorId} 提交 APPROVED / REQUEST_FIX / REJECTED。`}
  breadcrumbs={data.breadcrumbs}
/>

<Card
  title="提交審核決策"
  description="意見需至少 5 字；決策會寫入 append-only 歷程，APPROVED 需先滿足必填文件齊全。"
>
  <form class="grid gap-3" onsubmit={submit}>
    <FormField label="決策" required>
      <select
        class="rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm"
        bind:value={decision}
      >
        {#each VENDOR_REVIEW_DECISION_OPTIONS as option}
          <option value={option}>{option}</option>
        {/each}
      </select>
    </FormField>

    <FormField label="審核意見（≥ 5 字）" required>
      <textarea
        class="min-h-[120px] rounded-lg border border-slate-300 bg-white px-3 py-2 text-sm"
        bind:value={comment}
        placeholder="請說明此次決策的理由"
      ></textarea>
    </FormField>

    {#if formError}
      <p class="rounded-lg border border-rose-200 bg-rose-50 px-3 py-2 text-xs text-rose-900">
        {formError}
      </p>
    {/if}

    <div class="flex gap-2">
      <Button type="submit" variant="primary" loading={submitting}>送出決策</Button>
      <Button variant="ghost" href={`/admin/vendors/${vendorId}`}>取消</Button>
    </div>
  </form>
</Card>
