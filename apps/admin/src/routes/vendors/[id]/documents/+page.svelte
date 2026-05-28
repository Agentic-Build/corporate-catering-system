<script lang="ts">
  import { PageHeader, Card, StateTag, Button, Icon, Modal } from "@tbite/ui";

  let { data, form } = $props();
  const v = $derived(data.vendor);

  let filename = $state("");
  let kind = $state("business_license");
  let expiresAt = $state("");
  let contentBase64 = $state("");
  let uploading = $state(false);

  const kindLabel: Record<string, string> = {
    business_license: "營業登記",
    food_safety_permit: "食安許可",
    tax_registration: "稅籍登記",
    insurance: "保險",
    other: "其他",
  };

  const statusTone = {
    pending: "warning",
    approved: "success",
    rejected: "danger",
    expired: "neutral",
  } as Record<string, "info" | "neutral" | "warning" | "danger" | "success">;
  const statusLabel = {
    pending: "待審",
    approved: "已核准",
    rejected: "已駁回",
    expired: "已過期",
  } as Record<string, string>;

  async function onFileChange(e: Event) {
    const target = e.target as HTMLInputElement;
    const file = target.files?.[0];
    if (!file) {
      contentBase64 = "";
      return;
    }
    uploading = true;
    try {
      filename = file.name;
      const reader = new FileReader();
      const b64: string = await new Promise((resolve, reject) => {
        reader.onerror = () => reject(reader.error);
        reader.onload = () => {
          const dataUrl = String(reader.result ?? "");
          const idx = dataUrl.indexOf(",");
          resolve(idx >= 0 ? dataUrl.slice(idx + 1) : dataUrl);
        };
        reader.readAsDataURL(file);
      });
      contentBase64 = b64;
    } finally {
      uploading = false;
    }
  }

  type Doc = (typeof data.documents)[number];
  let confirmRejectTarget = $state<Doc | null>(null);
  const rejectFormEls: Record<string, HTMLFormElement> = {};
</script>

<a
  href="/vendors/{v.id}"
  class="-mb-2 inline-flex items-center gap-1 text-xs font-semibold text-tb-slate-500 hover:text-tb-slate-700"
>
  <Icon name="chevron" class="h-3.5 w-3.5 rotate-90" />返回商家
</a>

<PageHeader eyebrow="合規文件" title="{v.display_name} · 合規文件" subtitle={v.contact_email} />

<div class="grid max-w-3xl gap-6">
  {#if form?.error}
    <p class="rounded-lg bg-tb-rose-50 px-3 py-2 text-sm text-tb-rose-700">{form.error}</p>
  {/if}

  <Card title="上傳新文件" description="支援的文件種類會列入入駐審核">
    <form method="POST" action="?/upload" class="grid gap-3 sm:grid-cols-2">
      <label class="flex flex-col gap-1 text-sm">
        <span class="text-[11px] font-bold uppercase tracking-eyebrow text-tb-slate-500"
          >文件種類</span
        >
        <select
          bind:value={kind}
          name="kind"
          class="rounded-lg border border-tb-slate-300 px-3 py-2 focus:border-tb-slate-500 focus:outline-none focus:ring-2 focus:ring-tb-slate-300"
        >
          <option value="business_license">營業登記</option>
          <option value="food_safety_permit">食安許可</option>
          <option value="tax_registration">稅籍登記</option>
          <option value="insurance">保險</option>
          <option value="other">其他</option>
        </select>
      </label>
      <label class="flex flex-col gap-1 text-sm">
        <span class="text-[11px] font-bold uppercase tracking-eyebrow text-tb-slate-500"
          >到期日（選填）</span
        >
        <input
          type="date"
          bind:value={expiresAt}
          name="expires_at"
          class="rounded-lg border border-tb-slate-300 px-3 py-2 focus:border-tb-slate-500 focus:outline-none focus:ring-2 focus:ring-tb-slate-300"
        />
      </label>
      <label class="flex flex-col gap-1 text-sm sm:col-span-2">
        <span class="text-[11px] font-bold uppercase tracking-eyebrow text-tb-slate-500">檔案</span>
        <input
          type="file"
          onchange={onFileChange}
          class="rounded-lg border border-tb-slate-300 px-3 py-2"
        />
      </label>
      <input type="hidden" name="filename" value={filename} />
      <input type="hidden" name="content_base64" value={contentBase64} />
      <div class="sm:col-span-2">
        <Button
          variant="primary"
          size="md"
          type="submit"
          disabled={!filename || !contentBase64 || uploading}
        >
          <Icon name="doc" class="h-3.5 w-3.5" />{uploading ? "讀取中…" : "上傳文件"}
        </Button>
      </div>
    </form>
  </Card>

  {#if data.documents.length === 0}
    <p
      class="rounded-tb-2xl border border-dashed border-tb-slate-300 bg-tb-slate-50/60 p-8 text-center text-sm text-tb-slate-500"
    >
      尚無已上傳的文件
    </p>
  {:else}
    <Card>
      <div class="overflow-x-auto rounded-xl border border-tb-slate-200">
        <table class="w-full min-w-[40rem] text-sm">
          <thead
            class="bg-tb-slate-50/60 text-left text-[11px] font-bold uppercase tracking-wider text-tb-slate-500"
          >
            <tr>
              <th scope="col" class="px-4 py-2.5">檔名</th>
              <th scope="col" class="px-4 py-2.5">種類</th>
              <th scope="col" class="px-4 py-2.5">狀態</th>
              <th scope="col" class="px-4 py-2.5">到期</th>
              <th scope="col" class="px-4 py-2.5"></th>
            </tr>
          </thead>
          <tbody class="divide-y divide-tb-slate-100">
            {#each data.documents as d (d.id)}
              <tr class="align-top hover:bg-tb-slate-50/60">
                <td class="px-4 py-3">
                  <p class="break-all font-semibold text-tb-slate-900">{d.filename}</p>
                  <p class="break-all font-jetbrains-mono text-xs text-tb-slate-500">
                    {d.blob_uri}
                  </p>
                  {#if d.notes}
                    <p class="mt-1 text-xs text-tb-slate-600">{d.notes}</p>
                  {/if}
                </td>
                <td class="px-4 py-3 text-tb-slate-700">{kindLabel[d.kind] ?? d.kind}</td>
                <td class="px-4 py-3">
                  <StateTag tone={statusTone[d.status] ?? "neutral"}>
                    {statusLabel[d.status] ?? d.status}
                  </StateTag>
                </td>
                <td class="px-4 py-3 font-jetbrains-mono text-xs text-tb-slate-500">
                  {d.expires_at ?? "—"}
                </td>
                <td class="px-4 py-3">
                  {#if d.status === "pending"}
                    <div class="flex flex-col gap-1.5">
                      <form method="POST" action="?/review">
                        <input type="hidden" name="id" value={d.id} />
                        <input type="hidden" name="status" value="approved" />
                        <input type="hidden" name="notes" value="" />
                        <Button
                          variant="primary"
                          size="sm"
                          type="submit"
                          fullWidth
                          ariaLabel={`核准文件：${d.filename}`}
                        >
                          核准
                        </Button>
                      </form>
                      <form method="POST" action="?/review" bind:this={rejectFormEls[d.id]}>
                        <input type="hidden" name="id" value={d.id} />
                        <input type="hidden" name="status" value="rejected" />
                        <input type="hidden" name="notes" value="" />
                        <Button
                          variant="danger"
                          size="sm"
                          type="button"
                          fullWidth
                          ariaLabel={`駁回文件：${d.filename}`}
                          onclick={() => (confirmRejectTarget = d)}
                        >
                          駁回
                        </Button>
                      </form>
                    </div>
                  {/if}
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    </Card>
  {/if}
</div>

<Modal
  open={confirmRejectTarget !== null}
  onClose={() => (confirmRejectTarget = null)}
  title="確認駁回文件"
>
  {#if confirmRejectTarget}
    <p class="text-sm text-tb-slate-700">
      駁回後此文件將標記為「已駁回」。商家需重新上傳合格文件。此操作<strong>不可復原</strong>。
    </p>
    <p class="mt-2 font-semibold text-sm text-tb-slate-900">{confirmRejectTarget.filename}</p>
    <p class="text-xs text-tb-slate-500">
      {kindLabel[confirmRejectTarget.kind] ?? confirmRejectTarget.kind}
    </p>
  {/if}
  {#snippet footer()}
    <Button variant="secondary" size="md" onclick={() => (confirmRejectTarget = null)}>取消</Button>
    <Button
      variant="danger"
      size="md"
      onclick={() => {
        if (confirmRejectTarget) {
          const el = rejectFormEls[confirmRejectTarget.id];
          confirmRejectTarget = null;
          el?.requestSubmit();
        }
      }}
    >
      確認駁回
    </Button>
  {/snippet}
</Modal>
