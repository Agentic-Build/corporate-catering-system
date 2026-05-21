<script lang="ts">
  // Root shell. The real native app is full-screen; the phone-frame styling
  // from the design mockup is dropped on purpose (it was only a harness).
  //
  // Responsibilities:
  //  - import global CSS
  //  - hydrate the session/favorites stores and register deep links once
  //  - gate the app: unauthenticated users are sent to /login
  //  - mount the bottom nav and global overlays (cart, notifications)
  import "../app.css";
  import { onMount, type Snippet } from "svelte";
  import { goto } from "$app/navigation";
  import { page } from "$app/stores";
  import { initDeepLinks } from "$lib/auth";
  import { favorites } from "$lib/favorites.svelte";
  import { session } from "$lib/session.svelte";
  import BottomNav from "$lib/components/BottomNav.svelte";
  import CartBar from "$lib/components/CartBar.svelte";
  import CartSheet from "$lib/components/CartSheet.svelte";
  import NotifModal from "$lib/components/NotifModal.svelte";
  import { uiState } from "$lib/ui.svelte";

  let { children }: { children: Snippet } = $props();

  onMount(() => {
    session.hydrate();
    favorites.hydrate();
    void initDeepLinks();
  });

  // Routes that render their own full-screen chrome (no bottom nav).
  const path = $derived($page.url.pathname);
  const isLogin = $derived(path === "/login");
  const isImmersive = $derived(
    isLogin || path === "/totp" || path.startsWith("/vendor/"),
  );

  // Auth gate — runs once the session store has hydrated.
  $effect(() => {
    if (!session.ready) return;
    if (!session.isAuthed && !isLogin) {
      goto("/login");
    } else if (session.isAuthed && isLogin) {
      goto("/");
    }
  });

  // Supply date for checkout — today; the home strip can override it later.
  const today = new Date().toISOString().slice(0, 10);
</script>

<div class="mx-auto flex h-[100dvh] max-w-md flex-col overflow-hidden bg-white">
  <div class="relative flex-1 overflow-hidden">
    {@render children()}

    {#if session.isAuthed && !isLogin}
      <!-- Global overlays, anchored to the screen area -->
      <CartSheet
        open={uiState.cartOpen}
        onClose={() => (uiState.cartOpen = false)}
        supplyDate={today}
      />
      <NotifModal open={uiState.notifOpen} onClose={() => (uiState.notifOpen = false)} />
      {#if !isImmersive}
        <CartBar onOpen={() => (uiState.cartOpen = true)} />
      {/if}
    {/if}
  </div>

  {#if session.isAuthed && !isImmersive}
    <BottomNav />
  {/if}
</div>
