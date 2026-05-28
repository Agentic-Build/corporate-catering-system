<script lang="ts">
  import { untrack } from "svelte";
  import { PageHeader, Card, Button } from "@tbite/ui";
  let { data } = $props();

  function preview(p: Record<string, unknown> | null | undefined): string {
    if (!p) return "{}";
    try {
      const s = JSON.stringify(p);
      return s.length > 220 ? s.slice(0, 220) + "…" : s;
    } catch {
      return "{}";
    }
  }

  // Convert datetime-local value (YYYY-MM-DDTHH:MM) to RFC3339 for the server.
  // We append ":00Z" — treating the input as UTC, which is consistent with server-side filtering.
  function toRfc3339(localVal: string): string {
    if (!localVal) return "";
    // datetime-local gives "YYYY-MM-DDTHH:MM" (no seconds, no TZ)
    return localVal.length === 16 ? localVal + ":00Z" : localVal;
  }

  // Initialize the datetime-local input from the existing since value.
  // Strip trailing "Z" / seconds so the input shows "YYYY-MM-DDTHH:MM".
  function fromRfc3339(since: string): string {
    if (!since) return "";
    // e.g. "2026-05-01T00:00:00Z" → "2026-05-01T00:00"
    return since.slice(0, 16);
  }

  // sinceLocal is controlled by the user; initialised from URL param on mount only.
  let sinceLocal = $state(untrack(() => fromRfc3339(data.since)));
  let sinceHidden = $derived(toRfc3339(sinceLocal));
</script>

<PageHeader eyebrow="合規" title="稽核紀錄" subtitle="append-only 系統稽核日誌 · 最近的事件在上" />

<Card title="篩選">
  <form method="GET" class="grid gap-3 sm:grid-cols-4">
    <label class="flex flex-col gap-1 text-sm">
      <span class="text-[11px] font-bold uppercase tracking-eyebrow text-tb-slate-500">
        目標類型
      </span>
      <input
        type="text"
        name="target_kind"
        value={data.target_kind}
        placeholder="如 vendor"
        class="rounded-lg border border-tb-slate-300 px-3 py-1.5 focus:border-tb-slate-500 focus:outline-none focus:ring-2 focus:ring-tb-slate-300"
      />
    </label>
    <label class="flex flex-col gap-1 text-sm">
      <span class="text-[11px] font-bold uppercase tracking-eyebrow text-tb-slate-500">
        目標 ID
      </span>
      <input
        type="text"
        name="target_id"
        value={data.target_id}
        class="rounded-lg border border-tb-slate-300 px-3 py-1.5 font-jetbrains-mono focus:border-tb-slate-500 focus:outline-none focus:ring-2 focus:ring-tb-slate-300"
      />
    </label>
    <label class="flex flex-col gap-1 text-sm">
      <span class="text-[11px] font-bold uppercase tracking-eyebrow text-tb-slate-500">
        起始時間
      </span>
      <!-- datetime-local for usability; hidden field converts to RFC3339 for server -->
      <input
        type="datetime-local"
        bind:value={sinceLocal}
        class="rounded-lg border border-tb-slate-300 px-3 py-1.5 font-jetbrains-mono focus:border-tb-slate-500 focus:outline-none focus:ring-2 focus:ring-tb-slate-300"
      />
      <input type="hidden" name="since" value={sinceHidden} />
    </label>
    <label class="flex flex-col gap-1 text-sm">
      <span class="text-[11px] font-bold uppercase tracking-eyebrow text-tb-slate-500">
        最多筆數
      </span>
      <input
        type="number"
        name="limit"
        value={data.limit}
        min="1"
        max="1000"
        class="rounded-lg border border-tb-slate-300 px-3 py-1.5 font-jetbrains-mono focus:border-tb-slate-500 focus:outline-none focus:ring-2 focus:ring-tb-slate-300"
      />
    </label>
    <div class="flex items-center gap-2 sm:col-span-4">
      <Button variant="primary" size="md" type="submit">查詢</Button>
      {#if data.target_kind || data.target_id || data.since}
        <a href="/audit" class="text-xs text-tb-slate-500 hover:text-tb-slate-700">清除篩選</a>
      {/if}
    </div>
  </form>
</Card>

{#if data.events.length === 0}
  <p
    class="rounded-tb-2xl border border-dashed border-tb-slate-300 bg-tb-slate-50/60 p-8 text-center text-sm text-tb-slate-500"
  >
    無符合條件的稽核紀錄
  </p>
{:else}
  <ol class="relative grid gap-3 border-l-2 border-tb-slate-200 pl-4">
    {#each data.events as e (e.id)}
      <li
        class="relative min-w-0 rounded-tb-2xl border border-tb-slate-200 bg-white p-4 shadow-tb-sm"
      >
        <span
          class="absolute -left-[1.4rem] top-5 h-3 w-3 rounded-full bg-tb-slate-300 ring-2 ring-white"
        ></span>
        <div class="flex flex-wrap items-center justify-between gap-2">
          <p class="font-jetbrains-mono text-sm font-semibold text-tb-slate-900">{e.action}</p>
          <span class="font-jetbrains-mono text-xs text-tb-slate-500">
            {e.at.slice(0, 19).replace("T", " ")}
          </span>
        </div>
        <p class="mt-1 text-xs text-tb-slate-600">
          <span class="text-tb-slate-500">actor ·</span>
          <span class="font-jetbrains-mono">{e.actor_role || "system"}</span>
          {#if e.actor_id}
            <span class="text-tb-slate-500"> · </span>
            <span class="font-jetbrains-mono">{e.actor_id.slice(0, 8)}</span>
          {/if}
          <span class="text-tb-slate-500"> · target · </span>
          <span class="font-jetbrains-mono">{e.target_kind}/{(e.target_id ?? "").slice(0, 8)}</span>
        </p>
        {#if e.payload}
          <pre
            class="mt-2 min-w-0 overflow-x-auto whitespace-pre-wrap break-all rounded-lg bg-tb-slate-50 p-2 font-jetbrains-mono text-xs text-tb-slate-700">{preview(
              e.payload,
            )}</pre>
        {/if}
        {#if e.request_id}
          <p class="mt-1 font-jetbrains-mono text-xs text-tb-slate-500">
            request_id · {e.request_id}
          </p>
        {/if}
      </li>
    {/each}
  </ol>
{/if}
