<script lang="ts">
  // In-app notification list (no native push — see design doc "範圍外").
  import { NOTIFICATIONS } from "$lib/sample";
  import BottomSheet from "./BottomSheet.svelte";

  interface Props {
    open: boolean;
    onClose: () => void;
  }
  let { open, onClose }: Props = $props();

  const unread = $derived(NOTIFICATIONS.filter((n) => n.unread).length);
  const glyph: Record<string, string> = { ready: "✓", info: "ℹ", reminder: "⏰" };
  const badge: Record<string, string> = {
    ready: "bg-tb-emerald-500",
    info: "bg-tb-sky-500",
    reminder: "bg-tb-amber-500",
  };
</script>

<BottomSheet {open} {onClose} maxHeight="75%">
  <div class="flex items-center justify-between border-b border-tb-slate-100 px-5 py-3">
    <h2 class="text-lg font-extrabold text-tb-slate-900">通知 · {unread} 則新訊息</h2>
    <button
      type="button"
      class="grid h-8 w-8 place-items-center rounded-full bg-tb-slate-100 text-lg text-tb-slate-600"
      onclick={onClose}
    >
      ✕
    </button>
  </div>
  <div class="no-scroll grid flex-1 gap-2 overflow-y-auto px-4 py-3">
    {#each NOTIFICATIONS as n (n.id)}
      <div
        class="rounded-2xl p-3.5 {n.unread
          ? 'bg-tb-red-50 ring-1 ring-tb-red-100'
          : 'bg-tb-slate-50'}"
      >
        <div class="flex items-start gap-3">
          <div
            class="grid h-9 w-9 flex-shrink-0 place-items-center rounded-full text-sm text-white {badge[
              n.type
            ]}"
          >
            {glyph[n.type]}
          </div>
          <div class="min-w-0 flex-1">
            <div class="flex items-center gap-2">
              <div class="text-sm font-bold text-tb-slate-900">{n.title}</div>
              {#if n.unread}
                <span class="h-2 w-2 rounded-full bg-tb-red-500"></span>
              {/if}
            </div>
            <div class="mt-0.5 text-xs text-tb-slate-600">{n.msg}</div>
            <div class="mt-1 text-[10px] text-tb-slate-400">{n.time}</div>
          </div>
        </div>
      </div>
    {/each}
  </div>
</BottomSheet>
