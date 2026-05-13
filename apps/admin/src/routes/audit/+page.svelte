<script lang="ts">
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
</script>

<section class="space-y-4">
  <header>
    <h1 class="text-2xl font-black text-tb-slate-900">稽核</h1>
    <p class="mt-1 text-sm text-tb-slate-500">append-only 系統稽核紀錄 — 最近的事件在上</p>
  </header>

  <form method="GET" class="grid gap-2 sm:grid-cols-4">
    <label class="flex flex-col gap-1 text-sm">
      <span class="text-xs uppercase tracking-eyebrow text-tb-slate-500">target_kind</span>
      <input type="text" name="target_kind" value={data.target_kind} class="rounded-lg border border-tb-slate-300 px-3 py-1.5" />
    </label>
    <label class="flex flex-col gap-1 text-sm">
      <span class="text-xs uppercase tracking-eyebrow text-tb-slate-500">target_id</span>
      <input type="text" name="target_id" value={data.target_id} class="rounded-lg border border-tb-slate-300 px-3 py-1.5 font-jetbrains-mono" />
    </label>
    <label class="flex flex-col gap-1 text-sm">
      <span class="text-xs uppercase tracking-eyebrow text-tb-slate-500">since (RFC3339)</span>
      <input type="text" name="since" value={data.since} placeholder="2026-05-01T00:00:00Z" class="rounded-lg border border-tb-slate-300 px-3 py-1.5 font-jetbrains-mono" />
    </label>
    <label class="flex flex-col gap-1 text-sm">
      <span class="text-xs uppercase tracking-eyebrow text-tb-slate-500">limit</span>
      <input type="number" name="limit" value={data.limit} min="1" max="1000" class="rounded-lg border border-tb-slate-300 px-3 py-1.5 font-jetbrains-mono" />
    </label>
    <div class="sm:col-span-4">
      <button class="rounded-lg bg-tb-red-600 px-3.5 py-1.5 text-sm font-semibold text-white hover:bg-tb-red-700">查詢</button>
      {#if data.target_kind || data.target_id || data.since}
        <a href="/audit" class="ml-2 text-xs text-tb-slate-500 hover:text-tb-slate-700">清除</a>
      {/if}
    </div>
  </form>

  {#if data.events.length === 0}
    <p class="rounded-tb-2xl border border-tb-slate-200 bg-white p-6 text-center text-sm text-tb-slate-500">
      無稽核紀錄
    </p>
  {:else}
    <ol class="relative space-y-3 border-l-2 border-tb-slate-200 pl-4">
      {#each data.events as e (e.id)}
        <li class="relative rounded-tb-2xl border border-tb-slate-200 bg-white p-3 shadow-tb-sm">
          <span class="absolute -left-[1.4rem] top-4 h-3 w-3 rounded-full bg-tb-slate-300"></span>
          <div class="flex flex-wrap items-center justify-between gap-2">
            <p class="font-semibold text-tb-slate-900 font-jetbrains-mono text-sm">{e.action}</p>
            <span class="font-jetbrains-mono text-xs text-tb-slate-500">{e.at.slice(0, 19).replace("T", " ")}</span>
          </div>
          <p class="mt-1 text-xs text-tb-slate-600">
            <span class="text-tb-slate-500">actor:</span>
            <span class="font-jetbrains-mono">{e.actor_role || "system"}</span>
            {#if e.actor_id}
              <span class="text-tb-slate-400">·</span>
              <span class="font-jetbrains-mono">{e.actor_id.slice(0, 8)}</span>
            {/if}
            <span class="text-tb-slate-400"> · target:</span>
            <span class="font-jetbrains-mono">{e.target_kind}/{(e.target_id ?? "").slice(0, 8)}</span>
          </p>
          {#if e.payload}
            <pre class="mt-2 overflow-x-auto rounded-lg bg-tb-slate-50 p-2 text-xs font-jetbrains-mono text-tb-slate-700">{preview(e.payload)}</pre>
          {/if}
          {#if e.request_id}
            <p class="mt-1 font-jetbrains-mono text-xs text-tb-slate-400">request_id: {e.request_id}</p>
          {/if}
        </li>
      {/each}
    </ol>
  {/if}
</section>
