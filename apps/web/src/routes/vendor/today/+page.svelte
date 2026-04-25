<script lang="ts">
  import { PageHeader } from "$lib/components/ui";
  import FulfillmentBoard from "$lib/components/vendor/fulfillment-board.svelte";
  import { zhTW } from "$lib/i18n/zh-tw";

  import type { PageData } from "./$types";

  let { data }: { data: PageData } = $props();

  const initialPlantId = $derived(data.actor?.scope.plantIds[0] ?? null);
</script>

<PageHeader
  title={zhTW.vendor.board.title}
  description={zhTW.vendor.board.description}
  eyebrow={data.actor?.displayName ?? ""}
  breadcrumbs={data.breadcrumbs}
/>

<FulfillmentBoard
  apiBearerToken={data.auth.apiBearerToken}
  fixedPlantId={null}
  {initialPlantId}
/>
