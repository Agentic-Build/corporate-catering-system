<script lang="ts">
  // FavoritesScreen — vendors the user has favourited. Vendor names are
  // resolved from today's menu; favourites with no item today still show
  // by id. Reached from ProfileScreen.
  import { onMount } from "svelte";
  import { goto } from "$app/navigation";
  import { getHome, groupByVendor, type VendorGroup } from "$lib/api";
  import { favorites } from "$lib/favorites.svelte";
  import { plateColor } from "$lib/sample";
  import AppIcon from "$lib/components/AppIcon.svelte";
  import Plate from "$lib/components/Plate.svelte";

  let allVendors = $state<VendorGroup[]>([]);
  let loading = $state(true);

  onMount(async () => {
    try {
      const home = await getHome();
      allVendors = groupByVendor(home.day_menu ?? []);
    } catch {
      allVendors = [];
    } finally {
      loading = false;
    }
  });

  const favList = $derived(allVendors.filter((v) => favorites.has(v.vendor_id)));
</script>

<div class="flex h-full flex-col">
  <div
    class="flex flex-shrink-0 items-center gap-3 bg-white px-4 pb-3"
    style="padding-top: max(env(safe-area-inset-top), 1rem)"
  >
    <button
      type="button"
      aria-label="返回"
      onclick={() => goto("/profile")}
      class="grid h-9 w-9 place-items-center rounded-full bg-tb-slate-100"
    >
      <AppIcon name="back" class="h-5 w-5 text-tb-slate-900" />
    </button>
    <h1 class="text-xl font-black text-tb-slate-900">我的常點</h1>
  </div>

  <div class="no-scroll flex-1 overflow-y-auto bg-tb-slate-50 px-4 py-3">
    {#if loading}
      <div class="grid gap-3">
        {#each [0, 1] as i (i)}
          <div class="h-24 animate-pulse rounded-2xl bg-tb-slate-200"></div>
        {/each}
      </div>
    {:else if favList.length > 0}
      <div class="grid gap-3">
        {#each favList as v (v.vendor_id)}
          <button
            type="button"
            onclick={() => goto(`/vendor/${v.vendor_id}`)}
            class="flex w-full cursor-pointer gap-3 rounded-2xl bg-white p-3 text-left shadow-sm ring-1 ring-tb-slate-200/70 transition active:scale-[0.98]"
          >
            <Plate
              color={plateColor(v.vendor_id)}
              class="h-16 w-16 flex-shrink-0 rounded-xl"
            />
            <div class="min-w-0 flex-1">
              <div class="text-sm font-extrabold text-tb-slate-900">{v.vendor}</div>
              <div class="mt-0.5 text-xs text-tb-slate-500">{v.items.length} 道餐點</div>
              <div class="mt-1.5 flex items-center gap-2">
                <span class="text-[10px] text-tb-slate-400">🕐 {v.eta_label}</span>
              </div>
            </div>
            <span class="self-center text-xl text-tb-rose-500">♥</span>
          </button>
        {/each}
      </div>
    {:else}
      <div
        class="grid place-items-center rounded-3xl border border-dashed border-tb-slate-300 bg-white py-16 text-center"
      >
        <p class="mb-2 text-4xl">♡</p>
        <p class="text-sm text-tb-slate-500">還沒有收藏的餐廳</p>
        <p class="mt-1 text-xs text-tb-slate-400">點擊愛心將喜歡的餐廳加入常點</p>
      </div>
    {/if}
  </div>
</div>
