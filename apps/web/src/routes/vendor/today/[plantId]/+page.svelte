<script lang="ts">
  import { PageHeader } from "$lib/components/ui";
  import FulfillmentBoard from "$lib/components/vendor/fulfillment-board.svelte";
  import { zhTW } from "$lib/i18n/zh-tw";

  import type { PageData } from "./$types";

  let { data }: { data: PageData } = $props();
</script>

<PageHeader
  title={`${zhTW.vendor.board.title} · ${data.plantId}`}
  description={`固定廠區 ${data.plantId}。若要切換廠區，請回到總覽看板。`}
  eyebrow={data.actor?.displayName ?? ""}
  breadcrumbs={data.breadcrumbs}
/>

<FulfillmentBoard
  apiBearerToken={data.auth.apiBearerToken}
  fixedPlantId={data.plantId}
  initialPlantId={data.plantId}
/>
