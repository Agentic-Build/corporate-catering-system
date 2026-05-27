<script lang="ts">
  import { page } from "$app/stores";

  const title = $derived(
    $page.status === 404 ? "找不到頁面" : $page.status === 403 ? "沒有存取權限" : "發生問題",
  );
</script>

<div class="flex min-h-[60vh] flex-col items-center justify-center px-6 text-center">
  <p class="text-5xl font-black text-tb-red-600">{$page.status}</p>
  <h1 class="mt-3 text-2xl font-extrabold tracking-tight text-tb-slate-900">{title}</h1>
  <p class="mt-2 max-w-sm text-sm text-tb-slate-500">
    {$page.error?.message ?? "請稍後再試，或回到首頁重新開始。"}
  </p>
  <a
    href="/"
    class="mt-6 rounded-xl bg-tb-red-600 px-5 py-2.5 text-sm font-bold text-white transition hover:bg-tb-red-700"
  >
    回首頁
  </a>
</div>
