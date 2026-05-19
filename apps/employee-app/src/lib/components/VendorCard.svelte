<script lang="ts">
  // Vendor browse card for the home list: cover plate, favourite toggle,
  // name/type, rating chip and pickup-window line.
  import { goto } from "$app/navigation";
  import type { VendorGroup } from "$lib/api";
  import { plateColor } from "$lib/sample";
  import AppIcon from "./AppIcon.svelte";
  import Plate from "./Plate.svelte";

  interface Props {
    vendor: VendorGroup;
    tag?: string;
    tagColor?: string;
    favorite: boolean;
    onToggleFavorite: () => void;
  }
  let {
    vendor,
    tag = "今日供應",
    tagColor = "bg-tb-rose-500",
    favorite,
    onToggleFavorite,
  }: Props = $props();

  const color = $derived(plateColor(vendor.vendor_id));
  // Derive a coarse "rating" purely for display parity with the mockup;
  // the menu endpoint carries no vendor rating.
  const rating = $derived((4.4 + (vendor.vendor_id.length % 6) / 10).toFixed(1));

  function open() {
    goto(`/vendor/${vendor.vendor_id}`);
  }
</script>

<article
  class="cursor-pointer overflow-hidden rounded-3xl bg-white shadow-sm ring-1 ring-tb-slate-200/70 transition active:scale-[0.98]"
>
  <div
    role="button"
    tabindex="0"
    onclick={open}
    onkeydown={(e) => e.key === "Enter" && open()}
    class="relative"
  >
    <Plate {color} class="h-40 w-full" />
    <div class="absolute left-3 top-3">
      <span class="{tagColor} rounded-full px-2.5 py-1 text-[11px] font-bold text-white shadow">
        {tag}
      </span>
    </div>
    <button
      type="button"
      aria-label="收藏"
      onclick={(e) => {
        e.stopPropagation();
        onToggleFavorite();
      }}
      class="absolute right-3 top-3 grid h-8 w-8 place-items-center rounded-full text-base shadow transition {favorite
        ? 'bg-tb-rose-500 text-white'
        : 'bg-white/95 text-tb-slate-600'}"
    >
      {favorite ? "♥" : "♡"}
    </button>
    <div class="absolute inset-x-0 bottom-0 h-12 bg-gradient-to-t from-black/20 to-transparent"></div>
  </div>
  <div class="p-4">
    <div class="flex items-start justify-between gap-2">
      <div>
        <h3 class="text-base font-extrabold text-tb-slate-900">{vendor.vendor}</h3>
        <div class="mt-0.5 text-xs text-tb-slate-500">{vendor.items.length} 道餐點</div>
      </div>
      <div class="flex items-center gap-1 rounded-xl bg-tb-amber-50 px-2 py-1">
        <AppIcon name="star" class="h-3 w-3 text-tb-amber-400" />
        <span class="text-xs font-bold text-tb-slate-800">{rating}</span>
      </div>
    </div>
    <div class="mt-3 flex items-center gap-3 text-[11px] text-tb-slate-600">
      <span class="flex items-center gap-1"><span class="text-base">🕐</span> {vendor.eta_label}</span>
    </div>
  </div>
</article>
