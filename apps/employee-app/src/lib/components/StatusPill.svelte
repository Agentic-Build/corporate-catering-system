<script lang="ts" module>
  // Order-status badge. Maps the backend's order status strings to a label
  // and colour. Unknown statuses degrade gracefully to a neutral pill.
  const MAP: Record<string, { label: string; cls: string }> = {
    ready: { label: "可領取", cls: "bg-tb-emerald-100 text-tb-emerald-800" },
    submitted: { label: "已預訂", cls: "bg-tb-sky-100 text-tb-sky-800" },
    placed: { label: "已預訂", cls: "bg-tb-sky-100 text-tb-sky-800" },
    prepping: { label: "備餐中", cls: "bg-tb-amber-100 text-tb-amber-800" },
    preparing: { label: "備餐中", cls: "bg-tb-amber-100 text-tb-amber-800" },
    picked_up: { label: "已領取", cls: "bg-tb-slate-100 text-tb-slate-600" },
    cancelled: { label: "已取消", cls: "bg-tb-slate-100 text-tb-slate-500" },
  };

  export function statusMeta(status: string) {
    return MAP[status] ?? { label: status, cls: "bg-tb-slate-100 text-tb-slate-600" };
  }
</script>

<script lang="ts">
  interface Props {
    status: string;
  }
  let { status }: Props = $props();
  const meta = $derived(statusMeta(status));
</script>

<span class="rounded-full px-2.5 py-1 text-[11px] font-bold {meta.cls}">{meta.label}</span>
