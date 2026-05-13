<script lang="ts">
  import { Card, StateTag } from "@tbite/ui";

  let { data, form } = $props();
  const v = data.vendor;

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
</script>

<section class="max-w-3xl space-y-4">
  <header>
    <a href="/vendors/{v.id}" class="text-xs text-tb-slate-500 hover:text-tb-slate-700">← 返回商家</a>
    <h1 class="mt-1 text-2xl font-black text-tb-slate-900">{v.display_name} · 合規文件</h1>
    <p class="mt-1 text-sm text-tb-slate-500 font-jetbrains-mono">{v.contact_email}</p>
  </header>

  {#if form?.error}
    <p class="rounded-lg bg-tb-rose-50 px-3 py-2 text-sm text-tb-rose-700">{form.error}</p>
  {/if}

  <Card>
    <h2 class="text-sm font-bold text-tb-slate-900">上傳新文件</h2>
    <form method="POST" action="?/upload" class="mt-3 grid gap-3 sm:grid-cols-2">
      <label class="flex flex-col gap-1 text-sm">
        <span class="text-xs uppercase tracking-eyebrow text-tb-slate-500">文件種類</span>
        <select bind:value={kind} name="kind" class="rounded-lg border border-tb-slate-300 px-3 py-2">
          <option value="business_license">營業登記</option>
          <option value="food_safety_permit">食安許可</option>
          <option value="tax_registration">稅籍登記</option>
          <option value="insurance">保險</option>
          <option value="other">其他</option>
        </select>
      </label>
      <label class="flex flex-col gap-1 text-sm">
        <span class="text-xs uppercase tracking-eyebrow text-tb-slate-500">到期日 (選填)</span>
        <input
          type="date"
          bind:value={expiresAt}
          name="expires_at"
          class="rounded-lg border border-tb-slate-300 px-3 py-2"
        />
      </label>
      <label class="flex flex-col gap-1 text-sm sm:col-span-2">
        <span class="text-xs uppercase tracking-eyebrow text-tb-slate-500">檔案</span>
        <input type="file" onchange={onFileChange} class="rounded-lg border border-tb-slate-300 px-3 py-2" />
      </label>
      <input type="hidden" name="filename" value={filename} />
      <input type="hidden" name="content_base64" value={contentBase64} />
      <div class="sm:col-span-2">
        <button
          type="submit"
          disabled={!filename || !contentBase64 || uploading}
          class="rounded-lg bg-tb-red-600 px-3.5 py-2 text-sm font-semibold text-white hover:bg-tb-red-700 disabled:bg-tb-slate-300"
        >
          {uploading ? "讀取中…" : "上傳"}
        </button>
      </div>
    </form>
  </Card>

  {#if data.documents.length === 0}
    <p class="rounded-tb-2xl border border-tb-slate-200 bg-white p-6 text-center text-sm text-tb-slate-500">
      尚無文件
    </p>
  {:else}
    <div class="overflow-hidden rounded-tb-2xl border border-tb-slate-200 bg-white shadow-tb-sm">
      <table class="w-full text-sm">
        <thead class="bg-tb-slate-50 text-left text-xs uppercase tracking-eyebrow text-tb-slate-500">
          <tr>
            <th class="px-4 py-2">檔名</th>
            <th class="px-4 py-2">種類</th>
            <th class="px-4 py-2">狀態</th>
            <th class="px-4 py-2">到期</th>
            <th class="px-4 py-2"></th>
          </tr>
        </thead>
        <tbody>
          {#each data.documents as d (d.id)}
            <tr class="border-t border-tb-slate-100 align-top">
              <td class="px-4 py-3">
                <p class="font-semibold text-tb-slate-900 break-all">{d.filename}</p>
                <p class="font-jetbrains-mono text-xs text-tb-slate-500 break-all">{d.blob_uri}</p>
                {#if d.notes}
                  <p class="mt-1 text-xs text-tb-slate-600">{d.notes}</p>
                {/if}
              </td>
              <td class="px-4 py-3 text-tb-slate-700">{kindLabel[d.kind] ?? d.kind}</td>
              <td class="px-4 py-3">
                <StateTag tone={statusTone[d.status] ?? "neutral"}>{statusLabel[d.status] ?? d.status}</StateTag>
              </td>
              <td class="px-4 py-3 font-jetbrains-mono text-xs text-tb-slate-500">{d.expires_at ?? "-"}</td>
              <td class="px-4 py-3">
                {#if d.status === "pending"}
                  <div class="flex flex-col gap-1.5">
                    <form method="POST" action="?/review">
                      <input type="hidden" name="id" value={d.id} />
                      <input type="hidden" name="status" value="approved" />
                      <input type="hidden" name="notes" value="" />
                      <button class="w-full rounded-lg bg-tb-red-600 px-2 py-1 text-xs font-semibold text-white hover:bg-tb-red-700">核准</button>
                    </form>
                    <form method="POST" action="?/review">
                      <input type="hidden" name="id" value={d.id} />
                      <input type="hidden" name="status" value="rejected" />
                      <input type="hidden" name="notes" value="" />
                      <button class="w-full rounded-lg border border-tb-rose-300 bg-tb-rose-50 px-2 py-1 text-xs font-semibold text-tb-rose-700 hover:border-tb-rose-600">駁回</button>
                    </form>
                  </div>
                {/if}
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  {/if}
</section>
