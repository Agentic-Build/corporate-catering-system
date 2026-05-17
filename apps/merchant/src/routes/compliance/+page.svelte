<script lang="ts">
  import { PageHeader, Card, StateTag, EmptyState, Icon, Button } from "@tbite/ui";

  let { data, form } = $props();

  const docKinds = [
    { id: "business_license", label: "營業登記" },
    { id: "food_safety_permit", label: "食品安全許可" },
    { id: "tax_registration", label: "稅籍登記" },
    { id: "insurance", label: "保險證明" },
    { id: "other", label: "其他" },
  ];

  // When set, the upload form is locked to resupplying this specific document.
  let resupplyDoc = $state<{ id: string; kind: string } | null>(null);

  // A document can be resupplied once it has been reviewed — rejected and
  // expired docs need a fix; an approved doc may be renewed proactively.
  function canResupply(status: string): boolean {
    return status === "rejected" || status === "expired" || status === "approved";
  }

  function startResupply(doc: { id: string; kind: string }) {
    resupplyDoc = { id: doc.id, kind: doc.kind };
    document.getElementById("doc-upload-card")?.scrollIntoView({ behavior: "smooth" });
  }

  // Vendor-status banner copy — one entry per `vendor.status` enum value.
  const statusBanner = {
    pending: {
      label: "待審核",
      tone: "border-tb-amber-300 bg-tb-amber-50 text-tb-amber-900",
      dot: "bg-tb-amber-400",
      desc: "您的商家資料正由福委會審核中，審核通過後即可開始接單供餐。",
    },
    approved: {
      label: "已核准",
      tone: "border-tb-emerald-500/40 bg-tb-emerald-50 text-tb-emerald-900",
      dot: "bg-tb-emerald-500",
      desc: "您的商家已通過審核，可正常排程菜單、接單與供餐。請留意文件到期，避免影響營運。",
    },
    suspended: {
      label: "停權中",
      tone: "border-tb-rose-300 bg-tb-rose-50 text-tb-rose-900",
      dot: "bg-tb-rose-600",
      desc: "您的商家目前被暫停接單。請依下方警示補正文件或聯繫福委會，恢復後即可重新供餐。",
    },
    terminated: {
      label: "已終止",
      tone: "border-tb-slate-300 bg-tb-slate-100 text-tb-slate-800",
      dot: "bg-tb-slate-500",
      desc: "您與本系統的合作已終止，無法再接單供餐。如有疑問請聯繫福委會。",
    },
  } as Record<string, { label: string; tone: string; dot: string; desc: string }>;

  const banner = $derived(
    statusBanner[data.vendor?.status ?? ""] ?? {
      label: data.vendor?.status ?? "未知",
      tone: "border-tb-slate-300 bg-tb-slate-100 text-tb-slate-800",
      dot: "bg-tb-slate-500",
      desc: "目前無法判斷商家狀態，請聯繫福委會。",
    },
  );

  const docKindLabel = {
    business_license: "營業登記",
    food_safety_permit: "食品安全許可",
    tax_registration: "稅籍登記",
    insurance: "保險證明",
    other: "其他",
  } as Record<string, string>;

  const docStatusMeta = {
    pending: { tone: "pending", label: "待審核" },
    approved: { tone: "success", label: "已核准" },
    rejected: { tone: "danger", label: "已駁回" },
    expired: { tone: "warning", label: "已過期" },
  } as Record<
    string,
    { tone: "success" | "danger" | "warning" | "pending" | "neutral"; label: string }
  >;

  const warningMeta = {
    document_rejected: { label: "文件遭駁回" },
    document_expired: { label: "文件已過期" },
    document_expiring: { label: "文件即將到期" },
    document_missing: { label: "缺少必繳文件" },
  } as Record<string, { label: string }>;

  function warningTone(severity: string): "danger" | "warning" | "info" {
    if (severity === "high" || severity === "critical") return "danger";
    if (severity === "medium") return "warning";
    return "info";
  }

  function fmtDate(s: string | undefined | null): string {
    if (!s) return "—";
    return new Date(s).toLocaleDateString("zh-TW", {
      year: "numeric",
      month: "2-digit",
      day: "2-digit",
    });
  }
</script>

<PageHeader
  eyebrow="Compliance · 商家合規自查"
  title="商家合規自查"
  subtitle="查看您的商家審核狀態、合規文件與系統警示，並可自行上傳或補件。"
/>

<!-- Status banner -->
<div class="mb-6 rounded-tb-2xl border p-5 {banner.tone}">
  <div class="flex items-center gap-2 text-xs font-bold uppercase tracking-eyebrow">
    <span class="h-2 w-2 rounded-full {banner.dot}" aria-hidden="true"></span>
    目前狀態
  </div>
  <p class="mt-2 text-3xl font-black tracking-tight">{banner.label}</p>
  <p class="mt-2 text-sm">{banner.desc}</p>
  {#if data.vendor?.display_name}
    <p class="mt-3 text-xs opacity-70">商家：{data.vendor.display_name}</p>
  {/if}
</div>

<!-- Warnings -->
<section class="mb-8">
  <h2 class="mb-3 text-lg font-bold text-tb-slate-900">合規警示</h2>
  {#if data.warnings.length === 0}
    <div
      class="flex items-center gap-2 rounded-tb-2xl border border-tb-emerald-500/40 bg-tb-emerald-50/60 px-4 py-3 text-sm font-semibold text-tb-emerald-800"
    >
      <Icon name="check" class="h-4 w-4" />
      目前沒有合規警示，所有文件狀態正常。
    </div>
  {:else}
    <div class="space-y-2">
      {#each data.warnings as w, i (i)}
        {@const meta = warningMeta[w.kind] ?? { label: w.kind }}
        <Card tone={warningTone(w.severity)}>
          <div class="flex items-start gap-3">
            <Icon name="alert" class="mt-0.5 h-4 w-4 flex-shrink-0 text-tb-slate-600" />
            <div>
              <div class="flex items-center gap-2">
                <span class="text-sm font-bold text-tb-slate-900">{meta.label}</span>
                <StateTag tone={warningTone(w.severity)}>{w.severity}</StateTag>
              </div>
              <p class="mt-1 text-sm text-tb-slate-700">{w.message}</p>
            </div>
          </div>
        </Card>
      {/each}
    </div>
  {/if}
</section>

<!-- Documents -->
<section>
  <h2 class="mb-3 text-lg font-bold text-tb-slate-900">合規文件</h2>
  {#if data.documents.length === 0}
    <EmptyState
      icon="doc"
      title="尚無合規文件"
      hint="您的商家目前沒有任何已上傳文件，請聯繫福委會協助補件。"
    />
  {:else}
    <div class="overflow-hidden rounded-tb-2xl border border-tb-slate-200 bg-white shadow-tb-sm">
      <table class="w-full text-sm">
        <thead
          class="bg-tb-slate-50/60 text-left text-[11px] font-bold uppercase tracking-eyebrow text-tb-slate-500"
        >
          <tr>
            <th class="px-5 py-3">文件種類</th>
            <th class="px-3 py-3">檔名</th>
            <th class="px-3 py-3">狀態</th>
            <th class="px-3 py-3">到期日</th>
            <th class="px-3 py-3">審核日</th>
            <th class="px-3 py-3">備註</th>
            <th class="px-5 py-3">操作</th>
          </tr>
        </thead>
        <tbody class="divide-y divide-tb-slate-100">
          {#each data.documents as doc (doc.id)}
            {@const meta = docStatusMeta[doc.status] ?? { tone: "neutral", label: doc.status }}
            <tr class="hover:bg-tb-slate-50/60">
              <td class="px-5 py-3 font-semibold text-tb-slate-900">
                {docKindLabel[doc.kind] ?? doc.kind}
              </td>
              <td class="px-3 py-3 text-tb-slate-600">{doc.filename}</td>
              <td class="px-3 py-3">
                <StateTag tone={meta.tone}>{meta.label}</StateTag>
              </td>
              <td class="px-3 py-3 font-jetbrains-mono text-xs text-tb-slate-600">
                {fmtDate(doc.expires_at)}
              </td>
              <td class="px-3 py-3 font-jetbrains-mono text-xs text-tb-slate-600">
                {fmtDate(doc.reviewed_at)}
              </td>
              <td class="px-3 py-3 text-xs text-tb-slate-500">{doc.notes || "—"}</td>
              <td class="px-5 py-3">
                {#if canResupply(doc.status)}
                  <button
                    type="button"
                    onclick={() => startResupply({ id: doc.id, kind: doc.kind })}
                    class="rounded-tb-lg border border-tb-slate-300 px-2.5 py-1 text-xs font-semibold text-tb-slate-700 transition hover:border-tb-slate-500"
                  >
                    補件
                  </button>
                {/if}
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  {/if}
</section>

<!-- Upload / resupply -->
<section class="mt-8" id="doc-upload-card">
  <h2 class="mb-3 text-lg font-bold text-tb-slate-900">
    {resupplyDoc ? "補件 · 重新上傳文件" : "上傳合規文件"}
  </h2>
  <Card>
    {#if form?.uploadError}
      <p class="mb-3 rounded-tb-xl bg-tb-rose-50 px-3 py-2 text-sm text-tb-rose-700">
        {form.uploadError}
      </p>
    {/if}
    {#if form?.uploadOk}
      <p class="mb-3 rounded-tb-xl bg-tb-emerald-50 px-3 py-2 text-sm text-tb-emerald-700">
        文件已送出，福委會將進行審核。
      </p>
    {/if}
    {#if resupplyDoc}
      <p
        class="mb-3 flex items-center gap-2 rounded-tb-xl bg-tb-amber-50 px-3 py-2 text-sm text-tb-amber-800"
      >
        <Icon name="alert" class="h-4 w-4" />
        補件對象：{docKindLabel[resupplyDoc.kind] ?? resupplyDoc.kind}
        <button
          type="button"
          onclick={() => (resupplyDoc = null)}
          class="ml-auto font-semibold underline">改為上傳新文件</button
        >
      </p>
    {/if}
    <form method="POST" action="?/uploadDocument" enctype="multipart/form-data" class="space-y-3">
      {#if resupplyDoc}
        <input type="hidden" name="supersedes" value={resupplyDoc.id} />
        <input type="hidden" name="kind" value={resupplyDoc.kind} />
      {:else}
        <label class="flex flex-col gap-1.5 text-sm">
          <span class="text-[11px] font-bold uppercase tracking-eyebrow text-tb-slate-500">
            文件種類
          </span>
          <select
            name="kind"
            required
            class="rounded-tb-lg border border-tb-slate-300 px-3 py-2 text-sm transition focus:border-tb-red-500 focus:outline-none focus:ring-4 focus:ring-tb-red-100"
          >
            <option value="" disabled selected>請選擇文件種類</option>
            {#each docKinds as k (k.id)}
              <option value={k.id}>{k.label}</option>
            {/each}
          </select>
        </label>
      {/if}
      <label class="flex flex-col gap-1.5 text-sm">
        <span class="text-[11px] font-bold uppercase tracking-eyebrow text-tb-slate-500">
          檔案（PDF 或圖片，上限 10MB）
        </span>
        <input
          type="file"
          name="file"
          required
          accept=".pdf,image/*"
          class="text-sm text-tb-slate-700 file:mr-3 file:rounded-tb-lg file:border-0 file:bg-tb-slate-100 file:px-3 file:py-2 file:text-sm file:font-semibold"
        />
      </label>
      <label class="flex flex-col gap-1.5 text-sm">
        <span class="text-[11px] font-bold uppercase tracking-eyebrow text-tb-slate-500">
          到期日（選填）
        </span>
        <input
          type="date"
          name="expires_at"
          class="rounded-tb-lg border border-tb-slate-300 px-3 py-2 text-sm transition focus:border-tb-red-500 focus:outline-none focus:ring-4 focus:ring-tb-red-100"
        />
      </label>
      <Button variant="primary" size="md" type="submit">
        {resupplyDoc ? "送出補件" : "上傳文件"}
      </Button>
    </form>
  </Card>
</section>
