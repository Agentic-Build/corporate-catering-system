<script lang="ts">
  // Login screen. Kicks off the OIDC deep-link flow (see lib/auth.ts).
  // After the system browser completes login the app is re-opened via the
  // tbite:// deep link, which stores the token and the auth gate routes home.
  import { startLogin } from "$lib/auth";

  let busy = $state<string | null>(null);

  const PROVIDERS = [
    { id: "google", label: "使用 Google 登入", glyph: "G" },
    { id: "microsoft", label: "使用 Microsoft 登入", glyph: "⊞" },
  ];

  async function login(provider: string) {
    busy = provider;
    try {
      await startLogin(provider);
    } finally {
      // If the system browser opened, the app will be re-entered via deep
      // link; clearing busy lets the user retry if they cancel.
      busy = null;
    }
  }
</script>

<div
  class="flex h-full flex-col justify-between bg-gradient-to-br from-tb-slate-900 via-tb-rose-900 to-tb-red-800 px-8 text-white"
  style="padding-top: max(env(safe-area-inset-top), 2rem); padding-bottom: max(env(safe-area-inset-bottom), 2rem)"
>
  <div class="flex flex-1 flex-col items-center justify-center text-center">
    <div
      class="mb-6 grid h-20 w-20 place-items-center rounded-3xl bg-white text-3xl font-black text-tb-red-600 shadow-2xl"
    >
      T
    </div>
    <h1 class="text-3xl font-black">T-Bite</h1>
    <p class="mt-2 text-sm text-white/70">企業餐飲 · 員工訂餐 App</p>
  </div>

  <div class="grid gap-3">
    {#each PROVIDERS as p (p.id)}
      <button
        type="button"
        disabled={busy != null}
        onclick={() => login(p.id)}
        class="flex w-full items-center justify-center gap-3 rounded-2xl bg-white py-4 text-sm font-bold text-tb-slate-900 shadow-lg disabled:opacity-60"
      >
        <span class="grid h-6 w-6 place-items-center rounded-full bg-tb-slate-100 text-xs">
          {p.glyph}
        </span>
        {busy === p.id ? "開啟登入中…" : p.label}
      </button>
    {/each}
    <p class="mt-2 text-center text-[11px] text-white/50">
      登入即表示同意以薪資代扣方式支付餐費
    </p>
  </div>
</div>
