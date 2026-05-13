<script lang="ts">
  let { data, form } = $props();

  function ageMinutes(iso: string): string {
    const t = Date.parse(iso);
    if (Number.isNaN(t)) return "-";
    const mins = Math.floor((Date.now() - t) / 60000);
    if (mins < 60) return `${mins}m`;
    const h = Math.floor(mins / 60);
    if (h < 24) return `${h}h`;
    return `${Math.floor(h / 24)}d`;
  }
</script>

<section class="space-y-4">
  <header>
    <h1 class="text-2xl font-black text-tb-slate-900">死信</h1>
    <p class="mt-1 text-sm text-tb-slate-500">無法處理的事件 — 可重送原 subject 或標記已解決</p>
  </header>

  {#if form?.error}
    <p class="rounded-lg bg-tb-rose-50 px-3 py-2 text-sm text-tb-rose-700">{form.error}</p>
  {/if}

  <form method="GET" class="flex flex-wrap items-center gap-2">
    <input
      type="text"
      name="stream"
      value={data.stream}
      placeholder="篩選 stream"
      class="rounded-lg border border-tb-slate-300 px-3 py-1.5 text-sm"
    />
    <button class="rounded-lg border border-tb-slate-300 px-3 py-1.5 text-sm font-semibold text-tb-slate-800 hover:border-tb-slate-500">篩選</button>
    {#if data.stream}
      <a href="/dlq" class="text-xs text-tb-slate-500 hover:text-tb-slate-700">清除</a>
    {/if}
  </form>

  {#if data.messages.length === 0}
    <p class="rounded-tb-2xl border border-tb-slate-200 bg-white p-6 text-center text-sm text-tb-slate-500">
      無待處理死信
    </p>
  {:else}
    <div class="overflow-hidden rounded-tb-2xl border border-tb-slate-200 bg-white shadow-tb-sm">
      <table class="w-full text-sm">
        <thead class="bg-tb-slate-50 text-left text-xs uppercase tracking-eyebrow text-tb-slate-500">
          <tr>
            <th class="px-4 py-2">subject / consumer</th>
            <th class="px-4 py-2">last_error</th>
            <th class="px-4 py-2">age</th>
            <th class="px-4 py-2"></th>
          </tr>
        </thead>
        <tbody>
          {#each data.messages as m (m.id)}
            <tr class="border-t border-tb-slate-100 align-top">
              <td class="px-4 py-3">
                <p class="font-jetbrains-mono text-xs text-tb-slate-900 break-all">{m.source_subject}</p>
                <p class="mt-1 font-jetbrains-mono text-xs text-tb-slate-500">
                  {m.source_stream}/{m.source_consumer}
                </p>
              </td>
              <td class="px-4 py-3 text-xs text-tb-rose-700 break-words max-w-md">{m.last_error}</td>
              <td class="px-4 py-3 font-jetbrains-mono text-xs text-tb-slate-500">{ageMinutes(m.first_seen_at)}</td>
              <td class="px-4 py-3">
                <div class="flex flex-col gap-1.5">
                  <form method="POST" action="?/replay">
                    <input type="hidden" name="id" value={m.id} />
                    <button class="w-full rounded-lg bg-tb-red-600 px-2 py-1 text-xs font-semibold text-white hover:bg-tb-red-700">重送</button>
                  </form>
                  <form method="POST" action="?/resolve" class="flex flex-col gap-1">
                    <input type="hidden" name="id" value={m.id} />
                    <input type="text" name="notes" placeholder="解決備註" class="rounded-lg border border-tb-slate-300 px-2 py-1 text-xs" />
                    <button class="w-full rounded-lg border border-tb-slate-300 px-2 py-1 text-xs font-semibold text-tb-slate-800 hover:border-tb-slate-500">標記解決</button>
                  </form>
                </div>
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  {/if}
</section>
