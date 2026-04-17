<script lang="ts">
  import AdminPortalMvp from "$lib/components/admin-portal-mvp.svelte";
  import type { PageData } from "./$types";

  let { data }: { data: PageData } = $props();
</script>

{#if data.actor}
  <AdminPortalMvp
    sectionId={data.sectionId}
    actorDisplayName={data.actor.displayName}
    actorId={data.actor.id}
    provider={data.auth.provider}
    apiBearerToken={data.auth.apiBearerToken}
  />
{/if}
