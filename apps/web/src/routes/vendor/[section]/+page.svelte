<script lang="ts">
  import VendorPortalMvp from "$lib/components/vendor-portal-mvp.svelte";

  import type { PageData } from "./$types";

  let { data }: { data: PageData } = $props();
</script>

{#if data.actor}
  <VendorPortalMvp
    sectionId={data.sectionId}
    actorDisplayName={data.actor.displayName}
    actorId={data.actor.id}
    provider={data.auth.provider}
    plantId={data.actor.scope.plantIds[0] ?? null}
    apiBearerToken={data.auth.apiBearerToken}
  />
{/if}
