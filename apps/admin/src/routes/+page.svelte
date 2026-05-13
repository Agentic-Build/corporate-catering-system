<script lang="ts">
  import { StatCard } from "@tbite/ui";
  let { data } = $props();
</script>

<section class="space-y-6">
  <header>
    <h1 class="text-2xl font-black text-tb-slate-900">福委會治理</h1>
    <p class="mt-1 text-sm text-tb-slate-500">{data.user.display_name} · 商家治理中心</p>
  </header>

  <div class="grid grid-cols-2 gap-4 sm:grid-cols-4">
    <StatCard eyebrow="Pending"    value={data.counts.pending}    suffix="家" />
    <StatCard eyebrow="Approved"   value={data.counts.approved}   suffix="家" />
    <StatCard eyebrow="Suspended"  value={data.counts.suspended}  suffix="家" />
    <StatCard eyebrow="Terminated" value={data.counts.terminated} suffix="家" />
  </div>

  <div class="flex gap-3 text-sm">
    <a href="/vendors" class="rounded-lg bg-tb-red-600 px-3.5 py-2 font-semibold text-white hover:bg-tb-red-700">管理商家 →</a>
  </div>
</section>
