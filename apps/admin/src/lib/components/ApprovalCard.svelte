<script lang="ts">
  // Ported from ui_kits/tbite/AdminView.jsx TbApprovalCard.
  // Bound to real VendorDTO data: id / display_name / legal_name / contact_email
  // / status / plants. The reference's per-doc chips have no list source on the
  // dashboard endpoint, so the doc summary links to the vendor documents page.
  import { enhance } from "$app/forms";
  import { Button, StateTag, Icon } from "@tbite/ui";

  interface Props {
    vendor: {
      id: string;
      display_name: string;
      legal_name?: string;
      contact_email: string;
      status: string;
    };
  }
  let { vendor }: Props = $props();

  let submitting = $state(false);
</script>

<div
  class="grid gap-3 rounded-xl border border-tb-slate-200 bg-white p-4 md:grid-cols-[1fr_auto] md:items-center"
>
  <div class="grid gap-2">
    <div class="flex flex-wrap items-center gap-2">
      <h4 class="text-base font-bold text-tb-slate-900">{vendor.display_name}</h4>
      <StateTag tone="pending">待處理</StateTag>
      {#if vendor.legal_name}
        <span class="text-xs text-tb-slate-500">法人 · {vendor.legal_name}</span>
      {/if}
    </div>
    <div class="flex flex-wrap items-center gap-2 text-xs text-tb-slate-500">
      <span class="font-jetbrains-mono">{vendor.contact_email}</span>
      <span aria-hidden="true">·</span>
      <a
        href="/vendors/{vendor.id}/documents"
        class="inline-flex items-center gap-1 font-semibold text-tb-red-600 hover:text-tb-red-700"
      >
        <Icon name="doc" class="h-3.5 w-3.5" />合規文件
      </a>
    </div>
  </div>
  <div class="flex flex-wrap items-center gap-2 md:justify-end">
    <a href="/vendors/{vendor.id}/documents">
      <Button variant="secondary" size="sm">通知補件</Button>
    </a>
    <form
      method="POST"
      action="?/approveVendor"
      use:enhance={() => {
        submitting = true;
        return async ({ update }) => {
          await update();
          submitting = false;
        };
      }}
    >
      <input type="hidden" name="id" value={vendor.id} />
      <Button variant="primary" size="sm" type="submit" disabled={submitting}>
        <Icon name="check" class="h-3.5 w-3.5" />{submitting ? "處理中…" : "核准"}
      </Button>
    </form>
  </div>
</div>
