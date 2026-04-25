<script lang="ts">
  /**
   * Zero-dependency SVG sparkline.
   *
   * Given a series of numeric values, draws a line + area fill that auto-scales
   * to the container width. Includes min/max dots and an optional baseline.
   *
   * Designed for dashboard "at a glance" signals — not for precise analysis.
   */

  interface Props {
    values: number[];
    width?: number;
    height?: number;
    stroke?: string;
    fill?: string;
    showDots?: boolean;
    "aria-label"?: string;
  }

  let {
    values,
    width = 160,
    height = 48,
    stroke = "#0891b2", // cyan-600
    fill = "rgba(8, 145, 178, 0.1)",
    showDots = true,
    "aria-label": ariaLabel
  }: Props = $props();

  const stats = $derived.by(() => {
    if (values.length === 0) return { min: 0, max: 1, range: 1 };
    const min = Math.min(...values);
    const max = Math.max(...values);
    const range = max - min || 1;
    return { min, max, range };
  });

  const points = $derived.by(() => {
    if (values.length === 0) return [] as Array<{ x: number; y: number; value: number }>;
    const padding = 4;
    const innerW = width - padding * 2;
    const innerH = height - padding * 2;
    const stepX = values.length > 1 ? innerW / (values.length - 1) : 0;
    return values.map((value, index) => ({
      x: padding + index * stepX,
      y: padding + innerH - ((value - stats.min) / stats.range) * innerH,
      value
    }));
  });

  const linePath = $derived.by(() => {
    if (points.length === 0) return "";
    return points.map((p, i) => `${i === 0 ? "M" : "L"}${p.x.toFixed(1)},${p.y.toFixed(1)}`).join(" ");
  });

  const areaPath = $derived.by(() => {
    if (points.length === 0) return "";
    const baseY = height - 4;
    const first = points[0];
    const last = points[points.length - 1];
    return `M${first.x.toFixed(1)},${baseY} ${points
      .map((p) => `L${p.x.toFixed(1)},${p.y.toFixed(1)}`)
      .join(" ")} L${last.x.toFixed(1)},${baseY} Z`;
  });
</script>

{#if values.length === 0}
  <span class="inline-block text-xs text-slate-400">—</span>
{:else}
  <svg
    xmlns="http://www.w3.org/2000/svg"
    {width}
    {height}
    viewBox={`0 0 ${width} ${height}`}
    role="img"
    aria-label={ariaLabel ?? `${values.length} 點趨勢`}
    class="inline-block align-middle"
  >
    <path d={areaPath} fill={fill} stroke="none" />
    <path d={linePath} fill="none" stroke={stroke} stroke-width="1.5" stroke-linejoin="round" stroke-linecap="round" />
    {#if showDots && points.length > 0}
      <circle cx={points[points.length - 1].x} cy={points[points.length - 1].y} r="2.5" fill={stroke} />
    {/if}
  </svg>
{/if}
