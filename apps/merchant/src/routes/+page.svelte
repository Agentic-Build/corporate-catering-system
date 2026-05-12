<script lang="ts">
  import { Button } from "@tbite/ui";
  let { data } = $props();
</script>

<h1 class="text-3xl font-black text-tb-slate-900">商家後台</h1>
<p class="mt-2 text-tb-slate-500">P0 skeleton · merchant app</p>
<p class="mt-1 text-sm">
  API health:
  <span
    class="font-jetbrains-mono"
    class:text-tb-emerald-500={data.apiHealth === "ok"}
    class:text-tb-rose-600={data.apiHealth !== "ok"}
  >
    {data.apiHealth}
  </span>
</p>
<div class="mt-6"><Button>送出預訂 · 由本月薪資代扣</Button></div>
