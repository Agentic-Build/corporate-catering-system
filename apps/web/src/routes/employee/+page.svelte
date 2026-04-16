<script lang="ts">
  import PortalSurface from "$lib/components/portal-surface.svelte";
  import type { PageData } from "./$types";

  let { data }: { data: PageData } = $props();
</script>

{#if data.actor}
  <PortalSurface
    role="employee"
    sectionId={data.sectionId}
    actorDisplayName={data.actor.displayName}
    actorId={data.actor.id}
    provider={data.auth.provider}
    experienceMode={data.experienceMode}
  />
{/if}
