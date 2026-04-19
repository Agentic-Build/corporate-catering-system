<script lang="ts">
  type Tone =
    | "neutral"
    | "info"
    | "success"
    | "warning"
    | "danger"
    | "pending";

  interface Props {
    label: string;
    tone?: Tone;
  }

  let { label, tone = "neutral" }: Props = $props();

  const toneClass = $derived.by(() => {
    switch (tone) {
      case "info":
        return "bg-cyan-100 text-cyan-900";
      case "success":
        return "bg-emerald-100 text-emerald-900";
      case "warning":
        return "bg-amber-100 text-amber-900";
      case "danger":
        return "bg-rose-100 text-rose-900";
      case "pending":
        return "bg-slate-200 text-slate-800";
      default:
        return "bg-slate-100 text-slate-700";
    }
  });
</script>

<span class={`inline-flex items-center rounded-full px-2 py-0.5 text-xs font-semibold ${toneClass}`}>
  {label}
</span>
