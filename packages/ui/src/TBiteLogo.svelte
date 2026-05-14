<script lang="ts">
  // Placeholder brand mark: rounded gradient tile with a "bite" notch on the
  // top-right corner where the amber accent dot peeks through. Designed to
  // be swapped out once the final brand asset lands — keep the Props
  // interface stable so the five callers (login pages, layouts, merchant
  // onboard) stay unchanged.
  interface Props {
    size?: number;
  }
  let { size = 28 }: Props = $props();

  // Stable id per instance so multiple <TBiteLogo /> on the same page don't
  // clash on the gradient / mask defs. Math.random is fine — not security
  // sensitive.
  const uid = `tbite-${Math.random().toString(36).slice(2, 8)}`;
</script>

<a href="/" class="inline-flex items-center gap-2 font-noto-tc" aria-label="T-Bite home">
  <svg
    width={size}
    height={size}
    viewBox="0 0 48 48"
    xmlns="http://www.w3.org/2000/svg"
    role="img"
    aria-label="T-Bite logo"
    class="shrink-0"
  >
    <defs>
      <linearGradient id="{uid}-grad" x1="0" y1="0" x2="1" y2="1">
        <stop offset="0%" stop-color="#ef4444" />
        <stop offset="100%" stop-color="#be123c" />
      </linearGradient>
      <mask id="{uid}-bite">
        <rect width="48" height="48" fill="white" />
        <!-- Circular notch cut from the top-right corner — the "bite". -->
        <circle cx="42" cy="6" r="9" fill="black" />
      </mask>
    </defs>

    <!-- Rounded tile with the bite carved out. -->
    <rect
      x="2"
      y="2"
      width="44"
      height="44"
      rx="11"
      fill="url(#{uid}-grad)"
      mask="url(#{uid}-bite)"
    />

    <!-- Amber accent dot sits inside the bite notch — peeks out like a
         garnish detail. -->
    <circle cx="40" cy="10" r="3.5" fill="#fbbf24" />

    <!-- Bold sans-serif "T" centered in the remaining mass. -->
    <path d="M 11 17 H 33 V 22 H 25 V 39 H 19 V 22 H 11 Z" fill="white" />
  </svg>

  <span class="text-[15px] font-black leading-tight text-tb-slate-900">T-Bite.</span>
</a>
