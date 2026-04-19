<script lang="ts">
  import type { Snippet } from "svelte";

  import { PageHeader, Card, Button } from "$lib/components/ui";

  interface Props {
    role: string | null;
    plantId: string | null;
    children: Snippet;
  }

  let { role, plantId, children }: Props = $props();

  const unauthorized = $derived(role !== "employee");
  const missingPlant = $derived(role === "employee" && (!plantId || plantId.length === 0));
</script>

{#if unauthorized}
  <PageHeader title="無法進入員工頁面" description="此頁需要以員工身分登入。" />
  <Card variant="danger" title="權限不足">
    <p class="text-sm text-slate-700">
      您目前登入的身分不是員工，無法查看員工訂餐內容。
    </p>
    <div class="flex flex-wrap gap-2">
      <Button href="/" variant="primary">返回首頁</Button>
    </div>
  </Card>
{:else if missingPlant}
  <PageHeader title="尚未設定廠區" description="員工帳號需要具備廠區範圍才能使用。" />
  <Card variant="warning" title="缺少廠區範圍">
    <p class="text-sm text-slate-700">
      目前登入帳號沒有可用的廠區範圍，無法載入員工訂餐資料。請聯絡福委會管理員協助設定。
    </p>
  </Card>
{:else}
  {@render children()}
{/if}
