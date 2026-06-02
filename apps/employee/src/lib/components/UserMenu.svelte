<script lang="ts">
  import { Icon } from "@tbite/ui";

  let { displayName, email = "" }: { displayName: string; email?: string } = $props();

  const initial = $derived((displayName ?? "").trim().slice(0, 1) || "你");
  let open = $state(false);
  let root = $state<HTMLElement>();

  function onWindowClick(e: MouseEvent) {
    if (open && root && !root.contains(e.target as Node)) open = false;
  }
  function onKey(e: KeyboardEvent) {
    if (e.key === "Escape") open = false;
  }
</script>

<svelte:window onclick={onWindowClick} onkeydown={onKey} />

<div class="relative" bind:this={root}>
  <button
    type="button"
    onclick={() => (open = !open)}
    class="ml-1 grid h-10 w-10 place-items-center rounded-full bg-gradient-to-br from-tb-red-500 to-tb-rose-700 text-sm font-bold text-white shadow-tb-sm"
    aria-haspopup="menu"
    aria-expanded={open}
    aria-label="使用者選單"
    title={displayName}
  >
    {initial}
  </button>
  {#if open}
    <div
      role="menu"
      class="absolute right-0 mt-2 w-56 overflow-hidden rounded-2xl border border-tb-slate-200 bg-white py-1 shadow-tb-md"
    >
      <div class="border-b border-tb-slate-100 px-4 py-3">
        <p class="truncate text-sm font-bold text-tb-slate-800">{displayName}</p>
        {#if email}
          <p class="truncate text-xs text-tb-slate-500">{email}</p>
        {/if}
      </div>
      <a
        href="/me"
        role="menuitem"
        onclick={() => (open = false)}
        class="flex items-center gap-2 px-4 py-2.5 text-sm font-semibold text-tb-slate-700 hover:bg-tb-slate-50"
      >
        <Icon name="user" class="h-4 w-4 text-tb-slate-400" />個人資料
      </a>
      <form method="POST" action="/auth/logout">
        <button
          type="submit"
          role="menuitem"
          class="flex w-full items-center gap-2 px-4 py-2.5 text-left text-sm font-semibold text-tb-red-600 hover:bg-tb-red-50"
        >
          <Icon name="close" class="h-4 w-4" />登出
        </button>
      </form>
    </div>
  {/if}
</div>
