<script lang="ts">
  interface Props {
    amountMinor: number;
    currency: string;
  }

  let { amountMinor, currency }: Props = $props();

  const formatted = $derived.by(() => {
    const major = amountMinor / 100;
    return new Intl.NumberFormat("zh-TW", {
      style: "currency",
      currency,
      currencyDisplay: "code",
      minimumFractionDigits: 0,
      maximumFractionDigits: 2
    }).format(major);
  });
</script>

<span class="tabular-nums font-semibold text-slate-900">{formatted}</span>
