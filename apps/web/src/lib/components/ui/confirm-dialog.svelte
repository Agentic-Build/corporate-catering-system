<script lang="ts">
  import Button from "./button.svelte";

  interface Props {
    open: boolean;
    title: string;
    description?: string;
    confirmLabel?: string;
    cancelLabel?: string;
    tone?: "default" | "danger";
    loading?: boolean;
    onConfirm: () => void;
    onCancel: () => void;
  }

  let {
    open,
    title,
    description,
    confirmLabel = "確認",
    cancelLabel = "取消",
    tone = "default",
    loading = false,
    onConfirm,
    onCancel
  }: Props = $props();
</script>

{#if open}
  <div
    class="fixed inset-0 z-50 flex items-center justify-center bg-slate-900/40 px-4"
    role="dialog"
    aria-modal="true"
  >
    <div class="w-full max-w-md rounded-2xl bg-white p-5 shadow-xl">
      <h3 class="text-lg font-semibold text-slate-900">{title}</h3>
      {#if description}
        <p class="mt-2 text-sm text-slate-600">{description}</p>
      {/if}
      <div class="mt-5 flex justify-end gap-2">
        <Button variant="ghost" onclick={onCancel} disabled={loading}>{cancelLabel}</Button>
        <Button
          variant={tone === "danger" ? "danger" : "primary"}
          onclick={onConfirm}
          loading={loading}
        >
          {confirmLabel}
        </Button>
      </div>
    </div>
  </div>
{/if}
