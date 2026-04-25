<script lang="ts">
  import { onDestroy, onMount } from "svelte";

  interface Props {
    deadlineEpochMs: number | null;
    expiredLabel?: string;
    prefix?: string;
  }

  let { deadlineEpochMs, expiredLabel = "已截止", prefix = "剩" }: Props = $props();

  let nowMs = $state(Date.now());
  let timer: ReturnType<typeof setInterval> | null = null;

  onMount(() => {
    nowMs = Date.now();
    timer = setInterval(() => {
      nowMs = Date.now();
    }, 1000);
  });

  onDestroy(() => {
    if (timer) clearInterval(timer);
  });

  const remainingMs = $derived(deadlineEpochMs === null ? null : deadlineEpochMs - nowMs);

  const label = $derived.by(() => {
    if (remainingMs === null) return "";
    if (remainingMs <= 0) return expiredLabel;
    const totalSeconds = Math.floor(remainingMs / 1000);
    const hours = Math.floor(totalSeconds / 3600);
    const minutes = Math.floor((totalSeconds % 3600) / 60);
    const seconds = totalSeconds % 60;
    if (hours > 0) return `${prefix} ${hours}h ${minutes}m`;
    if (minutes > 0) return `${prefix} ${minutes}m ${seconds.toString().padStart(2, "0")}s`;
    return `${prefix} ${seconds}s`;
  });

  const tone = $derived.by(() => {
    if (remainingMs === null || remainingMs <= 0) return "bg-slate-200 text-slate-600";
    if (remainingMs < 60 * 60 * 1000) return "bg-rose-100 text-rose-900";
    if (remainingMs < 3 * 60 * 60 * 1000) return "bg-amber-100 text-amber-900";
    return "bg-emerald-100 text-emerald-900";
  });
</script>

{#if label}
  <span class={`inline-flex items-center rounded-full px-2 py-0.5 text-xs font-semibold tabular-nums ${tone}`}>
    {label}
  </span>
{/if}
