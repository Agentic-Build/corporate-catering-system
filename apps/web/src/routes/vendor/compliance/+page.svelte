<script lang="ts">
  import { Button, Card, PageHeader } from "$lib/components/ui";
  import { zhTW } from "$lib/i18n/zh-tw";

  import type { PageData } from "./$types";

  let { data }: { data: PageData } = $props();
</script>

<PageHeader
  title={zhTW.vendor.compliance.title}
  description="在此管理你的合規文件。目前商家端尚未開放「查看自己狀態」的 API；**實際狀態由福委會管理員掌握並以 email / 電話通知**。"
  breadcrumbs={data.breadcrumbs}
/>

<div class="grid gap-4">
  <Card title="我該做什麼？" variant="info">
    <ol class="ml-4 list-decimal space-y-2 text-sm text-slate-700">
      <li>
        <strong>首次入駐 / 補件</strong>：到「建立上傳計畫」上傳商業登記、食品安全證照等文件。上傳完成後，<em>請主動通知福委會</em>來複核。
      </li>
      <li>
        <strong>文件到期前續件</strong>：系統會由福委會端的 lifecycle 偵測到期，但**不會**自動通知你；建議每月主動檢視有效期、提前上傳新版本。
      </li>
      <li>
        <strong>被停權後復權</strong>：先上傳補件文件，再聯絡福委會觸發審核；通過後會自動恢復接單。
      </li>
      <li>
        <strong>驗證已上傳文件</strong>：到「建立下載連結」輸入 objectRef，產生短效預簽章 URL，確認福委會能正確開啟。
      </li>
    </ol>
  </Card>

  <div class="grid gap-4 md:grid-cols-2">
    <Card title={zhTW.vendor.compliance.uploadTitle} description="建立 presigned 上傳計畫供文件或圖片上傳。">
      {#snippet actions()}
        <Button href="/vendor/compliance/upload" variant="primary">建立上傳計畫</Button>
      {/snippet}
      <p class="text-sm text-slate-600">
        支援 COMPLIANCE_DOCUMENT（max 20MB；PDF/JPG/PNG）、MENU_IMAGE（max 10MB；JPG/PNG/WebP）、MENU_IMAGE_THUMBNAIL。
      </p>
    </Card>

    <Card title={zhTW.vendor.compliance.accessLinkTitle} description="為既有的 objectRef 產生限時下載連結。">
      {#snippet actions()}
        <Button href="/vendor/compliance/access-links" variant="primary">建立下載連結</Button>
      {/snippet}
      <p class="text-sm text-slate-600">下載連結為預簽章 URL，有效期通常為 1 小時。</p>
    </Card>
  </div>

  <Card title="常見狀態說明" variant="default">
    <dl class="grid gap-2 text-sm text-slate-700 md:grid-cols-2">
      <div>
        <dt class="font-semibold">Active / 通過</dt>
        <dd>所有必交文件齊全且在有效期內。可正常接單。</dd>
      </div>
      <div>
        <dt class="font-semibold">Fix Requested / 要求補件</dt>
        <dd>福委會要求補件。接單暫停；上傳文件後請主動通知福委會重審。</dd>
      </div>
      <div>
        <dt class="font-semibold">Suspended / 停權</dt>
        <dd>必交文件逾期。無法接單；重新上傳後聯絡福委會復權。</dd>
      </div>
      <div>
        <dt class="font-semibold">Pending Review / 審核中</dt>
        <dd>已提交申請或補件，等待福委會審核。</dd>
      </div>
    </dl>
  </Card>
</div>
