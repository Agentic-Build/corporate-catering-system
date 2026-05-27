<script lang="ts">
  import { PageHeader, Card, Button, Icon } from "@tbite/ui";
  let { data, form } = $props();

  function ageMinutes(iso: string): string {
    const t = Date.parse(iso);
    if (Number.isNaN(t)) return "—";
    const mins = Math.floor((Date.now() - t) / 60000);
    if (mins < 60) return `${mins}m`;
    const h = Math.floor(mins / 60);
    if (h < 24) return `${h}h`;
    return `${Math.floor(h / 24)}d`;
  }
</script>

<PageHeader
  eyebrow="系統治理"
  title="死信佇列"
  subtitle="無法處理的事件 · 可重送原 subject 或標記已解決"
/>

{#if form?.error}
  <p class="rounded-lg bg-tb-rose-50 px-3 py-2 text-sm text-tb-rose-700">{form.error}</p>
{/if}

<form method="GET" class="flex flex-wrap items-center gap-2">
  <input
    type="text"
    name="stream"
    value={data.stream}
    placeholder="篩選 stream"
    aria-label="篩選 Stream"
    class="rounded-lg border border-tb-slate-300 px-3 py-1.5 text-sm focus:border-tb-slate-500 focus:outline-none focus:ring-2 focus:ring-tb-slate-300"
  />
  <Button variant="secondary" size="md" type="submit">篩選</Button>
  {#if data.stream}
    <a href="/dlq" class="text-xs text-tb-slate-500 hover:text-tb-slate-700">清除篩選</a>
  {/if}
</form>

<div class="mt-1">
  {#if data.messages.length === 0}
    <p
      class="rounded-tb-2xl border border-dashed border-tb-slate-300 bg-tb-slate-50/60 p-8 text-center text-sm text-tb-slate-500"
    >
      無待處理的死信訊息
    </p>
  {:else}
    <Card>
      <div class="overflow-x-auto rounded-xl border border-tb-slate-200">
        <table class="w-full min-w-[44rem] text-sm">
          <thead
            class="bg-tb-slate-50/60 text-left text-[11px] font-bold uppercase tracking-wider text-tb-slate-500"
          >
            <tr>
              <th scope="col" class="px-4 py-2.5">subject / consumer</th>
              <th scope="col" class="px-4 py-2.5">last_error</th>
              <th scope="col" class="px-4 py-2.5">age</th>
              <th scope="col" class="px-4 py-2.5"></th>
            </tr>
          </thead>
          <tbody class="divide-y divide-tb-slate-100">
            {#each data.messages as m (m.id)}
              <tr class="align-top hover:bg-tb-slate-50/60">
                <td class="px-4 py-3">
                  <p class="break-all font-jetbrains-mono text-xs text-tb-slate-900">
                    {m.source_subject}
                  </p>
                  <p class="mt-1 font-jetbrains-mono text-xs text-tb-slate-500">
                    {m.source_stream}/{m.source_consumer}
                  </p>
                </td>
                <td class="max-w-md break-words px-4 py-3 text-xs text-tb-rose-700">
                  {m.last_error}
                </td>
                <td class="px-4 py-3 font-jetbrains-mono text-xs text-tb-slate-500">
                  {ageMinutes(m.first_seen_at)}
                </td>
                <td class="px-4 py-3">
                  <div class="flex flex-col gap-1.5">
                    <form method="POST" action="?/replay">
                      <input type="hidden" name="id" value={m.id} />
                      <Button variant="primary" size="sm" type="submit" fullWidth>
                        <Icon name="download" class="h-3.5 w-3.5 rotate-180" />重送
                      </Button>
                    </form>
                    <form method="POST" action="?/resolve" class="flex flex-col gap-1">
                      <input type="hidden" name="id" value={m.id} />
                      <input
                        type="text"
                        name="notes"
                        placeholder="解決備註"
                        class="rounded-lg border border-tb-slate-300 px-2 py-1 text-xs focus:border-tb-slate-500 focus:outline-none focus:ring-2 focus:ring-tb-slate-300"
                      />
                      <Button variant="secondary" size="sm" type="submit" fullWidth>標記解決</Button
                      >
                    </form>
                  </div>
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    </Card>
  {/if}
</div>
