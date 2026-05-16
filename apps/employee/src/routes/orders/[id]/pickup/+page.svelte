<script lang="ts">
  // 領餐核銷 — full-page pickup-code view. Reuses the shared TotpView
  // component (QR + 6-digit code + countdown); on expiry it re-fetches the
  // code via invalidateAll so the server load issues a fresh one.
  import { PageHeader, Card, Icon } from "@tbite/ui";
  import { invalidateAll } from "$app/navigation";
  import TotpView from "$lib/components/TotpView.svelte";

  let { data } = $props();
</script>

<a
  href={`/orders/${data.order.id}`}
  class="mb-3 inline-flex items-center gap-1 text-xs font-semibold text-tb-slate-500 hover:text-tb-slate-900"
>
  <Icon name="chevron" class="h-3.5 w-3.5 rotate-90" />返回訂單
</a>

<div class="mx-auto max-w-md">
  <PageHeader
    eyebrow="Pickup · 領餐碼"
    title="於領餐區出示此頁"
    subtitle="工讀生掃描 QR 後完成核銷，動態碼每 30 秒更新一次。"
  />

  <Card>
    <TotpView
      orderId={data.code.order_id}
      code={data.code.code}
      expiresInSeconds={data.code.expires_in_seconds}
      onExpire={() => invalidateAll()}
    />
  </Card>
</div>
