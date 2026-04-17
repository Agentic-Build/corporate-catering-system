<script lang="ts">
  import EmployeePortalMvp from "$lib/components/employee-portal-mvp.svelte";
  import type { PageData } from "./$types";

  let { data }: { data: PageData } = $props();
</script>

{#if data.actor}
  <EmployeePortalMvp
    sectionId={data.sectionId}
    actorDisplayName={data.actor.displayName}
    actorId={data.actor.id}
    provider={data.auth.provider}
    plantId={data.actor.scope.plantIds[0] ?? null}
  />
{/if}
