<script lang="ts">
  import type { PageData } from "./$types";

  let { data }: { data: PageData } = $props();
</script>

<svelte:head>
  <title>Admin Portal</title>
</svelte:head>

<main>
  <h1>Admin Portal</h1>
  <p>Authenticated actor: {data.actor?.displayName} ({data.actor?.id})</p>
  <p>Role: {data.actor?.role}</p>
  <p>Provider: {data.provider}</p>
  <p>Refresh after: {data.refreshAfterEpochMs}</p>
  <p>Expires at: {data.expiresAtEpochMs}</p>
  <p><a href="/auth/mock?logout=1&next=/">Sign out</a></p>
</main>
